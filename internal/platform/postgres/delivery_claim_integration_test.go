//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	appdelivery "github.com/cpefkianakis/ironhook/internal/app/delivery"
	"github.com/cpefkianakis/ironhook/internal/platform/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedClaimableDeliveries(
	t *testing.T,
	pool *pgxpool.Pool,
	count int,
) []uuid.UUID {
	t.Helper()

	ctx := context.Background()

	endpointID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(
		ctx,
		`
		INSERT INTO endpoints (
		    id,
		    url,
		    signing_secret
		)
		VALUES ($1, $2, $3)
		`,
		endpointID,
		"https://worker-test.example/webhook",
		[]byte("worker-test-secret"),
	)
	if err != nil {
		t.Fatalf(
			"insert endpoint: %v",
			err,
		)
	}

	deliveryIDs := make(
		[]uuid.UUID,
		0,
		count,
	)

	for i := 0; i < count; i++ {
		eventID, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}

		deliveryID, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}

		_, err = pool.Exec(
			ctx,
			`
			INSERT INTO events (
			    id,
			    event_type,
			    payload
			)
			VALUES ($1, $2, $3)
			`,
			eventID,
			"company.created",
			[]byte(
				fmt.Sprintf(
					`{"sequence":%d}`,
					i,
				),
			),
		)
		if err != nil {
			t.Fatalf(
				"insert event: %v",
				err,
			)
		}

		_, err = pool.Exec(
			ctx,
			`
			INSERT INTO deliveries (
			    id,
			    event_id,
			    endpoint_id,
			    destination_url,
			    signing_secret,
			    available_at,
			    max_attempts,
			    retry_deadline_at
			)
			VALUES (
			    $1,
			    $2,
			    $3,
			    $4,
			    $5,
			    now() - interval '1 second',
			    $6,
			    now() + interval '24 hours'
			)
			`,
			deliveryID,
			eventID,
			endpointID,
			"https://worker-test.example/webhook",
			[]byte("worker-test-secret"),
			8,
		)
		if err != nil {
			t.Fatalf(
				"insert delivery: %v",
				err,
			)
		}

		deliveryIDs = append(
			deliveryIDs,
			deliveryID,
		)
	}

	return deliveryIDs
}

func TestClaimConcurrentWorkersDoNotOverlap(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedClaimableDeliveries(
		t,
		pool,
		20,
	)

	store := postgres.NewDeliveryClaimStore(
		pool,
	)

	type result struct {
		worker     string
		deliveries []appdelivery.ClaimedDelivery
		err        error
	}

	results := make(
		chan result,
		2,
	)

	start := make(chan struct{})

	claim := func(worker string) {
		<-start

		deliveries, err := store.Claim(
			context.Background(),
			appdelivery.ClaimParams{
				WorkerID: worker,
				Limit:    10,
				Lease:    30 * time.Second,
			},
		)

		results <- result{
			worker:     worker,
			deliveries: deliveries,
			err:        err,
		}
	}

	go claim("worker-a")
	go claim("worker-b")

	close(start)

	first := <-results
	second := <-results

	if first.err != nil {
		t.Fatalf(
			"%s claim failed: %v",
			first.worker,
			first.err,
		)
	}

	if second.err != nil {
		t.Fatalf(
			"%s claim failed: %v",
			second.worker,
			second.err,
		)
	}

	if len(first.deliveries) != 10 {
		t.Fatalf(
			"%s expected 10 deliveries, got %d",
			first.worker,
			len(first.deliveries),
		)
	}

	if len(second.deliveries) != 10 {
		t.Fatalf(
			"%s expected 10 deliveries, got %d",
			second.worker,
			len(second.deliveries),
		)
	}

	seen := make(
		map[uuid.UUID]string,
		20,
	)

	for _, result := range []result{
		first,
		second,
	} {
		for _, delivery := range result.deliveries {
			if owner, exists := seen[delivery.ID]; exists {
				t.Fatalf(
					"delivery %s claimed by both %s and %s",
					delivery.ID,
					owner,
					result.worker,
				)
			}

			seen[delivery.ID] = result.worker

			if delivery.ClaimVersion != 1 {
				t.Fatalf(
					"delivery %s expected claim version 1, got %d",
					delivery.ID,
					delivery.ClaimVersion,
				)
			}
		}
	}

	if len(seen) != 20 {
		t.Fatalf(
			"expected 20 distinct claimed deliveries, got %d",
			len(seen),
		)
	}
}

func TestClaimDoesNotReturnActiveLease(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedClaimableDeliveries(
		t,
		pool,
		1,
	)

	store := postgres.NewDeliveryClaimStore(
		pool,
	)

	first, err := store.Claim(
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

	if len(first) != 1 {
		t.Fatalf(
			"expected first worker to claim 1 delivery, got %d",
			len(first),
		)
	}

	second, err := store.Claim(
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

	if len(second) != 0 {
		t.Fatalf(
			"expected active lease to block second claim, got %d deliveries",
			len(second),
		)
	}
}

func TestClaimRecoversExpiredLeaseAndIncrementsVersion(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedClaimableDeliveries(
		t,
		pool,
		1,
	)

	store := postgres.NewDeliveryClaimStore(
		pool,
	)

	first, err := store.Claim(
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

	if len(first) != 1 {
		t.Fatal(
			"expected worker-a to claim delivery",
		)
	}

	if first[0].ClaimVersion != 1 {
		t.Fatalf(
			"expected claim version 1, got %d",
			first[0].ClaimVersion,
		)
	}

	// Simulate passage of time without sleeping.
	_, err = pool.Exec(
		context.Background(),
		`
		UPDATE deliveries
		SET available_at = now() - interval '1 second'
		WHERE id = $1
		`,
		first[0].ID,
	)
	if err != nil {
		t.Fatalf(
			"expire lease: %v",
			err,
		)
	}

	second, err := store.Claim(
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

	if len(second) != 1 {
		t.Fatal(
			"expected worker-b to reclaim expired delivery",
		)
	}

	if second[0].ID != first[0].ID {
		t.Fatalf(
			"expected delivery %s, got %s",
			first[0].ID,
			second[0].ID,
		)
	}

	if second[0].ClaimVersion != 2 {
		t.Fatalf(
			"expected claim version 2, got %d",
			second[0].ClaimVersion,
		)
	}
}
