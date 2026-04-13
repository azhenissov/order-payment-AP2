package middleware

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
)

// LoggingUnaryInterceptor - LoggingUnaryInterceptor логирует все gRPC унарные запросы
// Это реализация требования Bonus (+10%) для middleware
func LoggingUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()

	// Call handler
	resp, err := handler(ctx, req)

	// Log the request details
	duration := time.Since(start)
	log.Printf(
		"[gRPC] Method=%s | Duration=%dms | Error=%v",
		info.FullMethod,
		duration.Milliseconds(),
		err,
	)

	return resp, err
}

// LoggingStreamInterceptor - для streaming RPC (опционально)
func LoggingStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()

	err := handler(srv, ss)

	duration := time.Since(start)
	log.Printf(
		"[gRPC Stream] Method=%s | Duration=%dms | Error=%v",
		info.FullMethod,
		duration.Milliseconds(),
		err,
	)

	return err
}
