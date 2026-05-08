package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"order-service/internal/domain"

	"github.com/redis/go-redis/v9"
)

type redisOrderCache struct {
	client *redis.Client
}

// Конструктор
func NewRedisOrderCache(client *redis.Client) domain.OrderCache {
	return &redisOrderCache{
		client: client,
	}
}

// Вспомогательная функция для генерации ключа
func orderKey(id string) string {
	return fmt.Sprintf("order:%s", id)
}

// Сохранение в кэш с TTL [cite: 30]
func (r *redisOrderCache) SetOrder(ctx context.Context, order *domain.Order, ttl time.Duration) error {
	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order for cache: %w", err)
	}

	err = r.client.Set(ctx, orderKey(order.ID), data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set cache: %w", err)
	}

	return nil
}

// Получение из кэша
func (r *redisOrderCache) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	val, err := r.client.Get(ctx, orderKey(id)).Result()
	
	if err == redis.Nil {
		// Ключ не найден (Cache MISS) — это нормальная ситуация, возвращаем nil без ошибки
		return nil, nil
	} else if err != nil {
		// Реальная ошибка Redis
		return nil, fmt.Errorf("redis get error: %w", err)
	}

	var order domain.Order
	if err := json.Unmarshal([]byte(val), &order); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached order: %w", err)
	}

	return &order, nil
}

// Атомарная инвалидация (удаление) ключа 
func (r *redisOrderCache) InvalidateOrder(ctx context.Context, id string) error {
	err := r.client.Del(ctx, orderKey(id)).Err()
	if err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}
	return nil
}