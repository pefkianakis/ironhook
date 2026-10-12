package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	appevent "github.com/cpefkianakis/ironhook/internal/app/event"
	"github.com/cpefkianakis/ironhook/internal/idgen"
)

type AcceptanceStore struct {
	pool *pgxpool.Pool
	ids  idgen.Generator
}

func NewAcceptanceStore(
	pool *pgxpool.Pool,
	ids idgen.Generator,
) *AcceptanceStore {
	return &AcceptanceStore{
		pool: pool,
		ids:  ids,
	}
}

type resolvedEndpoint struct {
	ID uuid.UUID

	URL           string
	SigningSecret []byte

	SuspendedUntil *time.Time
}

type existingIdempotencyRecord struct {
	EventID     uuid.UUID
	RequestHash []byte
	AcceptedAt  time.Time
}

func (s *AcceptanceStore) Accept(
	ctx context.Context,
	p appevent.AcceptParams,
) (appevent.AcceptResult, error) {
	tx, err := s.pool.BeginTx(
		ctx,
		pgx.TxOptions{
			IsoLevel: pgx.ReadCommitted,
		},
	)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"begin acceptance transaction: %w",
				err,
			)
	}

	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	acceptedAt, err := insertEvent(
		ctx,
		tx,
		p,
	)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"insert event: %w",
				err,
			)
	}

	claimed, err := insertIdempotencyRecord(
		ctx,
		tx,
		p,
	)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"insert idempotency record: %w",
				err,
			)
	}

	if !claimed {
		existing, err := findIdempotencyRecord(
			ctx,
			tx,
			p.Scope,
			p.IdempotencyKey,
		)
		if err != nil {
			return appevent.AcceptResult{},
				fmt.Errorf(
					"find idempotency record: %w",
					err,
				)
		}

		if !bytes.Equal(
			existing.RequestHash,
			p.RequestHash[:],
		) {
			if err := tx.Rollback(ctx); err != nil {
				return appevent.AcceptResult{},
					fmt.Errorf(
						"rollback idempotency conflict: %w",
						err,
					)
			}

			return appevent.AcceptResult{},
				appevent.ErrIdempotencyConflict
		}

		if err := tx.Rollback(ctx); err != nil {
			return appevent.AcceptResult{},
				fmt.Errorf(
					"rollback idempotent replay: %w",
					err,
				)
		}

		return appevent.AcceptResult{
			EventID:    existing.EventID,
			AcceptedAt: existing.AcceptedAt,
			Replayed:   true,
		}, nil
	}

	endpoints, err := resolveEndpoints(
		ctx,
		tx,
		p.EventType,
	)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"resolve subscriptions: %w",
				err,
			)
	}

	if err := s.insertDeliveries(
		ctx,
		tx,
		p,
		acceptedAt,
		endpoints,
	); err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"insert deliveries: %w",
				err,
			)
	}

	if err := tx.Commit(ctx); err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"commit acceptance transaction: %w",
				err,
			)
	}

	return appevent.AcceptResult{
		EventID:    p.EventID,
		AcceptedAt: acceptedAt,
		Replayed:   false,
	}, nil
}

const insertEventSQL = `
INSERT INTO events (
    id,
    event_type,
    payload,
    occurred_at
)
VALUES ($1, $2, $3, $4)
RETURNING accepted_at
`

func insertEvent(
	ctx context.Context,
	tx pgx.Tx,
	p appevent.AcceptParams,
) (time.Time, error) {
	var acceptedAt time.Time

	err := tx.QueryRow(
		ctx,
		insertEventSQL,
		toPGUUID(p.EventID),
		p.EventType,
		p.Payload,
		p.OccurredAt,
	).Scan(&acceptedAt)

	if err != nil {
		return time.Time{}, err
	}

	return acceptedAt, nil
}

const insertIdempotencySQL = `
INSERT INTO idempotency_records (
    scope,
    idempotency_key,
    request_hash,
    event_id
)
VALUES ($1, $2, $3, $4)
ON CONFLICT (scope, idempotency_key)
DO NOTHING
RETURNING event_id
`

func insertIdempotencyRecord(
	ctx context.Context,
	tx pgx.Tx,
	p appevent.AcceptParams,
) (bool, error) {
	var eventID pgtype.UUID

	err := tx.QueryRow(
		ctx,
		insertIdempotencySQL,
		p.Scope,
		p.IdempotencyKey,
		p.RequestHash[:],
		toPGUUID(p.EventID),
	).Scan(&eventID)

	switch {
	case err == nil:
		return true, nil

	case errors.Is(err, pgx.ErrNoRows):
		return false, nil

	default:
		return false, err
	}
}

const findIdempotencySQL = `
SELECT
    i.event_id,
    i.request_hash,
    e.accepted_at
FROM idempotency_records i
JOIN events e
    ON e.id = i.event_id
WHERE i.scope = $1
  AND i.idempotency_key = $2
`

func findIdempotencyRecord(
	ctx context.Context,
	tx pgx.Tx,
	scope string,
	key string,
) (existingIdempotencyRecord, error) {
	var (
		eventID pgtype.UUID
		record  existingIdempotencyRecord
	)

	err := tx.QueryRow(
		ctx,
		findIdempotencySQL,
		scope,
		key,
	).Scan(
		&eventID,
		&record.RequestHash,
		&record.AcceptedAt,
	)

	if err != nil {
		return existingIdempotencyRecord{}, err
	}

	record.EventID = fromPGUUID(eventID)

	return record, nil
}

const resolveEndpointsSQL = `
SELECT
    e.id,
    e.url,
    e.signing_secret,
    e.suspended_until
FROM subscriptions s
JOIN endpoints e
    ON e.id = s.endpoint_id
WHERE s.event_type = $1
  AND e.status = 'active'
ORDER BY e.id
FOR KEY SHARE OF e
`

func resolveEndpoints(
	ctx context.Context,
	tx pgx.Tx,
	eventType string,
) ([]resolvedEndpoint, error) {
	rows, err := tx.Query(
		ctx,
		resolveEndpointsSQL,
		eventType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	endpoints := make(
		[]resolvedEndpoint,
		0,
	)

	for rows.Next() {
		var (
			endpointID pgtype.UUID
			suspended  pgtype.Timestamptz
			endpoint   resolvedEndpoint
		)

		err := rows.Scan(
			&endpointID,
			&endpoint.URL,
			&endpoint.SigningSecret,
			&suspended,
		)
		if err != nil {
			return nil, err
		}

		endpoint.ID = fromPGUUID(endpointID)

		if suspended.Valid {
			value := suspended.Time
			endpoint.SuspendedUntil = &value
		}

		endpoints = append(
			endpoints,
			endpoint,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return endpoints, nil
}

func (s *AcceptanceStore) insertDeliveries(
	ctx context.Context,
	tx pgx.Tx,
	p appevent.AcceptParams,
	acceptedAt time.Time,
	endpoints []resolvedEndpoint,
) error {
	if len(endpoints) == 0 {
		return nil
	}

	retryDeadline := acceptedAt.Add(
		p.RetryHorizon,
	)

	rows := make(
		[][]any,
		0,
		len(endpoints),
	)

	for _, endpoint := range endpoints {
		deliveryID, err := s.ids.New()
		if err != nil {
			return fmt.Errorf(
				"generate delivery id: %w",
				err,
			)
		}

		availableAt := acceptedAt

		if endpoint.SuspendedUntil != nil &&
			endpoint.SuspendedUntil.After(availableAt) {

			availableAt = *endpoint.SuspendedUntil
		}

		rows = append(
			rows,
			[]any{
				toPGUUID(deliveryID),
				toPGUUID(p.EventID),
				toPGUUID(endpoint.ID),
				endpoint.URL,
				endpoint.SigningSecret,
				availableAt,
				p.MaxAttempts,
				retryDeadline,
			},
		)
	}

	count, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"deliveries"},
		[]string{
			"id",
			"event_id",
			"endpoint_id",
			"destination_url",
			"signing_secret",
			"available_at",
			"max_attempts",
			"retry_deadline_at",
		},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return err
	}

	if count != int64(len(rows)) {
		return fmt.Errorf(
			"expected to insert %d deliveries, inserted %d",
			len(rows),
			count,
		)
	}

	return nil
}

func toPGUUID(
	id uuid.UUID,
) pgtype.UUID {
	return pgtype.UUID{
		Bytes: [16]byte(id),
		Valid: true,
	}
}

func fromPGUUID(
	id pgtype.UUID,
) uuid.UUID {
	return uuid.UUID(id.Bytes)
}
