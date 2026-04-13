package api

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"payment-service/internal/domain"

	desc "github.com/azhenissov/grpc-contracts-go/payment_v1"
)

// PaymentGRPCHandler реализует интерфейс payment_v1.PaymentAPIServer из сгенерированного proto кода
type PaymentGRPCHandler struct {
	desc.UnimplementedPaymentAPIServer
	paymentUC domain.PaymentUseCase
}

func NewPaymentGRPCHandler(paymentUC domain.PaymentUseCase) *PaymentGRPCHandler {
	return &PaymentGRPCHandler{
		paymentUC: paymentUC,
	}
}

// ProcessPayment - реализация RPC метода из payment_v1.proto
// Обрабатывает платежи через Clean Architecture (handler -> usecase -> repository)
func (h *PaymentGRPCHandler) ProcessPayment(ctx context.Context, req *desc.ProcessPaymentRequest) (*desc.ProcessPaymentResponse, error) {
	orderID := req.GetOrderId()
	amount := req.GetAmount()
	log.Printf("[gRPC Method] ProcessPayment: OrderID=%s, Amount=%d", orderID, amount)

	// Validate input
	if orderID == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id must be provided")
	}

	if amount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "amount must be greater than zero")
	}

	// Call usecase to process payment (business logic is preserved)
	// Convert int64 orderID to string for domain layer
	payment, err := h.paymentUC.ProcessPayment(ctx, fmt.Sprintf("%s", orderID), amount)
	if err != nil {
		log.Printf("[gRPC Error] ProcessPayment failed: %v", err)
		return nil, status.Error(codes.Internal, "failed to process payment: "+err.Error())
	}

	return &desc.ProcessPaymentResponse{
		Success:       payment.Status == "Authorized",
		TransactionId: payment.TransactionID,
	}, nil
}
