package event

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"time"
)

const hashFormat = "ironhook:event-request:v1"

func requestHash(
	eventType string,
	occurredAt *time.Time,
	payload []byte,
) [32]byte {
	h := sha256.New()

	writeHashField(h, []byte(hashFormat))
	writeHashField(h, []byte(eventType))

	if occurredAt == nil {
		writeHashField(h, nil)
	} else {
		normalized := occurredAt.
			UTC().
			Format(time.RFC3339Nano)

		writeHashField(h, []byte(normalized))
	}

	writeHashField(h, payload)

	var result [32]byte
	copy(result[:], h.Sum(nil))

	return result
}

func writeHashField(h hash.Hash, value []byte) {
	var length [8]byte

	binary.BigEndian.PutUint64(
		length[:],
		uint64(len(value)),
	)

	_, _ = h.Write(length[:])
	_, _ = h.Write(value)
}
