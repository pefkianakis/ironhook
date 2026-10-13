-- name: InsertEvent :one

INSERT INTO events (
    id,
    event_type,
    payload,
    occurred_at
)
VALUES (
    sqlc.arg(id),
    sqlc.arg(event_type),
    sqlc.arg(payload),
    sqlc.arg(occurred_at)
)
RETURNING accepted_at;

-- name: InsertIdempotencyRecord :one
INSERT INTO idempotency_records (
    scope,
    idempotency_key,
    request_hash,
    event_id
)
VALUES (
    sqlc.arg(scope),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(event_id)
)
ON CONFLICT (scope, idempotency_key)
DO NOTHING
RETURNING event_id;

-- name: GetIdempotencyRecord :one
SELECT
    i.event_id,
    i.request_hash,
    e.accepted_at
FROM idempotency_records AS i
JOIN events AS e
    ON e.id = i.event_id
WHERE i.scope = sqlc.arg(scope)
  AND i.idempotency_key = sqlc.arg(idempotency_key);

-- name: ResolveEndpointsForEvent :many
SELECT
    e.id,
    e.url,
    e.signing_secret,
    e.suspended_until
FROM subscriptions AS s
JOIN endpoints AS e
    ON e.id = s.endpoint_id
WHERE s.event_type = sqlc.arg(event_type)
  AND e.status = 'active'
ORDER BY e.id
FOR KEY SHARE OF e;

-- name: InsertDeliveries :copyfrom
INSERT INTO deliveries (
    id,
    event_id,
    endpoint_id,
    destination_url,
    signing_secret,
    available_at,
    max_attempts,
    retry_deadline_at
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8
);