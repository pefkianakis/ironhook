# Reliable Webhook Delivery Platform — API Contract

## 1. Overview

The API is versioned under:

```text
/v1
```

All request and response bodies use:

```http
Content-Type: application/json
```

Timestamps are represented in UTC using RFC 3339.

Resource identifiers are opaque strings. Example formats may look like:

```text
ep_...
sub_...
evt_...
del_...
```

Clients must not depend on the internal identifier format.

---

# 2. Common Error Format

All application errors use a consistent envelope:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "The request contains invalid fields.",
    "details": [
      {
        "field": "url",
        "reason": "must be a valid HTTPS URL"
      }
    ]
  }
}
```

`error.code` is a stable machine-readable identifier.

`error.message` is intended for human-readable diagnostics.

`details` is optional.

Examples of error codes:

```text
validation_failed
endpoint_not_found
subscription_already_exists
idempotency_key_reused
endpoint_has_pending_deliveries
delivery_not_found
delivery_not_replayable
```

---

# 3. Status Code Conventions

General conventions:

| Status | Meaning |
|---|---|
| `200 OK` | Successful resource retrieval/update |
| `201 Created` | Resource created synchronously |
| `202 Accepted` | Asynchronous operation accepted durably |
| `204 No Content` | Successful deletion/deactivation |
| `400 Bad Request` | Malformed request syntax |
| `404 Not Found` | Resource does not exist |
| `409 Conflict` | Request conflicts with existing system state |
| `422 Unprocessable Entity` | Request syntax is valid but values are invalid |
| `500 Internal Server Error` | Unexpected platform failure |

---

# 4. Endpoint Resource

An Endpoint represents one external webhook destination.

Conceptual representation:

```json
{
  "id": "ep_123",
  "url": "https://analytics.example.com/webhooks",
  "description": "Analytics production endpoint",
  "status": "active",
  "created_at": "2026-09-09T01:00:00Z",
  "updated_at": "2026-09-09T01:00:00Z"
}
```

Possible administrative states:

```text
active
suspended
disabled
```

`active` means the endpoint can receive new and pending deliveries.

`suspended` means webhook traffic is temporarily paused.

`disabled` means the endpoint has been permanently removed from active platform use.

---

# 5. Create Endpoint

```http
POST /v1/endpoints
```

Request:

```json
{
  "url": "https://analytics.example.com/webhooks",
  "description": "Analytics production endpoint"
}
```

Response:

```http
201 Created
Location: /v1/endpoints/ep_123
```

```json
{
  "id": "ep_123",
  "url": "https://analytics.example.com/webhooks",
  "description": "Analytics production endpoint",
  "status": "active",
  "secret": "whsec_...",
  "created_at": "2026-09-09T01:00:00Z",
  "updated_at": "2026-09-09T01:00:00Z"
}
```

The signing secret is returned when the endpoint is created.

It is not returned by normal endpoint retrieval endpoints.

---

# 6. List Endpoints

```http
GET /v1/endpoints
```

Optional query parameters:

```text
limit
cursor
status
```

Example:

```text
GET /v1/endpoints?limit=50&status=active
```

Response:

```json
{
  "data": [
    {
      "id": "ep_123",
      "url": "https://analytics.example.com/webhooks",
      "description": "Analytics production endpoint",
      "status": "active",
      "created_at": "2026-09-09T01:00:00Z",
      "updated_at": "2026-09-09T01:00:00Z"
    }
  ],
  "next_cursor": null
}
```

Cursor-based pagination is preferred over offset-based pagination.

---

# 7. Get Endpoint

```http
GET /v1/endpoints/{endpoint_id}
```

Success:

```http
200 OK
```

```json
{
  "id": "ep_123",
  "url": "https://analytics.example.com/webhooks",
  "description": "Analytics production endpoint",
  "status": "active",
  "created_at": "2026-09-09T01:00:00Z",
  "updated_at": "2026-09-09T01:00:00Z"
}
```

Not found:

```http
404 Not Found
```

```json
{
  "error": {
    "code": "endpoint_not_found",
    "message": "The endpoint does not exist."
  }
}
```

---

# 8. Update Endpoint

```http
PATCH /v1/endpoints/{endpoint_id}
```

Example URL update:

```json
{
  "url": "https://new.example.com/webhooks"
}
```

Example suspension:

```json
{
  "status": "suspended"
}
```

Example resumption:

```json
{
  "status": "active"
}
```

Response:

```http
200 OK
```

with the updated endpoint representation.

---

# 9. Endpoint Suspension Semantics

Transition:

```text
ACTIVE ↔ SUSPENDED
```

Suspension means:

- no new deliveries are generated for the endpoint;
- workers stop initiating new attempts for existing pending deliveries;
- existing deliveries remain persisted;
- delivery history remains unchanged;
- retry horizons continue to advance using wall-clock time.

When the endpoint returns to `ACTIVE`, eligible pending deliveries may resume if they have not exceeded their retry horizon.

Suspension cannot guarantee cancellation of HTTP requests that are already in flight.

Workers should re-check endpoint state immediately before sending where practical.

---

# 10. Endpoint URL Updates

A Delivery permanently references an Endpoint identity.

It does not permanently snapshot the endpoint URL.

Example:

```text
Delivery D1 → Endpoint ep_123
```

If:

```text
ep_123.url
```

changes before a future attempt, that attempt uses the endpoint's current active URL.

This allows operational configuration fixes to apply to queued deliveries.

Each DeliveryAttempt should retain sufficient metadata to identify the actual endpoint configuration used during that attempt.

This may initially include the attempted URL and may later evolve into explicit endpoint configuration versioning.

---

# 11. Delete Endpoint

```http
DELETE /v1/endpoints/{endpoint_id}
```

Deletion is implemented as logical disablement.

The database row and historical relationships are preserved.

A successful transition is:

```text
ACTIVE/SUSPENDED → DISABLED
```

If the endpoint has outstanding:

```text
PENDING
DELIVERING
```

deliveries, deletion fails:

```http
409 Conflict
```

```json
{
  "error": {
    "code": "endpoint_has_pending_deliveries",
    "message": "The endpoint cannot be disabled while outstanding deliveries exist."
  }
}
```

If no outstanding deliveries remain:

```http
204 No Content
```

The endpoint becomes:

```text
DISABLED
```

Repeated `DELETE` requests against an already-disabled endpoint return:

```http
204 No Content
```

This keeps deletion idempotent.

Disabled endpoints:

- do not participate in future subscription fan-out;
- cannot receive delivery attempts;
- retain their historical records.

---

# 12. Subscription Resource

A Subscription associates an Endpoint with an event type.

Example:

```json
{
  "id": "sub_123",
  "endpoint_id": "ep_123",
  "event_type": "order.completed",
  "status": "active",
  "created_at": "2026-09-09T01:05:00Z"
}
```

An endpoint may only have one active subscription to the same event type.

---

# 13. Create Subscription

```http
POST /v1/endpoints/{endpoint_id}/subscriptions
```

Request:

```json
{
  "event_type": "order.completed"
}
```

Response:

```http
201 Created
Location: /v1/endpoints/ep_123/subscriptions/sub_123
```

```json
{
  "id": "sub_123",
  "endpoint_id": "ep_123",
  "event_type": "order.completed",
  "status": "active",
  "created_at": "2026-09-09T01:05:00Z"
}
```

Duplicate subscription:

```http
409 Conflict
```

```json
{
  "error": {
    "code": "subscription_already_exists",
    "message": "The endpoint is already subscribed to this event type."
  }
}
```

The database must enforce uniqueness under concurrent requests.

---

# 14. List Endpoint Subscriptions

```http
GET /v1/endpoints/{endpoint_id}/subscriptions
```

Response:

```json
{
  "data": [
    {
      "id": "sub_123",
      "endpoint_id": "ep_123",
      "event_type": "order.completed",
      "status": "active",
      "created_at": "2026-09-09T01:05:00Z"
    }
  ],
  "next_cursor": null
}
```

---

# 15. Delete Subscription

```http
DELETE /v1/endpoints/{endpoint_id}/subscriptions/{subscription_id}
```

Success:

```http
204 No Content
```

Deleting a subscription prevents future event fan-out.

It does not remove Deliveries that were already created before the subscription was deleted.

---

# 16. Event Submission

```http
POST /v1/events
Idempotency-Key: <client-generated-value>
```

Request:

```json
{
  "type": "order.completed",
  "payload": {
    "order_id": "ORD-123",
    "amount": 49.95
  }
}
```

Successful acceptance:

```http
202 Accepted
Location: /v1/events/evt_123
```

```json
{
  "id": "evt_123",
  "type": "order.completed",
  "status": "accepted",
  "delivery_count": 3,
  "created_at": "2026-09-09T01:10:00Z"
}
```

`delivery_count` represents the number of Delivery records durably created during acceptance.

It does not indicate successful webhook delivery.

---

# 17. Event Acceptance Transaction

Successful event acceptance requires the platform to atomically persist:

```text
Idempotency record
Event
matching subscriptions snapshot/result
Delivery records
```

Conceptually:

```text
BEGIN

validate idempotency state

INSERT Event

resolve active subscriptions
whose Endpoints are ACTIVE

INSERT Delivery records

bind idempotency key to Event

COMMIT

return 202
```

If the transaction fails, the API must not return successful acceptance.

---

# 18. Event Ingestion Idempotency

The producer supplies an idempotency key before the first request.

Example:

```http
Idempotency-Key: 4c18e24e-...
```

Initial retention period:

```text
7 days
```

Semantics:

### New key

```text
key does not exist
→ accept event
→ create deliveries
→ persist idempotency record
→ 202
```

### Same active key and same request

```text
existing key
+
same logical request
→ return existing Event
→ create no new Deliveries
→ 202
```

### Same active key and different request

```text
existing key
+
different request
→ 409 Conflict
```

Response:

```json
{
  "error": {
    "code": "idempotency_key_reused",
    "message": "The idempotency key has already been used with a different request."
  }
}
```

### Invalid request

If validation fails before acceptance:

```text
no accepted Event
no persistent consumed key
```

The producer may fix the request and reuse the key.

### Expired key

After the retention period, the key may be treated as a new submission.

Producers should therefore generate high-entropy keys and avoid intentional reuse.

---

# 19. Get Event

```http
GET /v1/events/{event_id}
```

Response:

```json
{
  "id": "evt_123",
  "type": "order.completed",
  "payload": {
    "order_id": "ORD-123",
    "amount": 49.95
  },
  "created_at": "2026-09-09T01:10:00Z",
  "deliveries": {
    "total": 3,
    "pending": 1,
    "delivering": 0,
    "delivered": 2,
    "failed": 0
  }
}
```

The Event does not expose one simple delivery status because its Deliveries may have different outcomes.

---

# 20. List Event Deliveries

```http
GET /v1/events/{event_id}/deliveries
```

Response:

```json
{
  "data": [
    {
      "id": "del_123",
      "endpoint_id": "ep_123",
      "status": "delivered",
      "attempt_count": 1,
      "created_at": "2026-09-09T01:10:00Z"
    },
    {
      "id": "del_456",
      "endpoint_id": "ep_456",
      "status": "pending",
      "attempt_count": 3,
      "next_attempt_at": "2026-09-09T01:20:00Z",
      "created_at": "2026-09-09T01:10:00Z"
    }
  ],
  "next_cursor": null
}
```

---

# 21. Delivery Resource

A Delivery represents:

> responsibility to deliver one Event to one Endpoint.

Possible persisted states:

```text
pending
delivering
delivered
failed
```

Conceptual representation:

```json
{
  "id": "del_456",
  "event_id": "evt_123",
  "endpoint_id": "ep_456",
  "status": "pending",
  "attempt_count": 3,
  "next_attempt_at": "2026-09-09T01:20:00Z",
  "retry_until": "2026-09-10T01:10:00Z",
  "replay_of_delivery_id": null,
  "created_at": "2026-09-09T01:10:00Z",
  "delivered_at": null
}
```

---

# 22. Get Delivery

```http
GET /v1/deliveries/{delivery_id}
```

Success:

```http
200 OK
```

Response:

```json
{
  "id": "del_456",
  "event_id": "evt_123",
  "endpoint_id": "ep_456",
  "status": "pending",
  "attempt_count": 3,
  "next_attempt_at": "2026-09-09T01:20:00Z",
  "retry_until": "2026-09-10T01:10:00Z",
  "replay_of_delivery_id": null,
  "created_at": "2026-09-09T01:10:00Z",
  "delivered_at": null
}
```

---

# 23. Delivery Attempts

```http
GET /v1/deliveries/{delivery_id}/attempts
```

Response:

```json
{
  "data": [
    {
      "attempt_number": 1,
      "http_status": 503,
      "duration_ms": 241,
      "error": null,
      "started_at": "2026-09-09T01:10:05Z",
      "finished_at": "2026-09-09T01:10:05Z"
    },
    {
      "attempt_number": 2,
      "http_status": null,
      "duration_ms": 5001,
      "error": "request_timeout",
      "started_at": "2026-09-09T01:11:00Z",
      "finished_at": "2026-09-09T01:11:05Z"
    }
  ],
  "next_cursor": null
}
```

Response bodies are not persisted by default.

Each attempt should preserve enough metadata to identify the destination configuration actually used.

---

# 24. Replay Delivery

```http
POST /v1/deliveries/{delivery_id}/replay
```

Replay creates a new Delivery.

It does not mutate:

```text
FAILED → PENDING
```

on the original record.

Example:

```text
Original:
D1 FAILED

Replay:
D2 PENDING
replay_of_delivery_id = D1
```

Response:

```http
201 Created
Location: /v1/deliveries/del_987
```

```json
{
  "id": "del_987",
  "event_id": "evt_123",
  "endpoint_id": "ep_456",
  "status": "pending",
  "replay_of_delivery_id": "del_456",
  "created_at": "2026-09-09T02:00:00Z"
}
```

Replay is initially intended for terminally failed deliveries.

Invalid replay:

```http
409 Conflict
```

```json
{
  "error": {
    "code": "delivery_not_replayable",
    "message": "Only terminally failed deliveries may be replayed."
  }
}
```

---

# 25. Pagination

List endpoints use cursor-based pagination.

Conceptual request:

```text
GET /v1/events?limit=50&cursor=...
```

Response:

```json
{
  "data": [],
  "next_cursor": "..."
}
```

Cursor pagination is preferred because the platform operates on append-heavy datasets where new Events and DeliveryAttempts are continuously inserted.

Offset-based pagination is not part of the initial API contract.

---

# 26. Outgoing Webhook Contract

Outgoing webhook requests use a stable envelope.

Example:

```json
{
  "id": "evt_123",
  "type": "order.completed",
  "created_at": "2026-09-09T01:10:00Z",
  "data": {
    "order_id": "ORD-123",
    "amount": 49.95
  }
}
```

Conceptual headers:

```http
Webhook-Event-Id: evt_123
Webhook-Delivery-Id: del_456
Webhook-Timestamp: 1788916200
Webhook-Signature: v1=...
```

The signature is calculated over the exact bytes transmitted.

The Event ID identifies the logical business event.

The Delivery ID identifies this platform-specific delivery lifecycle.

---

# 27. Delivery Failure Semantics

The API exposes delivery state, while the worker internally classifies delivery results.

Examples:

```text
2xx → delivered

408 → retry
429 → retry
500 → retry
502 → retry
503 → retry
504 → retry

400 → failed
422 → failed
501 → failed
```

Endpoint-level failures such as repeated authentication errors may suspend the Endpoint instead of allowing every queued Delivery to fail independently.

A `410 Gone` may result in endpoint disablement because it expresses a stronger permanent lifecycle signal.

---

# 28. Endpoint and Circuit State

Administrative Endpoint state:

```text
ACTIVE
SUSPENDED
DISABLED
```

Runtime circuit state:

```text
CLOSED
OPEN
HALF-OPEN
```

These are separate concepts.

For example:

```text
Endpoint status = ACTIVE
Circuit state = OPEN
```

means the Endpoint is enabled/configured correctly but delivery traffic is temporarily blocked because the destination appears unhealthy.

Circuit state does not replace administrative state.

---

# 29. Retry Horizon

Automatic delivery retries use a finite wall-clock horizon.

Suspension does not pause the retry deadline.

Example:

```text
Delivery created Monday 10:00

retry_until Tuesday 10:00

Endpoint suspended Monday 11:00

Endpoint resumed Wednesday 09:00

→ original delivery has already expired
```

The Delivery becomes terminally failed and may require replay.

---

# 30. API Resource Summary

```text
Endpoints
POST   /v1/endpoints
GET    /v1/endpoints
GET    /v1/endpoints/{endpoint_id}
PATCH  /v1/endpoints/{endpoint_id}
DELETE /v1/endpoints/{endpoint_id}

Subscriptions
POST   /v1/endpoints/{endpoint_id}/subscriptions
GET    /v1/endpoints/{endpoint_id}/subscriptions
DELETE /v1/endpoints/{endpoint_id}/subscriptions/{subscription_id}

Events
POST   /v1/events
GET    /v1/events/{event_id}
GET    /v1/events/{event_id}/deliveries

Deliveries
GET    /v1/deliveries/{delivery_id}
GET    /v1/deliveries/{delivery_id}/attempts
POST   /v1/deliveries/{delivery_id}/replay
```

---

# 31. Remaining Open API Decisions

The following are intentionally deferred:

- producer authentication format
- authorization/RBAC
- endpoint secret rotation endpoint
- exact maximum page size
- filtering syntax for list endpoints
- whether endpoint circuit state is exposed directly in the public API
- bulk replay API
- bulk endpoint operations
- event retention policy
- request-rate limiting semantics
- endpoint-specific delivery concurrency configuration
- OpenAPI schema generation tooling

These do not block initial implementation.