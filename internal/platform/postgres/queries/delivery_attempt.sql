-- name: StartDeliveryAttempt :one
WITH owned_delivery AS (
    UPDATE deliveries AS d
    SET
        attempt_count = d.attempt_count + 1
    WHERE d.id = sqlc.arg(delivery_id)
      AND d.status = 'pending'
      AND d.claim_version = sqlc.arg(claim_version)
      AND d.claimed_by = sqlc.arg(worker_id)::text
      AND d.available_at > now()
      AND d.attempt_count < d.max_attempts

      AND NOT EXISTS (
          SELECT 1
          FROM delivery_attempts AS da
          WHERE da.delivery_id = d.id
            AND da.claim_version = d.claim_version
            AND da.finished_at IS NULL
      )

    RETURNING
        d.id,
        d.attempt_count,
        d.claim_version
)
INSERT INTO delivery_attempts (
    delivery_id,
    attempt_number,
    claim_version,
    worker_id
)
SELECT
    od.id,
    od.attempt_count,
    od.claim_version,
    sqlc.arg(worker_id)::text
FROM owned_delivery AS od
RETURNING
    id,
    delivery_id,
    attempt_number,
    claim_version,
    worker_id,
    started_at;