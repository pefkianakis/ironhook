# ADR-002 — PostgreSQL as Durable Queue

## Status

Accepted

## Context

The platform requires:

- durable event storage;
- durable delivery scheduling;
- atomic fan-out;
- retry scheduling;
- concurrent worker processing.

Possible technologies included:

- PostgreSQL
- Kafka
- RabbitMQ
- Redis-backed queues

Introducing a message broker would add infrastructure and operational complexity.

## Decision

PostgreSQL will initially act as both:

1. the system of record;
2. the durable delivery work queue.

Workers claim eligible work using PostgreSQL concurrency primitives such as:

```sql
FOR UPDATE SKIP LOCKED
```

## Rationale

Event acceptance requires the event and its derived deliveries to be committed atomically.

PostgreSQL gives the system:

- transactions;
- durability;
- row-level locking;
- retry scheduling through timestamps;
- sufficient expected throughput for the initial project.

A separate broker is not justified until measurements demonstrate that PostgreSQL has become the bottleneck.

## Consequences

Positive:

- simpler infrastructure;
- atomic event + delivery creation;
- fewer consistency problems;
- easier local development.

Negative:

- worker polling adds database load;
- PostgreSQL eventually has limits as a high-throughput queue;
- database workload and queue workload share infrastructure.

## Rejected Alternative

### Kafka from V1

Rejected because current requirements do not justify the additional infrastructure or distributed-system complexity.

Kafka remains a possible future evolution if measured throughput or broader event-streaming requirements demand it.
