# Gorder

A distributed microservices order processing system built with Go, demonstrating DDD (Domain-Driven Design) and hexagonal architecture patterns.

## Architecture

```
                          +------------------+
                          |    Consul        |
                          | Service Registry |
                          +--------+---------+
                                   |
           +-----------+-----------+-----------+
           |           |           |           |
    +------+------+  +-+--------+ +----+----+ +------+------+
    | Order       |  | Stock    | | Payment | | Kitchen     |
    | HTTP + gRPC |  | gRPC     | | HTTP    | | Consumer    |
    | :9090/:5002 |  | :5003    | | :9092   | | (no port)   |
    +------+------+  +----+-----+ +----+----+ +------+------+
           |              |            |              |
           +-----------+--+---------+--+-----------+--+
                       |            |              |
                  +----+----+  +---+----+   +-----+-----+
                  | MongoDB |  | MySQL  |   | RabbitMQ  |
                  | Orders  |  | Stock  |   | Events    |
                  +---------+  +--------+   +-----------+
                                    |
                              +-----+-----+
                              |   Redis   |
                              | Flash Sale|
                              +-----------+
```

**Services:**

| Service | Transport | Port | Responsibility |
|---------|-----------|------|----------------|
| Order | HTTP + gRPC | 9090 / 5002 | Order lifecycle, flash sale endpoints |
| Stock | gRPC | 5003 | Inventory management, flash sale warmup |
| Payment | HTTP | 9092 | Stripe payment, webhook processing |
| Kitchen | MQ Consumer | - | Order preparation workflow |

**Communication:**
- **Synchronous:** gRPC for stock verification, order queries
- **Asynchronous:** RabbitMQ events for state transitions (order.created, order.paid, order.payment.timeout, order.refund)

## Tech Stack

| Category | Technology |
|----------|-----------|
| Language | Go 1.25 |
| HTTP Framework | Gin |
| RPC | gRPC + Protocol Buffers |
| Databases | MongoDB (orders), MySQL (stock) |
| Cache | Redis |
| Message Queue | RabbitMQ |
| Payment | Stripe |
| Service Discovery | Consul |
| Tracing | Jaeger (OpenTelemetry) |
| Metrics | Prometheus + Grafana |
| Logging | Logrus (structured) |
| Config | Viper |

## Project Structure

Each service follows hexagonal architecture (ports & adapters):

```
internal/
├── common/                     # Shared modules
│   ├── broker/                 # RabbitMQ event publishing & consuming
│   ├── client/                 # gRPC client wrappers
│   ├── config/                 # Viper config (global.yaml)
│   ├── convertor/              # Entity <-> Proto converters
│   ├── decorator/              # Command/Query decorator (logging, metrics, tracing)
│   ├── discovery/              # Consul service discovery
│   ├── entity/                 # Shared domain entities
│   ├── genproto/               # Generated protobuf code
│   ├── handler/                # Redis client, error handling, singleton factory
│   ├── logging/                # Structured logging (gRPC, MySQL, HTTP)
│   ├── metrics/                # Prometheus metrics client
│   ├── middleware/             # Gin middleware (request log, Prometheus metrics)
│   ├── server/                 # HTTP / gRPC server bootstrap
│   └── tracing/                # Jaeger / OpenTelemetry setup
│
├── order/                      # Order Service
│   ├── domain/order/           # Aggregate root, repository interface, status machine
│   ├── app/                    # Application layer
│   │   ├── command/            # CreateOrder, UpdateOrder, CancelOrder
│   │   ├── query/              # GetCustomerOrder, StockService interface
│   │   └── dto/                # Data transfer objects
│   ├── adapters/               # Repository impl (MongoDB, in-memory), gRPC adapters
│   ├── ports/                  # gRPC server, OpenAPI generated handlers
│   ├── infra/consumer/         # RabbitMQ consumers (order.paid, flash sale)
│   ├── http.go                 # Flash sale HTTP handlers
│   ├── service/                # Dependency injection / application bootstrap
│   └── main.go
│
├── stock/                      # Stock Service
│   ├── domain/stock/           # Repository interface, domain errors
│   ├── app/                    # CheckIfItemsInStock, GetItems, RestoreStock, WarmUpFlashStock, DeductStock
│   ├── adapters/               # MySQL repo, in-memory repo
│   ├── infra/
│   │   ├── persistent/         # MySQL queries + SQL builder
│   │   └── integration/        # Stripe product API
│   ├── ports/                  # gRPC server
│   ├── service/                # Bootstrap
│   └── main.go
│
├── payment/                    # Payment Service
│   ├── domain/                 # Payment entity
│   ├── app/command/            # CreatePayment, RefundPayment
│   ├── adapters/               # Order gRPC client
│   ├── infra/
│   │   ├── consumer/           # RabbitMQ consumer (order.created)
│   │   └── processor/          # Stripe processor, in-memory processor
│   ├── http.go                 # Stripe webhook handler
│   ├── service/                # Bootstrap
│   └── main.go
│
└── kitchen/                    # Kitchen Service
    ├── adapters/               # Order gRPC client
    ├── infra/consumer/         # RabbitMQ consumer (order.paid)
    └── main.go
```

## Key Features

### Order Flow

```
Client ──POST──> Order Service ──gRPC──> Stock Service (check & deduct)
                      │
                      ├── Stripe checkout link generated
                      ├── Publish order.created ──> Payment Service creates Stripe session
                      ├── Publish to delay queue (TTL) for payment timeout
                      │
                 [User pays via Stripe]
                      │
              Stripe webhook ──> Payment Service
                      │
                      ├── Publish order.paid ──> Order Service (status → PAID)
                      │                    └──> Kitchen Service (prepare order)
                      │
              Kitchen done ──gRPC──> Order Service (status → READY)
```

### Flash Sale

Prevents overselling using Redis Lua atomic script, plus a flash-sale-only one-user-one-order guard:

```
1. Warmup:  POST /flash-sale/warmup
            └── gRPC WarmUpFlashStock → Redis SET flash:stock:{id} = quantity

2. Purchase: POST /flash-sale/orders
             ├── Redis SETNX flash:once:{customer}:{item}  (one user, one flash-sale order)
             ├── Redis Lua DECRBY flash:stock:{id}         (atomic stock pre-deduct)
             ├── On success → publish to flash.order.created queue
             ├── On Redis/MQ failure → rollback flash:once key + pre-deducted Redis stock
             └── Return token for polling

3. Async create:
             └── Flash sale consumer → CreateFlashOrder
                 └── Stock gRPC DeductStock (CAS) → MySQL deduct → MongoDB create order

4. Result:  GET /flash-sale/result/{token}
            └── Read flash:result:{token} from Redis
```

### Payment Timeout (Dead Letter Queue)

```
CreateOrder → message to delay queue (TTL: 15 min)
                      │
              TTL expires
                      │
              DLX forwards to timeout queue
                      │
              Consumer → CancelOrder + RestoreStock
```

### Flash-sale Deduplication

Flash sale uses a dedicated Redis `SetNX` key (`flash:once:{customer}:{item}`), so "one user, one order" applies only within the flash-sale flow.

## Getting Started

### Prerequisites

- Go 1.25+
- Docker & Docker Compose
- Stripe account (for payment features)
- `protoc` compiler + Go plugins (for proto generation)
- `oapi-codegen` (for OpenAPI generation)

### 1. Start Infrastructure

```bash
docker compose up -d
```

This starts: Consul, RabbitMQ, Jaeger, MongoDB, MySQL, Redis, Prometheus, Grafana.

### 2. Set Environment Variables

```bash
export STRIPE_KEY="sk_test_..."
export ENDPOINT_STRIPE_SECRET="whsec_..."
```

### 3. Run Services

Open 4 terminals:

```bash
# Terminal 1 - Order Service
cd internal/order && go run .

# Terminal 2 - Stock Service
cd internal/stock && go run .

# Terminal 3 - Payment Service
cd internal/payment && go run .

# Terminal 4 - Kitchen Service
cd internal/kitchen && go run .
```

### 4. Create an Order

```bash
curl -X POST http://localhost:9090/api/customer/u001/orders \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "u001",
    "items": [
      {"id": "prod_U9k6VcIEwQb83T", "quantity": 1}
    ]
  }'
```

## API Reference

### Order Service (HTTP :9090)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/customer/{customer_id}/orders` | Create order |
| GET | `/api/customer/{customer_id}/orders/{order_id}` | Get order |
| POST | `/flash-sale/warmup` | Pre-warm flash sale stock to Redis |
| POST | `/flash-sale/orders` | Submit flash sale order |
| GET | `/flash-sale/result/{token}` | Poll flash sale result |
| GET | `/metrics` | Prometheus metrics |

### Stock Service (gRPC :5003)

| RPC | Description |
|-----|-------------|
| `CheckIfItemsInStock` | Check availability and deduct stock |
| `GetItems` | Get item details |
| `RestoreStock` | Restore stock on cancellation |
| `WarmUpFlashStock` | Pre-load flash sale stock to Redis |
| `DeductStock` | Pure CAS deduction for flash sale orders |

### Payment Service (HTTP :9092)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/webhook` | Stripe webhook receiver |
| GET | `/metrics` | Prometheus metrics |

## Event-Driven Communication

| Event | Type | Producer | Consumer |
|-------|------|----------|----------|
| `order.created` | Direct | Order | Payment |
| `order.paid` | Fanout | Payment | Order, Kitchen |
| `order.payment.timeout` | DLX | RabbitMQ TTL | Order |
| `order.refund` | Direct | Order | Payment |
| `flash.order.created` | Direct | Order (HTTP) | Order (Consumer) |

## Observability

### Distributed Tracing (Jaeger)

Open [http://localhost:16686](http://localhost:16686) to view traces across services. Tracing is auto-instrumented for HTTP (otelgin), gRPC (otelgrpc), and RabbitMQ (B3 header propagation).

### Metrics (Prometheus + Grafana)

- Prometheus: [http://localhost:9093](http://localhost:9093)
- Grafana: [http://localhost:3000](http://localhost:3000) (admin/admin)

Useful PromQL queries:

```promql
# QPS by endpoint
sum(rate(http_requests_total[1m])) by (path, method)

# P99 latency
histogram_quantile(0.99,
  sum(rate(http_request_duration_seconds_bucket[1m])) by (path, le)
)

# Average response time
sum(rate(http_request_duration_seconds_sum[1m])) by (path)
/
sum(rate(http_request_duration_seconds_count[1m])) by (path)

# Error rate
sum(rate(http_requests_total{status=~"5.."}[1m])) by (path)
/
sum(rate(http_requests_total[1m])) by (path)
```

### Service Management UIs

| Service | URL |
|---------|-----|
| Consul | [http://localhost:8500](http://localhost:8500) |
| RabbitMQ | [http://localhost:15672](http://localhost:15672) (guest/guest) |
| Jaeger | [http://localhost:16686](http://localhost:16686) |
| Mongo Express | [http://localhost:8092](http://localhost:8092) |
| Grafana | [http://localhost:3000](http://localhost:3000) (admin/admin) |
| Prometheus | [http://localhost:9093](http://localhost:9093) |

## Development

```bash
# Generate proto and OpenAPI code
make gen

# Format code
make fmt

# Run linter
make lint
```

## Database Schema

### MySQL (Stock)

```sql
CREATE TABLE o_stock (
    id         INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    product_id VARCHAR(255) NOT NULL,
    quantity   INT UNSIGNED NOT NULL DEFAULT 0,
    version    INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB;
```

Stock updates use optimistic locking via the `version` field.

### MongoDB (Orders)

Orders are stored as documents in the `order.orders` collection with fields: ID, CustomerID, Status, Items, PaymentLink.
