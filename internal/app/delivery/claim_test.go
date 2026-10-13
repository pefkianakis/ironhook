package delivery

import (
	"errors"
	"testing"
	"time"
)

func TestClaimParamsValidate(t *testing.T) {
	tests := []struct {
		name   string
		params ClaimParams
		valid  bool
	}{
		{
			name: "valid",
			params: ClaimParams{
				WorkerID: "worker-a",
				Limit:    10,
				Lease:    30 * time.Second,
			},
			valid: true,
		},
		{
			name: "zero limit",
			params: ClaimParams{
				WorkerID: "worker-a",
				Lease:    30 * time.Second,
			},
		},
		{
			name: "zero lease",
			params: ClaimParams{
				WorkerID: "worker-a",
				Limit:    10,
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

			if !errors.Is(err, ErrInvalidClaim) {
				t.Fatalf(
					"expected ErrInvalidClaim, got %v",
					err,
				)
			}
		})
	}
}
