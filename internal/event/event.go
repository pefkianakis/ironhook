package event

import (
	"encoding/json"
	"time"
)

type Event struct {
	ID       string
	Type     string
	Payload  json.RawMessage
	CretedAt time.Time
}
