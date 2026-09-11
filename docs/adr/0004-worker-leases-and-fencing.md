# ADR-004 — Worker Claiming with Leases and Fencing

## Status

Accepted

## Context

Multiple worker instances must process deliveries concurrently.

A naïve pattern:

```text
SELECT pending
→ send HTTP
→ UPDATE
```

allows multiple workers to select the same delivery.

Holding a PostgreSQL row lock during external HTTP I/O is also undesirable because network calls may be slow or fail.

Worker crashes introduce another problem: work must not remain permanently stuck in a `DELIVERING` state.

## Decision

Workers use a short database transaction to claim work.

Eligible rows are selected using:

```sql
FOR UPDATE SKIP LOCKED
```

and marked:

```text
status = DELIVERING
claimed_at = ...
claim_version = claim_version + 1
```

before commit.

HTTP delivery occurs after the transaction commits.

Claims behave like leases.

Expired claims may be reclaimed by another worker.

Every state-changing worker update must verify the `claim_version`.

Example:

```sql
UPDATE deliveries
SET status = 'DELIVERED'
WHERE id = $1
  AND status = 'DELIVERING'
  AND claim_version = $2;
```

## Rationale

`SKIP LOCKED` prevents multiple workers from claiming the same currently available row.

The persisted lease allows work to survive worker crashes.

The claim version acts as a fencing token, preventing stale workers from overwriting newer database state.

## Consequences

Positive:

- concurrent workers can safely claim separate work;
- row locks remain short-lived;
- crashed workers do not permanently strand jobs;
- stale workers cannot overwrite newer ownership state.

Negative:

- duplicate external HTTP requests are still possible if ownership expires after a request reached the receiver;
- claim recovery introduces additional state and logic.

## Important Limitation

Fencing protects the platform's internal database state.

It does **not** prevent a stale worker's already-issued HTTP request from being processed by the receiver.

Therefore the delivery guarantee remains at-least-once.
