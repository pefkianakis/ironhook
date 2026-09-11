# Reliable Webhook Delivery Platform — Architecture

## 1. Architecture Overview

The initial system consists of two independently deployable Go processes sharing PostgreSQL:

```text
                         Producer
                            |
                            | HTTP
                            v
                    ┌───────────────┐
                    │  API Process  │
                    └───────┬───────┘
                            |
                            | SQL
                            v
                    ┌───────────────┐
                    │  PostgreSQL   │
                    └───────┬───────┘
                            |
                            | claim deliveries
                            v
                    ┌───────────────┐
                    │ Worker Process│
                    └───────┬───────┘
                            |
                            | signed HTTP
                            v
                    Customer Endpoint
```

The codebase is modular, but the API and worker can be deployed and scaled independently.

This is intentionally not decomposed into multiple microservices.

---

## 2. Why PostgreSQL Is the Initial Queue

PostgreSQL acts as both:

1. durable source of truth;
2. durable delivery work queue.

Kafka, RabbitMQ, Redis, and similar infrastructure are deliberately excluded from V1.

Reasons:

- events and resulting deliveries require transactional atomicity;
- current throughput targets do not justify a separate broker;
- PostgreSQL provides durable storage and row-level locking;
- `FOR UPDATE SKIP LOCKED` enables concurrent worker claiming;
- operational complexity remains low.

A message broker may be introduced later if database-based scheduling becomes a demonstrated bottleneck.

---

## 3. Major Components

### API Process

Responsibilities:

- endpoint management
- subscription management
- event validation
- idempotency enforcement
- event persistence
- subscription resolution
- delivery creation
- query APIs
- replay requests

The API does not perform customer webhook delivery synchronously.

---

### PostgreSQL

Stores:

- endpoints
- subscriptions
- events
- idempotency records
- deliveries
- delivery attempts

It also coordinates concurrent worker ownership.

---

### Delivery Worker

Responsibilities:

- claim eligible deliveries
- maintain bounded concurrency
- create webhook envelopes
- sign requests
- execute HTTP requests
- classify outcomes
- record attempts
- calculate retries
- update delivery state
- participate in endpoint-level health handling

---

### Customer Endpoint

The webhook destination is outside the platform's transactional boundary.

The platform cannot know with certainty whether a receiver committed side effects when an HTTP response is lost.

This is the fundamental reason delivery semantics are at-least-once.

---

## 4. Event Acceptance

Event acceptance performs subscription fan-out inside the same transaction as event persistence.

```text
POST /v1/events
       |
       v
validate request
       |
       v
resolve idempotency key
       |
       v
BEGIN
       |
       ├── create idempotency record
       ├── create Event
       ├── resolve active subscriptions
       ├── create Delivery A
       ├── create Delivery B
       └── create Delivery C
       |
COMMIT
       |
       v
202 Accepted
```

The transaction establishes a stable delivery contract.

Configuration changes occurring after acceptance do not retroactively change the recipients of an accepted event.

---

## 5. Domain Model

### Endpoint

Represents a webhook destination.

Conceptual fields:

```text
id
url
secret
status
created_at
updated_at
```

Administrative state is separate from runtime health state.

---

### Subscription

Represents interest by an endpoint in an event type.

```text
id
endpoint_id
event_type
status
created_at
```

Conceptual constraint:

```text
UNIQUE(endpoint_id, event_type)
```

---

### Event

Represents a logical event accepted from a producer.

```text
id
type
payload
created_at
```

`payload` is stored as JSON because the platform does not own producer business schemas.

---

### Delivery

Represents the responsibility to deliver one event to one endpoint.

```text
id
event_id
endpoint_id

status

attempt_count
next_attempt_at
retry_until

claimed_at
claim_version

replay_of_delivery_id

created_at
delivered_at
```

`Event` and `Delivery` are intentionally separate because one event can fan out to multiple endpoints.

---

### DeliveryAttempt

Represents one network attempt.

```text
id
delivery_id
attempt_number

started_at
finished_at
duration

http_status
error
```

---

### Idempotency Record

Represents the temporary binding between a producer-generated idempotency key and an accepted event.

Conceptually:

```text
scope
key
request_hash
event_id
created_at
expires_at
```

The idempotency lifecycle is separate from the event lifecycle.

---

## 6. Worker Claiming

Workers must not use:

```text
SELECT pending
→ send
→ UPDATE
```

because multiple workers could select the same delivery.

Instead, claiming uses a short transaction based on:

```sql
SELECT ...
FROM deliveries
WHERE ...
FOR UPDATE SKIP LOCKED;
```

Eligible rows are marked `DELIVERING` before commit.

Network calls happen after the transaction completes.

This prevents database locks from being held while waiting on external systems.

---

## 7. Claim Leases and Crash Recovery

A delivery claim is a lease rather than permanent ownership.

Conceptually:

```text
status = DELIVERING
claimed_at = ...
claim_version = N
```

If the worker disappears and the claim becomes stale, another worker may reclaim it.

The new claim increments:

```text
claim_version = N + 1
```

This prevents abandoned work from being permanently stranded.

---

## 8. Fencing Stale Workers

Every state-changing worker update must verify its claim version.

Conceptually:

```sql
UPDATE deliveries
SET ...
WHERE id = $1
  AND status = 'DELIVERING'
  AND claim_version = $2;
```

If zero rows are updated, the worker no longer owns the delivery.

Example:

```text
Worker A claims version 17

lease expires

Worker B claims version 18
Worker B completes delivery

Worker A wakes up
Worker A tries update with version 17

→ rejected
```

This protects internal database state from stale workers.

It does not prevent a stale worker's HTTP request from reaching the receiver.

Therefore fencing does not provide exactly-once delivery.

---

## 9. Delivery Semantics

The platform provides **at-least-once delivery**.

Possible duplicate:

```text
Worker
  |
  | POST webhook
  v
Receiver commits operation
  |
  X response lost
  |
Worker retries
```

The platform therefore sends stable identifiers including:

```text
event ID
delivery ID
```

The receiver should use the logical event identifier where business-level deduplication is required.

---

## 10. Retry Architecture

Retryable failures return a delivery to:

```text
PENDING
```

with:

```text
next_attempt_at = calculated future time
```

Retry scheduling uses:

```text
exponential backoff
+
jitter
+
maximum backoff
+
absolute retry horizon
```

The retry horizon uses wall-clock time.

Suspending an endpoint does not pause the retry clock.

This keeps retry semantics predictable and prevents indefinite retention.

---

## 11. Endpoint-Level Failure Handling

The system distinguishes delivery-level failures from endpoint-level failures.

### Transient endpoint health

Examples:

```text
timeouts
502
503
504
```

Repeated failures may move the endpoint circuit:

```text
CLOSED
   ↓
OPEN
   ↓
HALF-OPEN
   ↓
CLOSED
```

The half-open state allows a limited number of real pending deliveries through as recovery probes.

---

### Configuration failures

Examples:

```text
repeated 401
persistent invalid authentication configuration
```

These may move the endpoint administrative status:

```text
ACTIVE → SUSPENDED
```

Queued deliveries remain durable but are not eligible while the endpoint is suspended.

---

### Endpoint lifecycle failure

A `410 Gone` indicates a stronger permanent signal and may result in endpoint disablement.

`404 Not Found` is treated less aggressively because it does not provide the same permanence semantics.

---

## 12. Outgoing Webhook Format

The platform sends a stable webhook envelope.

Example:

```json
{
  "id": "evt_...",
  "type": "order.completed",
  "created_at": "2026-09-08T19:30:00Z",
  "data": {
    "order_id": "ORD-123"
  }
}
```

Metadata headers include conceptual equivalents of:

```http
Webhook-Event-Id: evt_...
Webhook-Delivery-Id: del_...
Webhook-Timestamp: ...
Webhook-Signature: v1=...
```

The HMAC signature is generated from the exact body bytes transmitted.

---

## 13. Replay

Replay creates new work rather than rewriting history.

```text
Original Delivery D1
status = FAILED

Operator requests replay

New Delivery D2
replay_of_delivery_id = D1
status = PENDING
```

Attempts associated with D1 remain unchanged.

This preserves auditability.

---

## 14. Scaling Model

API and worker processes scale independently.

```text
                    ┌── API 1 ──┐
Producer ───────────┼── API 2 ──┼──── PostgreSQL
                    └── API 3 ──┘        |
                                         |
                          ┌──────────────┼──────────────┐
                          |              |              |
                       Worker 1       Worker 2       Worker 3
```

PostgreSQL coordinates workers through row locking and claim leases.

Worker concurrency must be bounded.

Possible future extensions include per-endpoint concurrency limits so one destination cannot consume all available worker capacity.

---

## 15. Initial Repository Structure

```text
webhook-platform/
│
├── cmd/
│   ├── api/
│   │   └── main.go
│   └── worker/
│       └── main.go
│
├── internal/
│   ├── endpoint/
│   │   ├── endpoint.go
│   │   ├── service.go
│   │   └── repository.go
│   │
│   ├── event/
│   │   ├── event.go
│   │   ├── service.go
│   │   └── repository.go
│   │
│   ├── delivery/
│   │   ├── delivery.go
│   │   ├── worker.go
│   │   ├── retry.go
│   │   └── repository.go
│   │
│   ├── webhook/
│   │   ├── signer.go
│   │   └── client.go
│   │
│   ├── postgres/
│   │   ├── endpoint.go
│   │   ├── event.go
│   │   └── delivery.go
│   │
│   └── httpapi/
│       ├── handler.go
│       ├── endpoints.go
│       ├── events.go
│       └── deliveries.go
│
├── migrations/
│
├── docs/
│   ├── requirements.md
│   ├── architecture.md
│   └── adr/
│
├── docker-compose.yml
├── go.mod
└── README.md
```

The exact package split may evolve.

Packages should primarily represent domain and infrastructure boundaries rather than generic folders such as:

```text
controllers/
services/
utils/
models/
```

---

## 16. Dependency Direction

Infrastructure depends on application/domain abstractions rather than the domain depending on PostgreSQL or HTTP implementation details.

Conceptually:

```text
HTTP API ───────┐
                v
          Domain/Application
                ^
                |
PostgreSQL ─────┤
                |
Webhook HTTP ───┘
```

Interfaces should be introduced only at meaningful boundaries, such as:

- persistence;
- outbound webhook transport;
- clock where deterministic testing requires it;
- ID generation where appropriate.

The codebase should avoid creating interfaces merely for abstraction's sake.

---

## 17. Architectural Trade-Offs

### PostgreSQL instead of Kafka

Advantages:

- simpler operations
- atomic fan-out
- fewer infrastructure dependencies
- strong transaction semantics

Trade-off:

- database polling/claiming eventually has throughput limits.

Kafka or another broker should only be introduced when measurements demonstrate a need.

---

### At-least-once instead of exactly-once

Advantage:

- provides honest semantics across an unreliable HTTP boundary.

Trade-off:

- receivers must tolerate duplicates.

Exactly-once HTTP side effects cannot be guaranteed without cooperation from the receiving system.

---

### Immediate subscription resolution

Advantage:

- accepted events have a stable recipient set.

Trade-off:

- a subscription deleted immediately after event acceptance may still receive that accepted event.

This behavior is intentional.

---

### Finite retry horizon

Advantage:

- bounded automatic backlog and predictable retention.

Trade-off:

- long endpoint outages may require later manual replay.

---

### Separate API and Worker Processes

Advantages:

- independent scaling
- workload isolation
- worker failures do not directly stop event ingestion

Trade-off:

- slightly more operational complexity than one combined executable.

---

## 18. Future Evolution Triggers

A new infrastructure component should be introduced because of measured system pressure rather than architectural fashion.

Potential triggers include:

### Message broker

Consider Kafka/RabbitMQ when:

- PostgreSQL claim polling becomes a material bottleneck;
- required throughput materially exceeds database queue capabilities;
- event-stream consumers beyond webhook delivery are required.

### Redis

Consider Redis when:

- distributed endpoint rate limiting becomes difficult through current mechanisms;
- circuit-breaker state requires very high-frequency shared access;
- caching demonstrates measurable value.

### Per-endpoint concurrency management

Introduce when:

- individual destinations can dominate global worker capacity;
- customer-specific delivery limits become required.

### Multi-region architecture

Consider only when explicit availability or latency requirements justify the complexity.