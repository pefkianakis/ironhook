# ADR-001 — At-Least-Once Delivery Semantics

## Status

Accepted

## Context

The platform delivers events to external systems through HTTP.

An ambiguous outcome can occur when the receiver processes a webhook successfully but the network connection fails before the platform receives the response.

Example:

```text
Webhook Platform              Receiver

POST webhook
----------------------------->

                              process event
                              commit DB transaction

<----------------------------- connection lost
```

The sender cannot determine whether the receiver committed its business operation.

If the sender does not retry, an event may be lost.

If the sender retries, the receiver may process the event twice.

## Decision

The platform provides **at-least-once delivery semantics**.

The same logical event may therefore be delivered more than once.

Outgoing requests expose stable identifiers including:

```text
event ID
delivery ID
```

Receivers are expected to implement idempotent processing when duplicate business side effects would be harmful.

## Consequences

Positive:

- accepted work is not silently discarded because of ambiguous network outcomes;
- delivery semantics accurately reflect what can be guaranteed over HTTP;
- retries can be implemented safely when receivers support idempotency.

Negative:

- receivers must tolerate duplicate requests;
- exactly-once business side effects cannot be guaranteed by the platform alone.

## Rejected Alternative

### Exactly-once HTTP delivery

Rejected because the platform cannot atomically coordinate its database transaction with an arbitrary remote receiver's transaction.
