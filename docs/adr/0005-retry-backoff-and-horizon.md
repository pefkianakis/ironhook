# ADR-005 — Exponential Backoff, Jitter, and Finite Retry Horizon

## Status

Accepted

## Context

Webhook endpoints may temporarily become unavailable.

A fixed retry interval can create synchronized retry storms.

Example:

```text
30,000 failed deliveries
        ↓
wait 30 seconds
        ↓
30,000 retries
```

Such behavior can increase pressure on a service that is already unhealthy.

Retrying indefinitely would also create an unbounded backlog.

## Decision

Retryable deliveries use:

- exponential backoff;
- jitter;
- maximum backoff;
- finite wall-clock retry horizon.

`Retry-After` should be honored where applicable.

Repeated endpoint-wide transient failures may additionally trigger a circuit breaker.

## Rationale

Exponential backoff reduces repeated pressure during outages.

Jitter prevents large populations of retries from becoming synchronized.

A finite retry horizon bounds automatic backlog growth and defines a clear reliability contract.

## Consequences

Positive:

- reduced retry storms;
- better behavior toward unhealthy customer endpoints;
- predictable automatic retry lifetime;
- bounded retry backlog.

Negative:

- prolonged outages may cause deliveries to reach a terminal failure state;
- operators may need to replay failed deliveries after recovery.

## Rejected Alternative

### Infinite retry

Rejected because it creates potentially unbounded storage and operational backlog.

### Fixed retry interval

Rejected because it can generate synchronized load spikes.
