package main

import (
	"encoding/json"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	amqp "github.com/rabbitmq/amqp091-go"
	"notification-service/internal/domain"
	"notification-service/internal/infrastructure"
)

// Структура сообщения согласно заданию
type OrderEvent struct {
	OrderID       string  `json:"order_id"`
	Amount        float64 `json:"amount"`
	CustomerEmail string  `json:"customer_email"`
	Status        string  `json:"status"`
}

func main() {
	// 1. Подключение к RabbitMQ
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

	// Идемпотентность In-memory store для отслеживания обработанных ID
	var processedOrders sync.Map

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


			if event.OrderID == "fail"{
				log.Printf("Simulating permanent error for Order #fail")
				// requeue=false отправляет сообщение в DLQ
				if err := d.Nack(false, false); err != nil {
					log.Printf("Error nacking message: %v", err)
				}
				continue
			}
			// Проверка идемпотентности
			if _, loaded := processedOrders.LoadOrStore(event.OrderID, true); loaded {
				log.Printf("Duplicate message detected for Order #%s, skipping...", event.OrderID)
				d.Ack(false) 
				continue
			}

			subject := fmt.Sprintf("Update on your Order #%s", event.OrderID)
			body := fmt.Sprintf("Hello, your order status is: %s. Amount: %.2f", event.Status, event.Amount)

			// Вызываем отправку письма
			err := emailSender.SendEmail(context.Background(), event.CustomerEmail, subject, body)
			
			if err != nil {
				log.Printf("[Notification] Provider error %v, ", err)
				d.Nack(false, false)
				continue
			}
			//  отправкa email
			log.Printf("[Notification] Sent email to %s for Order #%s. Amount: $%.2f", 
                event.CustomerEmail, event.OrderID, event.Amount)

			// Подтверждение после успешной обработки
			if err := d.Ack(false); err != nil {
				log.Printf("Error acknowledging: %v", err)
			}
		}
	}()

	log.Printf("Notification Service is running. Waiting for events...")
	<-sigChan 
	log.Println("Shutting down gracefully...")
}