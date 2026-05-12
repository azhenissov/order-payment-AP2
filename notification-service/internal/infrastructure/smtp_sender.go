package infrastructure

import (
	"context"
	"fmt"
	"net/smtp"

	"notification-service/internal/domain"	
)

type smtpSender struct {
	host string
	port string
	user string
	pass string
}

func NewSMTPSender(host, port, user, pass string) domain.EmailSender {
	return &smtpSender{
		host: host,
		port: port,
		user: user,
		pass: pass,
	}
}

func (s *smtpSender) SendEmail(ctx context.Context, to, subject, body string) error {
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)

	msg := []byte(fmt.Sprintf("To: %s\r\nSubject: %s\r\n\r\n%s", to, subject, body))

	addr := fmt.Sprintf("%s:%s", s.host, s.port)

	err := smtp.SendMail(addr, auth, s.user, []string{to}, msg)
	if err != nil {
		return fmt.Errorf("failed to send email via SMTP: %w", err)
	}
	
	return nil
}


