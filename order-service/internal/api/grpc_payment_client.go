package api

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc/status"

	"order-service/internal/domain"

	paymentDesc "github.com/azhenissov/grpc-contracts-go/payment_v1"
)

type GRPCPaymentClient struct {
	client paymentDesc.PaymentAPIClient
}

func NewGRPCPaymentClient(client paymentDesc.PaymentAPIClient) domain.PaymentClient {
	return &GRPCPaymentClient{
		client: client,
	}
}

func (c *GRPCPaymentClient) AuthorizePayment(ctx context.Context, orderID string, amount int64) (string, error) {
	log.Printf("[gRPC Client] Calling Payment Service: AuthorizePayment(orderID=%s, amount=%d)", orderID, amount)

	req := &paymentDesc.ProcessPaymentRequest{
		OrderId: orderID,
		Amount:  amount,
	}

	resp, err := c.client.ProcessPayment(ctx, req)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			log.Printf("[gRPC Error] ProcessPayment failed: Code=%s, Message=%s", st.Code(), st.Message())
		}
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("payment authorization failed")
	}

	log.Printf("[gRPC Response] ProcessPayment successful: TxID=%s", resp.TransactionId)
	return resp.TransactionId, nil
}
