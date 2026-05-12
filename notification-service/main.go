package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/infrastructure"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
)

// Структура сообщения согласно заданию
type OrderEvent struct {
	OrderID       string  `json:"order_id"`
	Amount        float64 `json:"amount"`
	CustomerEmail string  `json:"customer_email"`
	Status        string  `json:"status"`
}

func main() {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == ""{
		log.Fatal("REDIS_URL is not set")
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	for i := 0; i < 5; i++{
		if err := rdb.Ping(context.Background()).Err(); err == nil {
			log.Println("✓ Redis connected successfully")
			break
		}
		log.Printf("Failed to connect to Redis, retrying in 2s... (%d/5)", i+1)
		time.Sleep(2 * time.Second)
	}
	defer rdb.Close()

	conn, err := amqp.Dial(os.Getenv("RABBITMQ_URL"))
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()


	err = ch.ExchangeDeclare(
		"dlx_exchange", // name
		"direct",       // type
		true,           // durable
		false,          // auto-deleted
		false,          // internal
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare exchange: %v", err)
	}
	//очередь для мертвых сообщений
	_, err = ch.QueueDeclare(
		"death_letter_queue",
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,   // args	
	)
	if err != nil {
		log.Fatalf("Failed to declare dead letter queue: %v", err)
	}
	ch.QueueBind(
		"death_letter_queue", // queue name
		"failed_key",          // routing key
		"dlx_exchange",        // exchange
		false,					// noWait
		nil,					// args
	)

	args := amqp.Table{
		"x-dead-letter-exchange":    "dlx_exchange",
		"x-dead-letter-routing-key": "failed_key",
	}

	q, err := ch.QueueDeclare(
		"payment.completed",
		true, // durable
		false,
		false,
		false,
		args,
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	var emailSender domain.EmailSender
	providerMode := os.Getenv("PROVIDER_MODE")

	if providerMode == "REAL" {
		log.Println("Starting Notification Service with REAL SMTP Provider")
		emailSender = infrastructure.NewSMTPSender(
			os.Getenv("SMTP_HOST"),
			os.Getenv("SMTP_PORT"),
			os.Getenv("SMTP_USER"),
			os.Getenv("SMTP_PASS"),
		)
	} else {
		log.Println("Starting Notification Service with SIMULATED Provider")
		emailSender = infrastructure.NewSimulatedSender()
	}

	maxRetriesStr := os.Getenv("RETRY_MAX_ATTEMPTS")
	maxRetries, err := strconv.Atoi(maxRetriesStr)
	if err != nil || maxRetries <= 0 {
		maxRetries = 3 
	}

	// Идемпотентность In-memory store для отслеживания обработанных ID

	// Потребление сообщений Manual Ack
	msgs, err := ch.Consume(
		q.Name, "", false, false, false, false, nil,
	)
	if err != nil {
		log.Fatalf("Failed to register consumer: %v", err)
	}

	// Канал для Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for d := range msgs {
			var event OrderEvent
			if err := json.Unmarshal(d.Body, &event); err != nil {
				log.Printf("Error decoding message: %v", err)
				d.Nack(false, false) // Отклоняем битые сообщения
				continue
			}

			// Оставляем твой тест для DLQ
			if event.OrderID == "fail" {
				log.Printf("Simulating permanent error for Order #fail")
				if err := d.Nack(false, false); err != nil {
					log.Printf("Error nacking message: %v", err)
				}
				continue
			}

			// --- 1. REDIS IDEMPOTENCY ---
			// Используем SETNX (Set if Not eXists). Если ключ уже есть, isNew будет false.
			idempotencyKey := fmt.Sprintf("processed_order:%s", event.OrderID)
			isNew, err := rdb.SetNX(context.Background(), idempotencyKey, "processed", 24*time.Hour).Result()
			
			if err != nil {
				// Если сам Redis упал, мы не откидываем сообщение в DLQ, а возвращаем в очередь (requeue=true)
				log.Printf("Redis error checking idempotency: %v", err)
				d.Nack(false, true) 
				continue
			}

			if !isNew {
				log.Printf("Duplicate message detected in Redis for Order #%s, skipping...", event.OrderID)
				d.Ack(false)
				continue
			}

			subject := fmt.Sprintf("Update on your Order #%s", event.OrderID)
			body := fmt.Sprintf("Hello, your order status is: %s. Amount: %.2f", event.Status, event.Amount)

			var sendErr error
			maxRetries := 3
			
			for attempt := 1; attempt <= maxRetries; attempt++ {
				sendErr = emailSender.SendEmail(context.Background(), event.CustomerEmail, subject, body)
				
				if sendErr == nil {
					break 
				}

				log.Printf("[Notification] Provider error on attempt %d: %v", attempt, sendErr)
				
				if attempt < maxRetries {
					backoffDuration := time.Duration(1<<attempt) * time.Second 
					log.Printf("Retrying in %v...", backoffDuration)
					time.Sleep(backoffDuration)
				}
			}

			if sendErr != nil {
				log.Printf("Failed to process Order #%s after %d attempts. Sending to DLQ.", event.OrderID, maxRetries)
				
				rdb.Del(context.Background(), idempotencyKey) 
				
				d.Nack(false, false) 
				continue
			}

			
			log.Printf("[Notification] Sent email to %s for Order #%s. Amount: $%.2f", 
				event.CustomerEmail, event.OrderID, event.Amount)

			if err := d.Ack(false); err != nil {
				log.Printf("Error acknowledging: %v", err)
			}
		}
	}()
	log.Printf("Notification Service is running. Waiting for events...")
	<-sigChan 
	log.Println("Shutting down gracefully...")
}