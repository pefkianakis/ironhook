# Reliable Webhook Delivery Platform — Requirements

## 1. Purpose

The Reliable Webhook Delivery Platform accepts events from producers, persists them durably, determines which registered webhook endpoints are subscribed to those events, and delivers the events asynchronously over HTTP.

The platform owns delivery reliability concerns including:

- durable event acceptance
- subscription-based fan-out
- retries
- exponential backoff and jitter
- webhook signing
- delivery history
- endpoint failure handling
- concurrency control
- replay
- observability

The platform does not control the receiving application's business logic or database.

---

## 2. Core Reliability Contract

Once the platform returns `202 Accepted` for an event:

1. the event has been persisted durably;
2. the subscriptions applicable at acceptance time have been resolved;
3. the resulting delivery records have been persisted;
4. the platform assumes responsibility for attempting those deliveries according to its retry policy.

The platform provides **at-least-once delivery semantics**.

Exactly-once delivery is not guaranteed because an HTTP request may be processed successfully by the receiver while the delivery platform fails to observe the response.

Receivers must therefore be capable of handling duplicate deliveries.

---

## 3. Functional Requirements

### FR-01 — Endpoint Registration

Clients must be able to register webhook endpoints.

An endpoint contains at minimum:

- identifier
- URL
- signing secret
- administrative status
- creation timestamp
- update timestamp

Endpoint administrative states initially include:

- `ACTIVE`
- `SUSPENDED`
- `DISABLED`

Signing secrets must not be returned through ordinary endpoint retrieval APIs.

---

### FR-02 — Subscription Management

Webhook endpoints must be able to subscribe to event types.

Example:

```text
Endpoint A → order.completed
Endpoint A → refund.completed
Endpoint B → order.completed
```

An endpoint must not have more than one active subscription to the same event type.

Subscriptions are resolved when an event is accepted, not when a worker eventually performs delivery.

---

### FR-03 — Event Submission

A producer must be able to submit an event containing:

- event type
- arbitrary JSON payload

Example:

```json
{
  "type": "order.completed",
  "payload": {
    "order_id": "ORD-123"
  }
}
```

The platform must persist the event and all derived delivery records within the same database transaction.

The API must not acknowledge successful acceptance before this transaction commits.

---

### FR-04 — Idempotent Event Submission

`POST /v1/events` must support producer-generated idempotency keys.

Conceptual request:

```http
POST /v1/events
Idempotency-Key: <client-generated-value>
```

The semantics are:

- first accepted request → create event and deliveries;
- same active key + same logical request → return the previously accepted event;
- same active key + different logical request → return `409 Conflict`;
- invalid requests that are never accepted do not consume the key;
- expired keys may be reused and are treated as new requests.

Idempotency keys initially have a retention period of **7 days**.

When authentication/multi-tenancy is introduced, keys must be scoped to the producer/account rather than globally.

---

### FR-05 — Fan-Out

One event may generate multiple independent deliveries.

Example:

```text
Event E1
├── Delivery D1 → Accounting
├── Delivery D2 → Analytics
└── Delivery D3 → CRM
```

Failure of one delivery must not reset or repeat successful deliveries to other endpoints.

---

### FR-06 — Asynchronous Delivery

Event submission must not wait for webhook endpoints to respond.

Delivery must be performed asynchronously by delivery worker processes.

---

### FR-07 — Signed Webhooks

Outgoing webhook requests must be signed using a per-endpoint secret.

The receiver must receive enough information to:

- identify the logical event;
- identify the specific delivery;
- detect replayed requests;
- validate request authenticity.

Expected headers include equivalents of:

```http
Webhook-Event-Id: evt_...
Webhook-Delivery-Id: del_...
Webhook-Timestamp: ...
Webhook-Signature: v1=...
```

The signature must be calculated over the exact bytes transmitted in the HTTP request.

---

### FR-08 — Delivery Attempts

Each network attempt must be recorded independently.

A delivery attempt should capture at minimum:

- delivery identifier
- attempt number
- start timestamp
- completion timestamp
- HTTP response status, when available
- transport/error classification, when applicable
- duration

Response bodies will not be persisted by default.

---

### FR-09 — Retry Policy

Transient failures must be retried using:

- exponential backoff
- jitter
- maximum backoff
- finite wall-clock retry horizon

The exact retry intervals and retry horizon are configuration parameters and must be justified through testing rather than hard-coded as architectural constants.

`Retry-After` should be respected where appropriate, particularly for responses such as `429 Too Many Requests`.

---

### FR-10 — Failure Classification

Delivery outcomes must be classified by semantics rather than solely by HTTP status-code family.

Initial policy:

| Result | Classification | Action |
|---|---|---|
| `2xx` | success | mark delivered |
| `400` | permanent request failure | fail delivery |
| `401` | endpoint authentication/configuration failure | endpoint-level handling |
| `404` | possible endpoint/configuration failure | limited confirmation/retry |
| `408` | transient | retry |
| `410` | endpoint lifecycle failure | fail and disable/suspend endpoint |
| `422` | permanent request failure | fail delivery |
| `429` | transient/rate limited | retry, respect `Retry-After` |
| `500` | transient server failure | retry |
| `501` | non-transient capability failure | fail |
| `502` | transient gateway failure | retry |
| `503` | transient availability failure | retry |
| `504` | transient gateway timeout | retry |

Network errors, DNS failures, connection failures, and request timeouts must also participate in failure classification.

---

### FR-11 — Endpoint-Level Protection

Repeated failures that indicate endpoint-wide problems must not cause every queued delivery to independently exhaust its retry policy.

The platform distinguishes:

**Administrative/configuration state**

```text
ACTIVE
SUSPENDED
DISABLED
```

from:

**Runtime health/circuit state**

```text
CLOSED
OPEN
HALF-OPEN
```

Examples:

- repeated authentication failures may suspend an endpoint;
- repeated transient availability failures may open its circuit breaker;
- an open circuit temporarily prevents normal delivery traffic;
- half-open state permits a small number of real deliveries to verify recovery.

The exact thresholds are configuration parameters.

---

### FR-12 — Delivery Inspection

Clients must be able to inspect:

- individual events;
- deliveries generated from an event;
- delivery state;
- delivery attempts;
- next scheduled retry;
- terminal failures.

Because one event may have mixed outcomes, events must not be represented as having only a single delivery status.

---

### FR-13 — Manual Replay

Terminal deliveries must support operator-initiated replay.

Replay must create a **new delivery record** rather than mutating the historical failed delivery back into `PENDING`.

Example:

```text
Event E1
├── D1 FAILED
└── D2 DELIVERED
    replay_of = D1
```

This preserves audit history.

---

## 4. Delivery Lifecycle

Initial persisted states:

```text
PENDING
DELIVERING
DELIVERED
FAILED
```

A retry does not require a separate `RETRYING` state.

A retryable delivery may instead be represented as:

```text
status = PENDING
attempt_count = N
next_attempt_at = future timestamp
```

Terminal expiration of the automatic retry horizon results in `FAILED`.

---

## 5. Non-Functional Requirements

### Reliability

- Accepted events must survive process restarts.
- Event acceptance and delivery generation must be atomic.
- Worker crashes must not permanently strand claimed work.
- Multiple workers must safely process work concurrently.
- Stale workers must not overwrite state produced by newer owners.

### Performance

Initial design targets:

- event submission p95: less than 150 ms under expected load;
- event payload size: maximum approximately 256 KB;
- initial target throughput: hundreds of accepted events per second;
- delivery workers must use bounded concurrency.

These are engineering targets, not yet proven guarantees.

Load testing must validate or revise them.

### Scalability

The design must support:

- multiple API instances;
- multiple delivery worker instances;
- independent scaling of APIs and workers.

### Security

The system must provide:

- TLS for external network communication;
- HMAC-signed webhook requests;
- secure storage of signing secrets;
- protection against accidental secret disclosure;
- bounded payload sizes;
- bounded HTTP response handling.

Authentication and authorization of the management/producer API will be designed separately.

### Observability

The system must expose sufficient information to diagnose:

- event ingestion failures;
- retry backlog;
- delivery success/failure rates;
- endpoint health;
- circuit-breaker state;
- delivery latency;
- attempt counts;
- worker claim/recovery behavior.

Structured logging, metrics, and distributed tracing should be supported.

---

## 6. System Boundaries

The platform owns:

- endpoint registration
- subscription management
- event acceptance
- persistence
- fan-out
- scheduling
- retries
- signing
- delivery
- attempt history
- endpoint failure protection
- observability

The platform does not own:

- receiver business logic
- receiver database transactions
- receiver idempotency implementation
- business event schema validation beyond platform-level validation
- workflow orchestration
- arbitrary event transformations

---

## 7. Out of Scope for V1

The initial version will not attempt to provide:

- exactly-once delivery
- arbitrary workflow orchestration
- user-provided code execution
- event transformation languages
- Kafka-compatible event streaming
- multi-region replication
- geo-aware routing
- frontend dashboard
- complex RBAC
- arbitrary payload filtering rules
- customer-defined retry programs

---

## 8. Initial API Surface

### Endpoints

```text
POST   /v1/endpoints
GET    /v1/endpoints
GET    /v1/endpoints/{endpoint_id}
PATCH  /v1/endpoints/{endpoint_id}
DELETE /v1/endpoints/{endpoint_id}
```

### Subscriptions

```text
POST   /v1/endpoints/{endpoint_id}/subscriptions
GET    /v1/endpoints/{endpoint_id}/subscriptions
DELETE /v1/endpoints/{endpoint_id}/subscriptions/{subscription_id}
```

### Events

```text
POST /v1/events
GET  /v1/events/{event_id}
GET  /v1/events/{event_id}/deliveries
```

### Deliveries

```text
GET  /v1/deliveries/{delivery_id}
GET  /v1/deliveries/{delivery_id}/attempts
POST /v1/deliveries/{delivery_id}/replay
```

The event submission endpoint returns `202 Accepted` after durable acceptance.

---

## 9. Open Implementation Parameters

The following values are deliberately not fixed yet:

- initial retry delay
- exponential-backoff multiplier
- jitter algorithm
- maximum backoff
- automatic retry horizon
- HTTP delivery timeout
- worker batch size
- worker concurrency
- claim lease duration
- circuit-breaker thresholds
- half-open probe count
- endpoint authentication failure threshold
- retention period for events/delivery attempts

These should be established through implementation constraints, benchmarks, and operational reasoning rather than arbitrary constants.