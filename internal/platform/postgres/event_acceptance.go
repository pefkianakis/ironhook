package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	appevent "github.com/pefkianakis/ironhook/internal/app/event"
	"github.com/pefkianakis/ironhook/internal/idgen"
	"github.com/pefkianakis/ironhook/internal/platform/postgres/sqlc"
)

type AcceptanceStore struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
	ids     idgen.Generator
}

func NewAcceptanceStore(
	pool *pgxpool.Pool,
	ids idgen.Generator,
) *AcceptanceStore {
	return &AcceptanceStore{
		pool:    pool,
		queries: sqlc.New(pool),
		ids:     ids,
	}
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
			fmt.Errorf("begin acceptance transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	q := s.queries.WithTx(tx)

	acceptedAt, err := q.InsertEvent(
		ctx,
		sqlc.InsertEventParams{
			ID:         p.EventID,
			EventType:  p.EventType,
			Payload:    p.Payload,
			OccurredAt: nullableTimestamptz(p.OccurredAt),
		},
	)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf("insert event: %w", err)
	}

	_, err = q.InsertIdempotencyRecord(
		ctx,
		sqlc.InsertIdempotencyRecordParams{
			Scope:          p.Scope,
			IdempotencyKey: p.IdempotencyKey,
			RequestHash:    p.RequestHash[:],
			EventID:        p.EventID,
		},
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return handleIdempotencyReplay(
			ctx,
			tx,
			q,
			p,
		)
	}

	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf("insert idempotency record: %w", err)
	}

	endpoints, err := q.ResolveEndpointsForEvent(
		ctx,
		sqlc.ResolveEndpointsForEventParams{
			EventType: p.EventType,
		},
	)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf("resolve subscriptions: %w", err)
	}

	acceptedTime, err := timestamptzValue(acceptedAt)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf("invalid accepted_at: %w", err)
	}

	if err := s.insertDeliveries(
		ctx,
		q,
		p,
		acceptedTime,
		endpoints,
	); err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf("insert deliveries: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf("commit acceptance transaction: %w", err)
	}

	return appevent.AcceptResult{
		EventID:    p.EventID,
		AcceptedAt: acceptedTime,
		Replayed:   false,
	}, nil
}

func handleIdempotencyReplay(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	p appevent.AcceptParams,
) (appevent.AcceptResult, error) {
	existing, err := q.GetIdempotencyRecord(
		ctx,
		sqlc.GetIdempotencyRecordParams{
			Scope:          p.Scope,
			IdempotencyKey: p.IdempotencyKey,
		},
	)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"get existing idempotency record: %w",
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

	acceptedAt, err := timestamptzValue(
		existing.AcceptedAt,
	)
	if err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"invalid existing accepted_at: %w",
				err,
			)
	}

	// Discard the speculative event inserted by this transaction.
	if err := tx.Rollback(ctx); err != nil {
		return appevent.AcceptResult{},
			fmt.Errorf(
				"rollback idempotent replay: %w",
				err,
			)
	}

	return appevent.AcceptResult{
		EventID:    existing.EventID,
		AcceptedAt: acceptedAt,
		Replayed:   true,
	}, nil
}

func (s *AcceptanceStore) insertDeliveries(
	ctx context.Context,
	q *sqlc.Queries,
	p appevent.AcceptParams,
	acceptedAt time.Time,
	endpoints []sqlc.ResolveEndpointsForEventRow,
) error {
	if len(endpoints) == 0 {
		return nil
	}

	retryDeadline := acceptedAt.Add(
		p.RetryHorizon,
	)

	deliveries := make(
		[]sqlc.InsertDeliveriesParams,
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

		if endpoint.SuspendedUntil.Valid &&
			endpoint.SuspendedUntil.Time.After(availableAt) {

			availableAt = endpoint.SuspendedUntil.Time
		}

		deliveries = append(
			deliveries,
			sqlc.InsertDeliveriesParams{
				ID:             deliveryID,
				EventID:        p.EventID,
				EndpointID:     endpoint.ID,
				DestinationURL: endpoint.URL,
				SigningSecret:  endpoint.SigningSecret,

				AvailableAt: timestamptz(
					availableAt,
				),

				MaxAttempts: int32(
					p.MaxAttempts,
				),

				RetryDeadlineAt: timestamptz(
					retryDeadline,
				),
			},
		)
	}

	inserted, err := q.InsertDeliveries(
		ctx,
		deliveries,
	)
	if err != nil {
		return err
	}

	if inserted != int64(len(deliveries)) {
		return fmt.Errorf(
			"expected to insert %d deliveries, inserted %d",
			len(deliveries),
			inserted,
		)
	}

	return nil
}

func timestamptz(
	value time.Time,
) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  value,
		Valid: true,
	}
}

func nullableTimestamptz(
	value *time.Time,
) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}

	return pgtype.Timestamptz{
		Time:  *value,
		Valid: true,
	}
}

func timestamptzValue(
	value pgtype.Timestamptz,
) (time.Time, error) {
	if !value.Valid {
		return time.Time{},
			errors.New("timestamp is null")
	}

	return value.Time, nil
}

var _ appevent.AcceptanceStore = (*AcceptanceStore)(nil)
