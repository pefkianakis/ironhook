package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	appdelivery "github.com/cpefkianakis/ironhook/internal/app/delivery"
	"github.com/cpefkianakis/ironhook/internal/platform/postgres/sqlc"
)

type DeliveryClaimStore struct {
	queries *sqlc.Queries
}

func NewDeliveryClaimStore(
	pool *pgxpool.Pool,
) *DeliveryClaimStore {
	return &DeliveryClaimStore{
		queries: sqlc.New(pool),
	}
}

func (s *DeliveryClaimStore) Claim(
	ctx context.Context,
	params appdelivery.ClaimParams,
) ([]appdelivery.ClaimedDelivery, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	rows, err := s.queries.ClaimDeliveries(
		ctx,
		sqlc.ClaimDeliveriesParams{
			BatchLimit: int32(params.Limit),

			LeaseMilliseconds: params.Lease.Milliseconds(),

			WorkerID: pgtype.Text{
				String: params.WorkerID,
				Valid:  true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"claim deliveries: %w",
			err,
		)
	}

	deliveries := make(
		[]appdelivery.ClaimedDelivery,
		0,
		len(rows),
	)

	for _, row := range rows {
		retryDeadline, err := timestamptzValue(
			row.RetryDeadlineAt,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid retry_deadline_at for delivery %s: %w",
				row.ID,
				err,
			)
		}

		deliveries = append(
			deliveries,
			appdelivery.ClaimedDelivery{
				ID:         row.ID,
				EventID:    row.EventID,
				EndpointID: row.EndpointID,

				EventType: row.EventType,
				Payload:   row.Payload,

				DestinationURL: row.DestinationURL,
				SigningSecret:  row.SigningSecret,

				ClaimVersion: row.ClaimVersion,

				AttemptCount: int(row.AttemptCount),
				MaxAttempts:  int(row.MaxAttempts),

				RetryDeadlineAt: retryDeadline,
			},
		)
	}

	return deliveries, nil
}

var _ appdelivery.Claimer = (*DeliveryClaimStore)(nil)
