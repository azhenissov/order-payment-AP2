package domain

import (
	"context"
)

type EmailSender interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}