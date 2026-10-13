//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	appevent "github.com/pefkianakis/ironhook/internal/app/event"
	"github.com/pefkianakis/ironhook/internal/idgen"
	"github.com/pefkianakis/ironhook/internal/platform/postgres"
)

func testPool(
	t *testing.T,
) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv(
		"TEST_DATABASE_URL",
	)

	if databaseURL == "" {
		t.Fatal(
			"TEST_DATABASE_URL is required",
		)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"create postgres pool: %v",
			err,
		)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		t.Fatalf(
			"ping postgres: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	return pool
}

func resetDatabase(
	t *testing.T,
	pool *pgxpool.Pool,
) {
	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		`
		TRUNCATE TABLE
		    delivery_attempts,
		    deliveries,
		    idempotency_records,
		    events,
		    subscriptions,
		    endpoints
		RESTART IDENTITY CASCADE
		`,
	)
	if err != nil {
		t.Fatalf(
			"reset database: %v",
			err,
		)
	}
}

func seedSubscribedEndpoint(
	t *testing.T,
	pool *pgxpool.Pool,
	eventType string,
	url string,
) uuid.UUID {
	t.Helper()

	endpointID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}

	subscriptionID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

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
		endpointID.String(),
		url,
		[]byte("test-secret"),
	)
	if err != nil {
		t.Fatalf(
			"insert endpoint: %v",
			err,
		)
	}

	_, err = pool.Exec(
		ctx,
		`
		INSERT INTO subscriptions (
		    id,
		    endpoint_id,
		    event_type
		)
		VALUES ($1, $2, $3)
		`,
		subscriptionID.String(),
		endpointID.String(),
		eventType,
	)
	if err != nil {
		t.Fatalf(
			"insert subscription: %v",
			err,
		)
	}

	return endpointID
}

func newAcceptanceService(
	t *testing.T,
	pool *pgxpool.Pool,
) *appevent.Service {
	t.Helper()

	ids := idgen.UUIDv7{}

	store := postgres.NewAcceptanceStore(
		pool,
		ids,
	)

	service, err := appevent.NewService(
		store,
		ids,
		appevent.RetryPolicy{
			MaxAttempts: 8,
			Horizon:     24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return service
}

func TestAcceptConcurrentSameIdempotencyKey(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedSubscribedEndpoint(
		t,
		pool,
		"company.created",
		"https://one.example/webhook",
	)

	seedSubscribedEndpoint(
		t,
		pool,
		"company.created",
		"https://two.example/webhook",
	)

	service := newAcceptanceService(
		t,
		pool,
	)

	cmd := appevent.AcceptCommand{
		Scope:          "integration-test",
		IdempotencyKey: "request-123",
		EventType:      "company.created",
		Payload:        []byte(`{"company_id":42}`),
	}

	const workers = 8

	type result struct {
		value appevent.AcceptResult
		err   error
	}

	start := make(chan struct{})

	results := make(
		chan result,
		workers,
	)

	var wg sync.WaitGroup

	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()

			<-start

			value, err := service.Accept(
				context.Background(),
				cmd,
			)

			results <- result{
				value: value,
				err:   err,
			}
		}()
	}

	close(start)

	wg.Wait()
	close(results)

	var (
		eventID       uuid.UUID
		newCount      int
		replayedCount int
	)

	for result := range results {
		if result.err != nil {
			t.Fatalf(
				"accept event: %v",
				result.err,
			)
		}

		if eventID == uuid.Nil {
			eventID = result.value.EventID
		}

		if result.value.EventID != eventID {
			t.Fatalf(
				"expected all requests to return event %s, got %s",
				eventID,
				result.value.EventID,
			)
		}

		if result.value.Replayed {
			replayedCount++
		} else {
			newCount++
		}
	}

	if newCount != 1 {
		t.Fatalf(
			"expected exactly one new acceptance, got %d",
			newCount,
		)
	}

	if replayedCount != workers-1 {
		t.Fatalf(
			"expected %d replays, got %d",
			workers-1,
			replayedCount,
		)
	}

	assertCount(
		t,
		pool,
		"events",
		1,
	)

	assertCount(
		t,
		pool,
		"idempotency_records",
		1,
	)

	assertCount(
		t,
		pool,
		"deliveries",
		2,
	)
}

func TestAcceptRejectsIdempotencyKeyReuse(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedSubscribedEndpoint(
		t,
		pool,
		"company.created",
		"https://example.com/webhook",
	)

	service := newAcceptanceService(
		t,
		pool,
	)

	_, err := service.Accept(
		context.Background(),
		appevent.AcceptCommand{
			Scope:          "integration-test",
			IdempotencyKey: "same-key",
			EventType:      "company.created",
			Payload:        []byte(`{"company_id":1}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Accept(
		context.Background(),
		appevent.AcceptCommand{
			Scope:          "integration-test",
			IdempotencyKey: "same-key",
			EventType:      "company.created",
			Payload:        []byte(`{"company_id":2}`),
		},
	)

	if !errors.Is(
		err,
		appevent.ErrIdempotencyConflict,
	) {
		t.Fatalf(
			"expected idempotency conflict, got %v",
			err,
		)
	}

	assertCount(
		t,
		pool,
		"events",
		1,
	)

	assertCount(
		t,
		pool,
		"idempotency_records",
		1,
	)

	assertCount(
		t,
		pool,
		"deliveries",
		1,
	)
}

func TestAcceptanceRollsBackWhenFanoutFails(
	t *testing.T,
) {
	pool := testPool(t)
	resetDatabase(t, pool)

	seedSubscribedEndpoint(
		t,
		pool,
		"company.created",
		"https://example.com/webhook",
	)

	ids := idgen.UUIDv7{}

	store := postgres.NewAcceptanceStore(
		pool,
		ids,
	)

	eventID, err := ids.New()
	if err != nil {
		t.Fatal(err)
	}

	var requestHash [32]byte
	requestHash[0] = 1

	_, err = store.Accept(
		context.Background(),
		appevent.AcceptParams{
			EventID: eventID,

			Scope:          "integration-test",
			IdempotencyKey: "rollback-test",
			RequestHash:    requestHash,

			EventType: "company.created",
			Payload:   []byte(`{"company_id":42}`),

			// Deliberately invalid.
			// deliveries.max_attempts has CHECK > 0.
			MaxAttempts: 0,

			RetryHorizon: 24 * time.Hour,
		},
	)

	if err == nil {
		t.Fatal(
			"expected fan-out to fail",
		)
	}

	assertCount(
		t,
		pool,
		"events",
		0,
	)

	assertCount(
		t,
		pool,
		"idempotency_records",
		0,
	)

	assertCount(
		t,
		pool,
		"deliveries",
		0,
	)
}

func assertCount(
	t *testing.T,
	pool *pgxpool.Pool,
	table string,
	expected int,
) {
	t.Helper()

	allowed := map[string]bool{
		"events":              true,
		"idempotency_records": true,
		"deliveries":          true,
	}

	if !allowed[table] {
		t.Fatalf(
			"unsupported table %q",
			table,
		)
	}

	query := "SELECT count(*) FROM " + table

	var actual int

	if err := pool.QueryRow(
		context.Background(),
		query,
	).Scan(&actual); err != nil {
		t.Fatalf(
			"count %s: %v",
			table,
			err,
		)
	}

	if actual != expected {
		t.Fatalf(
			"%s: expected %d rows, got %d",
			table,
			expected,
			actual,
		)
	}
}
