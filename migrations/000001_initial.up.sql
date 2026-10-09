CREATE TABLE endpoints (
    id UUID PRIMARY KEY,

    url TEXT NOT NULL,
    signing_secret BYTEA NOT NULL,

    status TEXT NOT NULL DEFAULT 'active',
    suspended_until TIMESTAMPTZ,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT endpoints_url_not_blank_chk
        CHECK (
            url = btrim(url)
            AND url <> ''
        ),

    CONSTRAINT endpoints_signing_secret_not_empty_chk
        CHECK (
            octet_length(signing_secret) > 0
        ),

    CONSTRAINT endpoints_status_chk
        CHECK (
            status IN ('active', 'disabled', 'deleted')
        ),

    CONSTRAINT endpoints_consecutive_failures_chk
        CHECK (
            consecutive_failures >= 0
        )
);


CREATE TABLE subscriptions (
    id UUID PRIMARY KEY,

    endpoint_id UUID NOT NULL,
    event_type TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT subscriptions_endpoint_fk
        FOREIGN KEY (endpoint_id)
        REFERENCES endpoints(id)
        ON DELETE CASCADE,

    CONSTRAINT subscriptions_endpoint_event_unique
        UNIQUE (endpoint_id, event_type),

    CONSTRAINT subscriptions_event_type_not_blank_chk
        CHECK (
            event_type = btrim(event_type)
            AND event_type <> ''
        )
);


CREATE TABLE events (
    id UUID PRIMARY KEY,

    event_type TEXT NOT NULL,
    payload BYTEA NOT NULL,

    occurred_at TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT events_event_type_not_blank_chk
        CHECK (
            event_type = btrim(event_type)
            AND event_type <> ''
        )
);


CREATE TABLE idempotency_records (
    scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,

    request_hash BYTEA NOT NULL,
    event_id UUID NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT idempotency_records_pk
        PRIMARY KEY (scope, idempotency_key),

    CONSTRAINT idempotency_records_event_fk
        FOREIGN KEY (event_id)
        REFERENCES events(id)
        ON DELETE CASCADE,

    CONSTRAINT idempotency_records_scope_not_blank_chk
        CHECK (
            scope = btrim(scope)
            AND scope <> ''
        ),

    CONSTRAINT idempotency_records_key_not_blank_chk
        CHECK (
            idempotency_key = btrim(idempotency_key)
            AND idempotency_key <> ''
        ),

    CONSTRAINT idempotency_records_hash_size_chk
        CHECK (
            octet_length(request_hash) = 32
        )
);


CREATE TABLE deliveries (
    id UUID PRIMARY KEY,

    event_id UUID NOT NULL,
    endpoint_id UUID NOT NULL,

    destination_url TEXT NOT NULL,
    signing_secret BYTEA NOT NULL,

    status TEXT NOT NULL DEFAULT 'pending',

    available_at TIMESTAMPTZ NOT NULL,
    claimed_by TEXT,
    claim_version BIGINT NOT NULL DEFAULT 0,

    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    retry_deadline_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,

    CONSTRAINT deliveries_event_fk
        FOREIGN KEY (event_id)
        REFERENCES events(id)
        ON DELETE RESTRICT,

    CONSTRAINT deliveries_endpoint_fk
        FOREIGN KEY (endpoint_id)
        REFERENCES endpoints(id)
        ON DELETE RESTRICT,

    CONSTRAINT deliveries_event_endpoint_unique
        UNIQUE (event_id, endpoint_id),

    CONSTRAINT deliveries_destination_url_not_blank_chk
        CHECK (
            destination_url = btrim(destination_url)
            AND destination_url <> ''
        ),

    CONSTRAINT deliveries_signing_secret_not_empty_chk
        CHECK (
            octet_length(signing_secret) > 0
        ),

    CONSTRAINT deliveries_status_chk
        CHECK (
            status IN ('pending', 'succeeded', 'aborted')
        ),

    CONSTRAINT deliveries_claimed_by_not_blank_chk
        CHECK (
            claimed_by IS NULL
            OR (
                claimed_by = btrim(claimed_by)
                AND claimed_by <> ''
            )
        ),

    CONSTRAINT deliveries_claim_version_chk
        CHECK (
            claim_version >= 0
        ),

    CONSTRAINT deliveries_attempt_count_chk
        CHECK (
            attempt_count >= 0
        ),

    CONSTRAINT deliveries_max_attempts_chk
        CHECK (
            max_attempts > 0
        ),

    CONSTRAINT deliveries_attempt_limit_chk
        CHECK (
            attempt_count <= max_attempts
        ),

    CONSTRAINT deliveries_retry_deadline_chk
        CHECK (
            retry_deadline_at >= created_at
        ),

    CONSTRAINT deliveries_completion_state_chk
        CHECK (
            (
                status = 'pending'
                AND completed_at IS NULL
            )
            OR
            (
                status IN ('succeeded', 'aborted')
                AND completed_at IS NOT NULL
                AND claimed_by IS NULL
            )
        )
);


CREATE TABLE delivery_attempts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    delivery_id UUID NOT NULL,
    attempt_number INTEGER NOT NULL,

    claim_version BIGINT NOT NULL,
    worker_id TEXT NOT NULL,

    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,

    outcome TEXT,

    http_status INTEGER,
    error_kind TEXT,
    error_message TEXT,

    CONSTRAINT delivery_attempts_delivery_fk
        FOREIGN KEY (delivery_id)
        REFERENCES deliveries(id)
        ON DELETE CASCADE,

    CONSTRAINT delivery_attempts_number_unique
        UNIQUE (delivery_id, attempt_number),

    CONSTRAINT delivery_attempts_attempt_number_chk
        CHECK (
            attempt_number > 0
        ),

    CONSTRAINT delivery_attempts_claim_version_chk
        CHECK (
            claim_version > 0
        ),

    CONSTRAINT delivery_attempts_worker_id_not_blank_chk
        CHECK (
            worker_id = btrim(worker_id)
            AND worker_id <> ''
        ),

    CONSTRAINT delivery_attempts_http_status_chk
        CHECK (
            http_status IS NULL
            OR http_status BETWEEN 100 AND 599
        ),

    CONSTRAINT delivery_attempts_finished_at_chk
        CHECK (
            finished_at IS NULL
            OR finished_at >= started_at
        ),

    CONSTRAINT delivery_attempts_outcome_chk
        CHECK (
            outcome IS NULL
            OR outcome IN (
                'succeeded',
                'retryable_failure',
                'permanent_failure'
            )
        ),

    CONSTRAINT delivery_attempts_completion_chk
        CHECK (
            (
                finished_at IS NULL
                AND outcome IS NULL
                AND http_status IS NULL
                AND error_kind IS NULL
                AND error_message IS NULL
            )
            OR
            (
                finished_at IS NOT NULL
                AND outcome IS NOT NULL
            )
        ),

    CONSTRAINT delivery_attempts_success_status_chk
        CHECK (
            outcome <> 'succeeded'
            OR http_status BETWEEN 200 AND 299
        )
);


CREATE INDEX subscriptions_event_type_endpoint_idx
    ON subscriptions (event_type, endpoint_id);


CREATE INDEX deliveries_claim_idx
    ON deliveries (available_at, id)
    WHERE status = 'pending';


CREATE INDEX deliveries_endpoint_created_idx
    ON deliveries (endpoint_id, created_at DESC);