package event

import (
	"testing"
	"time"
)

func TestRequestHashIsDeterministic(t *testing.T) {
	occurredAt := time.Date(
		2026,
		time.October,
		13,
		12,
		30,
		0,
		123,
		time.UTC,
	)

	first := requestHash(
		"company.created",
		&occurredAt,
		[]byte(`{"id":42}`),
	)

	second := requestHash(
		"company.created",
		&occurredAt,
		[]byte(`{"id":42}`),
	)

	if first != second {
		t.Fatal("expected identical requests to produce identical hashes")
	}
}

func TestRequestHashChangesWhenPayloadChanges(t *testing.T) {
	first := requestHash(
		"company.created",
		nil,
		[]byte(`{"id":42}`),
	)

	second := requestHash(
		"company.created",
		nil,
		[]byte(`{"id":43}`),
	)

	if first == second {
		t.Fatal("expected different payloads to produce different hashes")
	}
}

func TestRequestHashChangesWhenEventTypeChanges(t *testing.T) {
	payload := []byte(`{"id":42}`)

	first := requestHash(
		"company.created",
		nil,
		payload,
	)

	second := requestHash(
		"company.deleted",
		nil,
		payload,
	)

	if first == second {
		t.Fatal("expected different event types to produce different hashes")
	}
}

func TestRequestHashNormalizesOccurredAtToUTC(t *testing.T) {
	firstTime := time.Date(
		2026,
		time.October,
		13,
		15,
		0,
		0,
		0,
		time.FixedZone("UTC+3", 3*60*60),
	)

	secondTime := time.Date(
		2026,
		time.October,
		13,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	first := requestHash(
		"company.created",
		&firstTime,
		[]byte(`{"id":42}`),
	)

	second := requestHash(
		"company.created",
		&secondTime,
		[]byte(`{"id":42}`),
	)

	if first != second {
		t.Fatal("expected equivalent instants to produce identical hashes")
	}
}
