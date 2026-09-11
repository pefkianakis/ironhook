# Architecture Decision Records

This directory contains the architecture decisions that materially shape the Reliable Webhook Delivery Platform.

The goal of these ADRs is not to document every implementation detail. They capture decisions where:

- multiple reasonable alternatives existed;
- the choice materially affects reliability, scalability, or maintainability;
- an engineer should be able to explain the trade-off;
- changing the decision later would have architectural consequences.

Initial ADRs:

```text
ADR-001 — At-Least-Once Delivery Semantics
ADR-002 — PostgreSQL as Durable Queue
ADR-003 — Resolve Subscriptions at Event Acceptance
ADR-004 — Worker Claiming with Leases and Fencing
ADR-005 — Exponential Backoff, Jitter, and Finite Retry Horizon
ADR-006 — Producer-Side Idempotent Event Ingestion
```