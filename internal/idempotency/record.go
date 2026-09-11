package idempotency

import "time"

type Record struct {
	Scope      string
	Key        string
	RquestHash []byte
	EventID    string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}
