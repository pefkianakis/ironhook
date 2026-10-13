package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidClaim = errors.New("invalid delivery claim")

type ClaimParams struct {
	WorkerID string
	Limit    int
	Lease    time.Duration
}

type ClaimedDelivery struct {
	ID         uuid.UUID
	EventID    uuid.UUID
	EndpointID uuid.UUID

	EventType string
	Payload   []byte

	DestinationURL string
	SigningSecret  []byte

	ClaimVersion int64

	AttemptCount int
	MaxAttempts  int

	RetryDeadlineAt time.Time
}

type Claimer interface {
	Claim(
		ctx context.Context,
		params ClaimParams,
	) ([]ClaimedDelivery, error)
}

func (p ClaimParams) Validate() error {
	switch {
	case p.WorkerID == "":
		return fmt.Errorf(
			"%w: worker id is required",
			ErrInvalidClaim,
		)

	case strings.TrimSpace(p.WorkerID) != p.WorkerID:
		return fmt.Errorf(
			"%w: worker id contains surrounding whitespace",
			ErrInvalidClaim,
		)

	case p.Limit <= 0:
		return fmt.Errorf(
			"%w: limit must be positive",
			ErrInvalidClaim,
		)

	case p.Lease <= 0:
		return fmt.Errorf(
			"%w: lease must be positive",
			ErrInvalidClaim,
		)
	}

	return nil
}
