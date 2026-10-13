package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	appdelivery "github.com/pefkianakis/ironhook/internal/app/delivery"
	"github.com/pefkianakis/ironhook/internal/platform/postgres/sqlc"
)

type DeliveryAttemptStore struct {
	queries *sqlc.Queries
}

func NewDeliveryAttemptStore(
	pool *pgxpool.Pool,
) *DeliveryAttemptStore {
	return &DeliveryAttemptStore{
		queries: sqlc.New(pool),
	}
}

func (s *DeliveryAttemptStore) StartAttempt(
	ctx context.Context,
	params appdelivery.StartAttemptParams,
) (appdelivery.StartedAttempt, error) {
	if err := params.Validate(); err != nil {
		return appdelivery.StartedAttempt{}, err
	}

	row, err := s.queries.StartDeliveryAttempt(
		ctx,
		sqlc.StartDeliveryAttemptParams{
			DeliveryID:   params.DeliveryID,
			ClaimVersion: params.ClaimVersion,
			WorkerID:     params.WorkerID,
		},
	)

	// This is an expected business outcome:
	//
	// The UPDATE inside StartDeliveryAttempt matched no delivery.
	// That means the worker no longer has authority to start
	// an attempt for this claim.
	if errors.Is(err, pgx.ErrNoRows) {
		return appdelivery.StartedAttempt{},
			appdelivery.ErrStaleClaim
	}

	if err != nil {
		return appdelivery.StartedAttempt{},
			fmt.Errorf(
				"start delivery attempt: %w",
				err,
			)
	}

	startedAt, err := timestamptzValue(
		row.StartedAt,
	)
	if err != nil {
		return appdelivery.StartedAttempt{},
			fmt.Errorf(
				"invalid attempt started_at: %w",
				err,
			)
	}

	return appdelivery.StartedAttempt{
		ID: row.ID,

		DeliveryID: row.DeliveryID,

		AttemptNumber: int(row.AttemptNumber),
		ClaimVersion:  row.ClaimVersion,
		WorkerID:      row.WorkerID,

		StartedAt: startedAt,
	}, nil

}

var _ appdelivery.AttemptStarter = (*DeliveryAttemptStore)(nil)
