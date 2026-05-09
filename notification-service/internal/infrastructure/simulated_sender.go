package infrastructure

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"time"

	"notification-service/internal/domain"
)

type simulatedSender struct {}

func NewSimulatedSender() domain.EmailSender {
	return &simulatedSender{}
}

func (s *simulatedSender) SendEmail(ctx context.Context, to, subject, body string) error {
	log.Printf("[SimulatedSender] Preparing to send email to %s...", to)

	latency := time.Duration(rand.Intn(2000)+ 1000) * time.Millisecond
	time.Sleep(latency)

	if rand.Float32() < 0.3 {
		log.Printf("[SimulatedSender] FAILED to send email to %s (simulated 503 error)", to)
		return errors.New("simulated external provider error: timeout or 503 Service Unavailable")
	}

	log.Printf("[SimulatedSender] SUCCESS: Email sent to %s", to)
	return nil
}