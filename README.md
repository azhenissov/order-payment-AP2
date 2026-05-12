# Order & Payment Microservices (Assignment 4)

Production-ready microservices architecture with a focus on high performance, reliability, and external integrations. 

## Technologies
* **Language:** Go (Golang)
* **Databases:** PostgreSQL (per-service)
* **Message Broker:** RabbitMQ
* **Cache & Shared State:** Redis
* **Communication:** gRPC & REST (Gin)

---

## Architectural Decisions & Patterns

### Caching Strategy (Order Service)
Implemented the **Cache-aside** pattern with **Atomic Invalidation** to reduce database load and guarantee data consistency.
* **Read Path:** `GET /orders/:id` checks Redis first. On a Cache MISS, it fetches from PostgreSQL and asynchronously writes to Redis with a TTL of 5 minutes.
* **Write Path (Invalidation):** Any state change (`Create`, `Checkout`, `Cancel`) triggers an immediate and atomic cache invalidation (`rdb.Del`). This strictly prevents serving stale data (e.g., showing "Pending" for an already "Paid" order).

### Reliable Background Worker (Notification Service)
The Notification Service acts as an asynchronous consumer, decoupled from the main request path.
* **Idempotency:** Utilizes Redis `SETNX` (Set if Not eXists) with a 24h TTL. This guarantees that duplicate events (e.g., RabbitMQ at-least-once delivery) will not trigger multiple emails to the customer.
* **Exponential Backoff & Retries:** Temporary provider failures are handled gracefully. The worker attempts to resend the email up to 3 times with exponentially increasing delays (2s, 4s, 8s). 
* **Dead Letter Queue (DLQ):** If the external provider remains unavailable after max retries, or if a critical error occurs, the message is routed to a `dlx_exchange` via `NACK (requeue=false)` for manual inspection without blocking the main queue.

### External Integration (Adapter Pattern)
Notification logic is decoupled using the `EmailSender` interface. 
* Configurable via `.env` (`PROVIDER_MODE`).
* Switches seamlessly between a **REAL** SMTP integration (Mailjet) and a **SIMULATED** mock provider that artificially introduces network latency and random failures to validate the retry policies.

### API Rate Limiter
Implemented a Redis-based **Fixed Window** Rate Limiter middleware for the Order Service REST API. 
* Limits requests to `10 per minute` per client IP using atomic `INCR` and `EXPIRE` operations.
* Returns `HTTP 429 Too Many Requests` when limits are exceeded, showcasing Redis as a fast, shared state store for distributed systems.

---

## How to Run

1. Ensure Docker and Docker Compose are installed.
2. Clone the repository and configure the `.env` files for each service.
3. Start the infrastructure and services:
```bash
docker-compose up -d --build

Access the API via http://localhost:8080/orders and RabbitMQ Management UI at http://localhost:15672