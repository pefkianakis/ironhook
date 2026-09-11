# ADR-003 — Resolve Subscriptions at Event Acceptance

## Status

Accepted

## Context

Subscriptions are mutable configuration.

An endpoint may subscribe or unsubscribe between the moment an event is accepted and the moment a worker attempts delivery.

Two possible models were considered.

### Delivery-time resolution

```text
Event
  ↓
worker queries current subscriptions
  ↓
create/send work
```

### Acceptance-time resolution

```text
Event accepted
  ↓
resolve subscriptions
  ↓
persist Delivery records
```

## Decision

Subscriptions are resolved when the event is accepted.

The event and all resulting delivery records are persisted atomically.

## Rationale

A successful `202 Accepted` should define a stable work contract.

Configuration changes occurring after acceptance should not retroactively change the recipient set for an already accepted event.

## Consequences

Positive:

- stable delivery contract;
- simpler worker responsibilities;
- historical recipient set is preserved;
- subscription changes cannot silently alter already accepted work.

Negative:

- an endpoint that unsubscribes immediately after acceptance may still receive that already accepted event.

This consequence is intentional.
