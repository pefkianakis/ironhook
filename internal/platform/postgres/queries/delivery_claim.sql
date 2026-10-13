-- name: ClaimDeliveries :many
WITH candidates AS (
    SELECT d.id
    FROM deliveries AS d
    WHERE d.status = 'pending'
      AND d.available_at <= now()
    ORDER BY d.available_at, d.id
    LIMIT sqlc.arg(batch_limit)::int
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE deliveries AS d
    SET
        available_at =
            now()
            + (
                sqlc.arg(lease_milliseconds)::bigint
                * interval '1 millisecond'
            ),
        claimed_by = sqlc.arg(worker_id),
        claim_version = d.claim_version + 1
    FROM candidates AS c
    WHERE d.id = c.id
    RETURNING
        d.id,
        d.event_id,
        d.endpoint_id,
        d.destination_url,
        d.signing_secret,
        d.claim_version,
        d.attempt_count,
        d.max_attempts,
        d.retry_deadline_at
)
SELECT
    c.id,
    c.event_id,
    c.endpoint_id,

    e.event_type,
    e.payload,

    c.destination_url,
    c.signing_secret,

    c.claim_version,
    c.attempt_count,
    c.max_attempts,
    c.retry_deadline_at
FROM claimed AS c
JOIN events AS e
    ON e.id = c.event_id
ORDER BY c.id;