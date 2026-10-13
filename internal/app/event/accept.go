package event

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pefkianakis/ironhook/internal/idgen"
)

var (
	ErrInvalidRequest      = errors.New("invalid event request")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
)

type RetryPolicy struct {
	MaxAttempts int
	Horizon     time.Duration
}

type AcceptCommand struct {
	Scope          string
	IdempotencyKey string

	EventType  string
	Payload    []byte
	OccurredAt *time.Time
}

type AcceptParams struct {
	EventID uuid.UUID

	Scope          string
	IdempotencyKey string
	RequestHash    [32]byte

	EventType  string
	Payload    []byte
	OccurredAt *time.Time

	MaxAttempts  int
	RetryHorizon time.Duration
}

type AcceptResult struct {
	EventID    uuid.UUID
	AcceptedAt time.Time
	Replayed   bool
}

type AcceptanceStore interface {
	Accept(
		ctx context.Context,
		params AcceptParams,
	) (AcceptResult, error)
}

type Service struct {
	store  AcceptanceStore
	ids    idgen.Generator
	policy RetryPolicy
}

func NewService(
	store AcceptanceStore,
	ids idgen.Generator,
	policy RetryPolicy,
) (*Service, error) {
	if store == nil {
		return nil, errors.New("acceptance store is required")
	}

	if ids == nil {
		return nil, errors.New("id generator is required")
	}

	if policy.MaxAttempts <= 0 {
		return nil, errors.New("max attempts must be positive")
	}

	if policy.Horizon <= 0 {
		return nil, errors.New("retry horizon must be positive")
	}

	return &Service{
		store:  store,
		ids:    ids,
		policy: policy,
	}, nil
}

func (s *Service) Accept(
	ctx context.Context,
	cmd AcceptCommand,
) (AcceptResult, error) {
	if err := validateCommand(cmd); err != nil {
		return AcceptResult{}, err
	}

	eventID, err := s.ids.New()
	if err != nil {
		return AcceptResult{}, fmt.Errorf("generate event id: %w", err)
	}

	payload := append([]byte(nil), cmd.Payload...)

	hash := requestHash(
		cmd.EventType,
		cmd.OccurredAt,
		payload,
	)

	params := AcceptParams{
		EventID: eventID,

		Scope:          cmd.Scope,
		IdempotencyKey: cmd.IdempotencyKey,
		RequestHash:    hash,

		EventType:  cmd.EventType,
		Payload:    payload,
		OccurredAt: cmd.OccurredAt,

		MaxAttempts:  s.policy.MaxAttempts,
		RetryHorizon: s.policy.Horizon,
	}

	return s.store.Accept(ctx, params)
}

func validateCommand(cmd AcceptCommand) error {
	switch {
	case cmd.Scope == "":
		return fmt.Errorf("%w: scope is required", ErrInvalidRequest)

	case strings.TrimSpace(cmd.Scope) != cmd.Scope:
		return fmt.Errorf("%w: scope contains surrounding whitespace", ErrInvalidRequest)

	case cmd.IdempotencyKey == "":
		return fmt.Errorf("%w: idempotency key is required", ErrInvalidRequest)

	case strings.TrimSpace(cmd.IdempotencyKey) != cmd.IdempotencyKey:
		return fmt.Errorf(
			"%w: idempotency key contains surrounding whitespace",
			ErrInvalidRequest,
		)

	case cmd.EventType == "":
		return fmt.Errorf("%w: event type is required", ErrInvalidRequest)

	case strings.TrimSpace(cmd.EventType) != cmd.EventType:
		return fmt.Errorf(
			"%w: event type contains surrounding whitespace",
			ErrInvalidRequest,
		)
	}

	return nil
}
