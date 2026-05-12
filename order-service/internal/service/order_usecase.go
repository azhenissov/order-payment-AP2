package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"order-service/internal/domain"

	"github.com/google/uuid"
)

type ErrServiceUnavailable struct {
	Message string
}

func (e *ErrServiceUnavailable) Error() string {
	return e.Message
}

type orderUseCase struct {
	repo          domain.OrderRepository
	paymentClient domain.PaymentClient
	broker        *OrderBroker
	cache         domain.OrderCache
	ttl           time.Duration
}

func NewOrderUseCase(repo domain.OrderRepository, paymentClient domain.PaymentClient, broker *OrderBroker, cache domain.OrderCache, ttlMinutes int) domain.OrderUseCase {
	return &orderUseCase{
		repo:          repo,
		paymentClient: paymentClient,
		broker:        broker,
		cache:         cache,
		ttl:           time.Duration(ttlMinutes) * time.Minute,
	}
}

// CreateOrder — создает заказ и запускает фоновую обработку
func (u *orderUseCase) CreateOrder(ctx context.Context, customerID, itemName string, amount int64, idempotencyKey string) (*domain.Order, error) {
	if idempotencyKey != "" {
		existingOrder, err := u.repo.GetByIdempotencyKey(ctx, idempotencyKey)
		if err == nil && existingOrder != nil {
			return existingOrder, nil
		}
	}

	if amount <= 0 {
		return nil, errors.New("amount must be greater than zero")
	}

	order := &domain.Order{
		ID:             uuid.New().String(),
		CustomerID:     customerID,
		ItemName:       itemName,
		Amount:         amount,
		Status:         "Pending",
		CreatedAt:      time.Now(),
		IdempotencyKey: idempotencyKey,
	}

	if err := u.repo.Store(ctx, order); err != nil {
		return nil, err
	}

	// Асинхронная логика
	go func(orderID string, amount int64) {

		bgCtx := context.Background()

		time.Sleep(15 * time.Second) // pending status simulating

		currentOrder, err := u.repo.GetByID(bgCtx, orderID)
		if err != nil {
			log.Printf("Error in checking order before payment %v", err)
			return
		}

		if currentOrder.Status == "Cancelled" {
			log.Printf("Order %s is cancelled, skipping payment", orderID)
			return
		}

		_, err = u.paymentClient.AuthorizePayment(bgCtx, orderID, amount)
		if err != nil {
			_ = u.repo.UpdateStatus(bgCtx, orderID, "Payment Failed")

			_ = u.cache.InvalidateOrder(context.Background(), orderID)

			u.broker.Publish(orderID, "Payment Failed")
			return
		}

		_ = u.repo.UpdateStatus(bgCtx, orderID, "Paid")

		_ = u.cache.InvalidateOrder(bgCtx, orderID)

		u.broker.Publish(orderID, "Paid")
	}(order.ID, order.Amount)

	return order, nil
}

func (u *orderUseCase) Checkout(ctx context.Context, orderID string, customerID string, itemName string, amount int64) error {
	existingOrder, err := u.repo.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("failed to check existing order: %w", err)
	}
	if existingOrder != nil {
		return fmt.Errorf("order with ID %s already exists", orderID)
	}

	err = u.repo.CreateOrder(ctx, orderID, customerID, itemName, amount, "PENDING")
	if err != nil {
		return fmt.Errorf("failed to create order: %w", err)
	}

	transactionID, err := u.paymentClient.AuthorizePayment(ctx, orderID, amount)
	if err != nil {
		log.Printf("Payment failed for order %s: %v", orderID, err)
		_ = u.repo.UpdateStatus(ctx, orderID, "FAILED")

		_ = u.cache.InvalidateOrder(context.Background(), orderID)
		return fmt.Errorf("payment authorization failed: %w", err)
	}

	err = u.repo.UpdateOrderPaid(ctx, orderID, "Paid", transactionID)
	if err != nil {
		log.Printf("Failed to update status to PAID for order %s: %v", orderID, err)
		return err
	}

	if cacheErr := u.cache.InvalidateOrder(context.Background(), orderID); cacheErr != nil {
		log.Printf("Failed to invalidate order %s in cache: %v", orderID, cacheErr)
	}

	log.Printf("Order %s successfully processed and paid with TxID: %s", orderID, transactionID)
	return nil
}

func (u *orderUseCase) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	cachedOrder, err := u.cache.GetOrder(ctx, id)
	if err == nil && cachedOrder != nil {
		log.Printf("Cache HIT for order: %s", id)
		return cachedOrder, nil
	}

	log.Printf("Cache MISS for order: %s", id)
	order, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := u.cache.SetOrder(context.Background(), order, u.ttl); err != nil {
			log.Printf("Failed to cache order %s: %v", order.ID, err)
		}
	}()

	return order, nil
}

func (u *orderUseCase) CancelOrder(ctx context.Context, id string) error {
	o, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if o == nil {
		return errors.New("order not found")
	}

	if o.Status == "Paid" {
		return errors.New("cannot cancel a Paid order")
	}
	if o.Status != "Pending" {
		return errors.New("only Pending orders can be cancelled")
	}

	err = u.repo.UpdateStatus(ctx, id, "Cancelled")
	if err == nil {
// Атомарная инвалидация кэша
		if cacheErr := u.cache.InvalidateOrder(context.Background(), id); cacheErr != nil {
			log.Printf("Failed to invalidate order %s in cache: %v", id, cacheErr)
		}

		u.broker.Publish(id, "Cancelled")
	}

	return err
}

func (u *orderUseCase) GetRevenueByCustomerID(ctx context.Context, customerID string) (*domain.CustomerRevenue, error) {
	if customerID == "" {
		return nil, errors.New("customer_id is required")
	}
	return u.repo.GetRevenueByCustomerID(ctx, customerID)
}
