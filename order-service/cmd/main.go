package main

import (
	"database/sql"
	"log"
	"net"
	"os"

	"order-service/internal/api"
	"order-service/internal/repository"
	"order-service/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	orderDesc "github.com/azhenissov/grpc-contracts-go/order_v1"
	paymentDesc "github.com/azhenissov/grpc-contracts-go/payment_v1"
)

func main() {
	// Load .env file - try multiple paths
	envPaths := []string{".env", "../.env", "../../order-service/.env"}
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
		log.Fatal("DATABASE_URL environment variable is not set")
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

	// 2. Setup gRPC Client for Payment Service
	paymentGRPCAddr := os.Getenv("PAYMENT_GRPC_ADDRESS")
	if paymentGRPCAddr == "" {
		panic("PAYMENT_GRPC_ADDRESS is not in .env")
	}

	paymentConn, err := grpc.Dial(paymentGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to Payment Service gRPC: %v", err)
	}
	defer paymentConn.Close()
	log.Printf("✓ gRPC Payment Client connected to %s\n", paymentGRPCAddr)

	paymentGRPCClient := paymentDesc.NewPaymentAPIClient(paymentConn)

	// 3. Setup Clean Architecture Layers
	orderRepo := repository.NewPostgresOrderRepository(db)
	paymentClient := api.NewGRPCPaymentClient(paymentGRPCClient)

	// Инициализируем наш Брокер (один на всё приложение)
	broker := service.NewOrderBroker()

	// Передаем брокер в UseCase
	orderUC := service.NewOrderUseCase(orderRepo, paymentClient, broker)

	// 4. Start gRPC Server for Order Service (in separate goroutine)
	grpcPort := os.Getenv("ORDER_GRPC_PORT")
	if grpcPort == "" {
		panic("ORDER_GRPC_PORT is not set in .env")
	}

	lis, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen on gRPC port %s: %v", grpcPort, err)
	}

	grpcServer := grpc.NewServer()

	orderGRPCHandler := api.NewOrderGRPCHandler(orderUC, broker)
	orderDesc.RegisterOrderServiceServer(grpcServer, orderGRPCHandler)

	go func() {
		log.Printf("✓ gRPC Order Service started on port %s\n", grpcPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve gRPC server: %v", err)
		}
	}()

	// 5. Start REST API Server (Gin) for backward compatibility
	restPort := os.Getenv("ORDER_REST_PORT")
	if restPort == "" {
		panic("ORDER_REST_PORT is not set in .env")
	}

	router := gin.Default()
	api.NewOrderHandler(router, orderUC)

	log.Println()
	log.Println("✓ External API: REST (Gin) - for users")
	log.Println("✓ Internal API: gRPC - for service-to-service communication")
	log.Println("✓ Streaming: Server-side streaming for order updates")
	log.Println("  Proto Contracts: github.com/azhenissov/grpc-contracts-go/*_v1")
	log.Println()

	// router.Run блокирует поток, так что WaitGroup здесь не нужен
	log.Printf("✓ REST API Order Service starting on port %s\n", restPort)
	if err := router.Run(restPort); err != nil {
		log.Fatalf("Failed to run REST server: %v", err)
	}
}
