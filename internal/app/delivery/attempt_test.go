package delivery

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestStartAttemptParamsValidate(t *testing.T) {
	id := uuid.Must(uuid.NewV7())

	tests := []struct {
		name   string
		params StartAttemptParams
		valid  bool
	}{
		{
			name: "valid",
			params: StartAttemptParams{
				DeliveryID:   id,
				ClaimVersion: 1,
				WorkerID:     "worker-a",
			},
			valid: true,
		},
		{
			name: "missing delivery id",
			params: StartAttemptParams{
				ClaimVersion: 1,
				WorkerID:     "worker-a",
			},
		},
		{
			name: "zero claim version",
			params: StartAttemptParams{
				DeliveryID: id,
				WorkerID:   "worker-a",
			},
		},
		{
			name: "missing worker",
			params: StartAttemptParams{
				DeliveryID:   id,
				ClaimVersion: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()

			if tt.valid {
				if err != nil {
					t.Fatalf("expected valid params, got %v", err)
				}
				return
			}

			if !errors.Is(err, ErrInvalidAttempt) {
				t.Fatalf(
					"expected ErrInvalidAttempt, got %v",
					err,
				)
			}
		})
	}
}
