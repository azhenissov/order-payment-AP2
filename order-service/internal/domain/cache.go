package domain

import (
	"context"
	"time"
)

type OrderCache interface {
	SetOrder(ctx context.Context, order *Order, ttl time.Duration) error
	GetOrder(ctx context.Context, id string) (*Order, error)
	InvalidateOrder(ctx context.Context, id string) error
}