package endpoint

import "time"

type Status string

const (
	StatusActive    Status = "active"
	StatusDisabled  Status = "disabled"
	StatusSuspended Status = "suspended"
)

type Endpoint struct {
	ID            string
	URL           string
	Description   string
	Status        Status
	SigningSecret []byte
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
