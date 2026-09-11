package delivery

import "time"

type Attempt struct {
	ID            string
	DeliveryID    string
	AttemptNumber int
	EndpointURL   string
	HTTPStatus    *int
	ErrorCode     *string
	StartedAt     time.Time
	FinishedAt    time.Time
}
