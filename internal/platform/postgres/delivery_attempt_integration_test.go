//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	appdelivery "github.com/pefkianakis/ironhook/internal/app/delivery"
	"github.com/pefkianakis/ironhook/internal/platform/postgres"
)

func assertDeliveryAttemptState(
	t *testing.T,
	pool *pgxpool.Pool,
	deliveryID any,
	expectedAttemptCount int,
	expectedAttemptRows int,
) {
	t.Helper()

	ctx := context.Background()

	var attemptCount int

	err := pool.QueryRow(
		ctx,
		`
		SELECT attempt_count
		FROM deliveries
		WHERE id = $1
		`,
		deliveryID,
	).Scan(&attemptCount)

	if err != nil {
		t.Fatalf(
			"read deliveryattempt count: %v",
			err,
		)
	}

	if attemptCount != expectedAttemptCount {
		t.Fatalf(
			"expected delivery attempt_count %d, got %d",
			expectedAttemptCount,
			attemptCount,
		)
	}

	var attemptRows int

	err = pool.QueryRow(
		ctx,
		`
		SELECT count(*)
		FROM delivery_attempts
		WHERE delivery_id = $1
		`,
		deliveryID,
	).Scan(&attemptRows)

	if err != nil {
		t.Fatalf(
			"expected %d attempt rows, got %d",
			expectedAttemptRows,
			attemptRows,
		)
	}

}

func TestStartAttemptWithCurrentClaim(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedClaimableDeliveries(
		t,
		pool,
		1,
	)

	claimStore := postgres.NewDeliveryClaimStore(
		pool,
	)

	attemptStore := postgres.NewDeliveryAttemptStore(
		pool,
	)

	claimed, err := claimStore.Claim(
		context.Background(),
		appdelivery.ClaimParams{
			WorkerID: "worker-a",
			Limit:    1,
			Lease:    30 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(claimed) != 1 {
		t.Fatalf(
			"expected one claimed delivery, got %d",
			len(claimed),
		)
	}

	delivery := claimed[0]

	attempt, err := attemptStore.StartAttempt(
		context.Background(),
		appdelivery.StartAttemptParams{
			DeliveryID:   delivery.ID,
			ClaimVersion: delivery.ClaimVersion,
			WorkerID:     "worker-a",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if attempt.DeliveryID != delivery.ID {
		t.Fatalf(
			"expected delivery %s, got %s",
			delivery.ID,
			attempt.DeliveryID,
		)
	}

	if attempt.AttemptNumber != 1 {
		t.Fatalf(
			"expected attempt number 1, got %d",
			attempt.AttemptNumber,
		)
	}

	if attempt.ClaimVersion != 1 {
		t.Fatalf(
			"expected claim version 1, got %d",
			attempt.ClaimVersion,
		)
	}

	if attempt.WorkerID != "worker-a" {
		t.Fatalf(
			"expected worker-a, got %s",
			attempt.WorkerID,
		)
	}

	assertDeliveryAttemptState(
		t,
		pool,
		delivery.ID,
		1,
		1,
	)
}

func TestStartAttemptRejectsStaleClaim(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedClaimableDeliveries(
		t,
		pool,
		1,
	)

	claimStore := postgres.NewDeliveryClaimStore(
		pool,
	)

	attemptStore := postgres.NewDeliveryAttemptStore(
		pool,
	)

	firstClaim, err := claimStore.Claim(
		context.Background(),
		appdelivery.ClaimParams{
			WorkerID: "worker-a",
			Limit:    1,
			Lease:    30 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(firstClaim) != 1 {
		t.Fatal(
			"expected worker-a to claim delivery",
		)
	}

	delivery := firstClaim[0]

	// Simulate worker A's lease expiring.
	_, err = pool.Exec(
		context.Background(),
		`
		UPDATE deliveries
		SET available_at = now() - interval '1 second'
		WHERE id = $1
		`,
		delivery.ID,
	)
	if err != nil {
		t.Fatalf(
			"expire worker-a lease: %v",
			err,
		)
	}

	secondClaim, err := claimStore.Claim(
		context.Background(),
		appdelivery.ClaimParams{
			WorkerID: "worker-b",
			Limit:    1,
			Lease:    30 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(secondClaim) != 1 {
		t.Fatal(
			"expected worker-b to reclaim delivery",
		)
	}

	if secondClaim[0].ClaimVersion != 2 {
		t.Fatalf(
			"expected claim version 2, got %d",
			secondClaim[0].ClaimVersion,
		)
	}

	// Worker A still has its old token: version 1.
	_, err = attemptStore.StartAttempt(
		context.Background(),
		appdelivery.StartAttemptParams{
			DeliveryID:   delivery.ID,
			ClaimVersion: delivery.ClaimVersion,
			WorkerID:     "worker-a",
		},
	)

	if !errors.Is(
		err,
		appdelivery.ErrStaleClaim,
	) {
		t.Fatalf(
			"expected stale claim error, got %v",
			err,
		)
	}

	assertDeliveryAttemptState(
		t,
		pool,
		delivery.ID,
		0,
		0,
	)
}

func TestStartAttemptRejectsExpiredLease(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedClaimableDeliveries(
		t,
		pool,
		1,
	)

	claimStore := postgres.NewDeliveryClaimStore(
		pool,
	)

	attemptStore := postgres.NewDeliveryAttemptStore(
		pool,
	)

	claimed, err := claimStore.Claim(
		context.Background(),
		appdelivery.ClaimParams{
			WorkerID: "worker-a",
			Limit:    1,
			Lease:    30 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	delivery := claimed[0]

	_, err = pool.Exec(
		context.Background(),
		`
		UPDATE deliveries
		SET available_at = now() - interval '1 second'
		WHERE id = $1
		`,
		delivery.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = attemptStore.StartAttempt(
		context.Background(),
		appdelivery.StartAttemptParams{
			DeliveryID:   delivery.ID,
			ClaimVersion: delivery.ClaimVersion,
			WorkerID:     "worker-a",
		},
	)

	if !errors.Is(
		err,
		appdelivery.ErrStaleClaim,
	) {
		t.Fatalf(
			"expected stale claim error, got %v",
			err,
		)
	}

	assertDeliveryAttemptState(
		t,
		pool,
		delivery.ID,
		0,
		0,
	)
}

func TestStartAttemptRejectsSecondUnfinishedAttempt(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedClaimableDeliveries(
		t,
		pool,
		1,
	)

	claimStore := postgres.NewDeliveryClaimStore(
		pool,
	)

	attemptStore := postgres.NewDeliveryAttemptStore(
		pool,
	)

	claimed, err := claimStore.Claim(
		context.Background(),
		appdelivery.ClaimParams{
			WorkerID: "worker-a",
			Limit:    1,
			Lease:    30 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	delivery := claimed[0]

	_, err = attemptStore.StartAttempt(
		context.Background(),
		appdelivery.StartAttemptParams{
			DeliveryID:   delivery.ID,
			ClaimVersion: delivery.ClaimVersion,
			WorkerID:     "worker-a",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = attemptStore.StartAttempt(
		context.Background(),
		appdelivery.StartAttemptParams{
			DeliveryID:   delivery.ID,
			ClaimVersion: delivery.ClaimVersion,
			WorkerID:     "worker-a",
		},
	)

	if !errors.Is(
		err,
		appdelivery.ErrStaleClaim,
	) {
		t.Fatalf(
			"expected second attempt to be rejected, got %v",
			err,
		)
	}

	assertDeliveryAttemptState(
		t,
		pool,
		delivery.ID,
		1,
		1,
	)
}
