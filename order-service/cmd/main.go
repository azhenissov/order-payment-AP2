package main

import (
	"context"
	"database/sql"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"order-service/internal/api"
	"order-service/internal/repository"
	"order-service/internal/service"
	"order-service/internal/api/middleware"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
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

	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		err := db.Ping()
		if err == nil {
			log.Println("✓ Database connected successfully")
			break
		}
		log.Printf("Failed to connect to database, retrying in 2 seconds... (%d/%d)", i+1, maxRetries)
		if i == maxRetries-1 {
			log.Fatalf("Could not connect to database after %d attempts: %v", maxRetries, err)
		}
		time.Sleep(2 * time.Second)
	}

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

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		log.Println("Warning: REDIS_URL is empty, defaulting to redis:6379")
		redisURL = "redis:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	var redisErr error
	for i := 0; i < 5; i++ {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        redisErr = rdb.Ping(ctx).Err()
        cancel()
        
        if redisErr == nil {
            log.Println("✓ Redis connected successfully")
            break
        }
        log.Printf("Failed to connect to Redis, retrying in 2s... (%d/5)", i+1)
        time.Sleep(2 * time.Second)
    }
	if redisErr != nil {
		log.Fatalf("Could not connect to Redis after %d attempts: %v", 5, redisErr)
	}
	defer rdb.Close()

	ttlMinutesStr := os.Getenv("CACHE_TTL_MINUTES")
    ttlMinutes, err := strconv.Atoi(ttlMinutesStr)
    if err != nil || ttlMinutes <= 0 {
        log.Println("Warning: Invalid CACHE_TTL_MINUTES, defaulting to 5")
        ttlMinutes = 5
    }

	// 3. Setup Clean Architecture Layers
	orderRepo := repository.NewPostgresOrderRepository(db)
	paymentClient := api.NewGRPCPaymentClient(paymentGRPCClient)

	orderCache := repository.NewRedisOrderCache(rdb)
	// Инициализируем наш Брокер (один на всё приложение)
	broker := service.NewOrderBroker()

	// Передаем брокер в UseCase
	orderUC := service.NewOrderUseCase(orderRepo, paymentClient, broker, orderCache, ttlMinutes)



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

	limit := 10
	window := 1*time.Minute

	router.Use(middleware.RateLimiter(rdb, limit, window))

	api.NewOrderHandler(router, orderUC)

	log.Println()
	log.Println("✓ External API: REST (Gin) - for users")
	log.Println("✓ Rate Limiter: 10 requests per minute")
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
