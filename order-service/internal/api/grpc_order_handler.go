package api

import (
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	desc "github.com/azhenissov/grpc-contracts-go/order_v1"
	
	"order-service/internal/domain"
	"order-service/internal/service" 
)

type OrderGRPCHandler struct {
	desc.UnimplementedOrderServiceServer 
	useCase domain.OrderUseCase
	broker  *service.OrderBroker
}

func NewOrderGRPCHandler(useCase domain.OrderUseCase, broker *service.OrderBroker) *OrderGRPCHandler {
	return &OrderGRPCHandler{
		useCase: useCase,
		broker:  broker,
	}
}

func (h *OrderGRPCHandler) SubscribeToOrderUpdates(req *desc.OrderSubscriptionRequest, stream desc.OrderService_SubscribeToOrderUpdatesServer) error {
	orderID := req.GetOrderId()
	
	if orderID == "" {
		return status.Error(codes.InvalidArgument, "order_id is required")
	}

	log.Printf("Client subscribed to updates for OrderID=%s", orderID)

	updateCh := h.broker.Subscribe(orderID)
	
	defer h.broker.Unsubscribe(orderID, updateCh)

	//  stream
	for {
		select {
		case <-stream.Context().Done():
			log.Printf("Client unsubscribed from updates for OrderID=%s", orderID)
			return nil

		case update, ok := <-updateCh:
			if !ok {
				return nil
			}

			resp := &desc.OrderStatusUpdate{
				OrderId: update.OrderID,
				Status:  update.Status,
			}
			
			log.Printf("Sending status %s for order %s in stream", update.Status, update.OrderID)

			if err := stream.Send(resp); err != nil {
				log.Printf("Error sending to stream for order %s: %v", orderID, err)
				return status.Error(codes.Internal, "error sending data to stream: "+err.Error())
			}
		}
	}
}