package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidAttempt = errors.New("invalid delivery attempt")

	// ErrStaleClaim means the worker no longer has a usable claim.
	//
	// This can mean the claim version changed, the lease expired,
	// another worker owns the delivery, the delivery is no longer
	// pending, or the retry attempt limit has already been reached.
	ErrStaleClaim = errors.New("delivery claim is stale")
)

type StartAttemptParams struct {
	DeliveryID uuid.UUID

	ClaimVersion int64
	WorkerID     string
}

type StartedAttempt struct {
	ID int64

	DeliveryID uuid.UUID

	AttemptNumber int
	ClaimVersion  int64
	WorkerID      string

	StartedAt time.Time
}

type AttemptStarter interface {
	StartAttempt(
		ctx context.Context,
		params StartAttemptParams,
	) (StartedAttempt, error)
}

func (p StartAttemptParams) Validate() error {
	switch {
	case p.DeliveryID == uuid.Nil:
		return fmt.Errorf(
			"%w: delivery id is required",
			ErrInvalidAttempt,
		)

	case p.ClaimVersion <= 0:
		return fmt.Errorf(
			"%w: claim version must be positive",
			ErrInvalidAttempt,
		)

	case p.WorkerID == "":
		return fmt.Errorf(
			"%w: worker id is required",
			ErrInvalidAttempt,
		)

	case strings.TrimSpace(p.WorkerID) != p.WorkerID:
		return fmt.Errorf(
			"%w: worker id contains surrounding whitespace",
			ErrInvalidAttempt,
		)
	}

	return nil
}
