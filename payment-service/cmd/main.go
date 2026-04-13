package main

import (
	"database/sql"
	"log"
	"net"
	"os"

	"payment-service/internal/api"
	"payment-service/internal/api/middleware"
	"payment-service/internal/repository"
	"payment-service/internal/service"

	desc "github.com/azhenissov/grpc-contracts-go/payment_v1"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"
)

func main() {
	// Load .env file - try multiple paths
	envPaths := []string{".env", "../.env", "../../payment-service/.env"}
	loaded := false
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			loaded = true
			log.Printf("Loaded .env from: %s\n", path)
			break
		}
	}
	if !loaded {
		log.Println("Warning: Could not load .env file from any location")
	}

	// 1. Setup Database Connection
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		panic("Database URL is not in .env")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("✓ Database connected successfully")

	// 2. Setup Clean Architecture Layers
	paymentRepo := repository.NewPostgresPaymentRepository(db)
	paymentUC := service.NewPaymentUseCase(paymentRepo)

	// 3. Configure gRPC Server with Middleware (Interceptors)
	port := os.Getenv("PAYMENT_GRPC_PORT")
	if port == "" {
		panic("PAYMENT_GRPC_PORT is not in .env")
	}

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	// Add UnaryInterceptor for logging and middleware
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(middleware.LoggingUnaryInterceptor),
	)

	paymentHandler := api.NewPaymentGRPCHandler(paymentUC)
	desc.RegisterPaymentAPIServer(grpcServer, paymentHandler)

	log.Printf("✓ gRPC Payment Service started on port %s\n", port)
	log.Println("  Proto contracts: github.com/azhenissov/grpc-contracts-go/payment_v1")
	log.Println("  Documentation: See README.md for details")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC server: %v", err)
	}
}
