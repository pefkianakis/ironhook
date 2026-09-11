package endpoint

import "time"

type SubscriptionStatus string

const (
	SubscriptionStatusActive SubscriptionStatus = "active"
)

type Subscription struct {
	ID         string
	EndpointID string
	EventType  string
	Status     SubscriptionStatus
	CreatedAt  time.Time
}
