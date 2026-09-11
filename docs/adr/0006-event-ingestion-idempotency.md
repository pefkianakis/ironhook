# ADR-006 — Producer-Side Idempotent Event Ingestion

## Status

Accepted

## Context

A producer may successfully submit an event while failing to receive the API response.

Example:

```text
Producer                  Platform

POST event
------------------------->

                          commit event
                          commit deliveries

<------------------------- response lost
```

The producer cannot know whether the event was accepted and may retry.

Without ingestion idempotency, the retry would create a second logical event and another set of deliveries.

## Decision

`POST /v1/events` requires/supports a client-generated idempotency key.

Conceptually:

```http
Idempotency-Key: <client-generated-value>
```

The platform persists an idempotency record containing:

```text
scope
key
request_hash
event_id
created_at
expires_at
```

Initial retention is **7 days**.

Semantics:

- new key → create event;
- same active key + same logical request → return existing event;
- same active key + different request → `409 Conflict`;
- invalid, unaccepted requests do not consume the key;
- expired keys may be treated as new.

Database constraints provide the final concurrency guarantee.

## Rationale

The idempotency identifier must exist before the producer makes the first network request.

Therefore the producer, not the platform, must supply it.

## Consequences

Positive:

- producers can safely retry ambiguous submissions;
- duplicate fan-out caused by network failures is avoided;
- concurrent duplicate submissions are handled safely.

Negative:

- idempotency state requires retention and cleanup;
- producers must correctly manage keys;
- reuse after expiration may create a new event.

## Important Distinction

Producer-side idempotency solves:

```text
Producer → Webhook Platform
```

Receiver-side idempotency solves:

```text
Webhook Platform → Receiver
```

They address two different network boundaries and both are necessary for a robust system.