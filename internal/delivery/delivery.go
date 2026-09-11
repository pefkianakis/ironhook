package delivery

import "time"

type Status string

const (
	StatusPending    Status = "pending"
	StatusDelivering Status = "delivering"
	StatusDelivered  Status = "delivered"
	StatusFailed     Status = "failed"
)

type Delivery struct {
	ID         string
	EventID    string
	EndpointId string

	Status       Status
	AttemptCount int

	NextAttemptAt *time.Time
	RetryUntil    time.Time

	ClaimedAt    *time.Time
	ClaimVersion int64

	ReplayOfDeliveryID *string

	CreatedAt   time.Time
	DeliveredAt *time.Time
}
