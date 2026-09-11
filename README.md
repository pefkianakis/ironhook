# Ironhook

Ironhook is a reliable webhook delivery platform written in Go.

It accepts events from producers, persists them durably, resolves subscribed webhook endpoints, and delivers events asynchronously with retry, observability, signing, and at-least-once delivery semantics.

## Why Ironhook?

Reliable webhook delivery becomes difficult once failures are considered.

A receiver may be temporarily unavailable, a network connection may disappear after the receiver processes a request, thousands of retries may become synchronized, or a worker may crash while owning delivery work.

Ironhook is designed around these failure modes rather than treating webhook delivery as a simple HTTP `POST`.

## Core Guarantees

Ironhook is designed to provide:

- durable event acceptance;
- at-least-once webhook delivery;
- idempotent producer event submission;
- subscription-based fan-out;
- independent per-endpoint delivery lifecycles;
- exponential backoff with jitter;
- finite retry horizons;
- safe concurrent worker claiming;
- worker crash recovery;
- stale-worker fencing;
- endpoint circuit breaking and suspension;
- signed webhook requests;
- delivery and attempt audit history;
- manual replay of terminal deliveries.

Exactly-once delivery is intentionally not claimed because Ironhook cannot atomically coordinate its state with an arbitrary remote HTTP receiver.

## Architecture

```text
Producer
   |
   | HTTP
   v
+----------------+
| Ironhook API   |
+-------+--------+
        |
        | SQL
        v
+----------------+
| PostgreSQL     |
|                |
| Events         |
| Deliveries     |
| Attempts       |
+-------+--------+
        |
        | claim work
        v
+----------------+
| Ironhook Worker|
+-------+--------+
        |
        | signed HTTP
        v
Webhook Endpoint
```

The API and worker are separate executables but share one Go codebase and PostgreSQL datastore.

PostgreSQL initially serves as both the system of record and durable delivery queue.

## Repository Structure

```text
cmd/
  api/            Ironhook HTTP API executable
  worker/         asynchronous delivery worker

internal/
  config/         application configuration
  httpapi/        HTTP transport
  endpoint/       endpoint and subscription domain
  event/          event domain
  delivery/       delivery and attempt domain
  idempotency/    producer idempotency domain
  webhook/        outbound webhook contract

migrations/       PostgreSQL migrations

docs/
  requirements.md
  architecture.md
  api.md
  day-01-interview-qa.md
  adr/            architecture decision records
```

## Development

### Requirements

- Go 1.27+
- Docker
- Docker Compose
- Make

### Start PostgreSQL

```bash
make compose-up
```

### Run the API

```bash
make run-api
```

The initial health endpoint is available at:

```text
GET /healthz
```

### Run tests

```bash
make test
```

or with Go's race detector:

```bash
make test-race
```

### Build

```bash
make build
```

The resulting binaries are:

```text
bin/ironhook-api
bin/ironhook-worker
```

## Documentation

The architecture is documented before implementation so that important reliability decisions are explicit rather than accidental.

See:

- `docs/requirements.md`
- `docs/architecture.md`
- `docs/api.md`
- `docs/adr/`

## Project Status

Ironhook is currently under active development.

The initial architecture and reliability contract have been defined. Persistence, event ingestion, delivery scheduling, retries, signing, observability, and failure handling are being implemented incrementally.
