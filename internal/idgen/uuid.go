package idgen

import "github.com/google/uuid"

type Generator interface {
	New() (uuid.UUID, error)
}

type UUIDv7 struct{}

func (UUIDv7) New() (uuid.UUID, error) {
	return uuid.NewV7()
}
