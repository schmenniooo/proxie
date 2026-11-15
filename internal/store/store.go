// Package store holds cache entries. Redis is the only implementation.
package store

import (
	"context"
	"time"
)

// Store keeps cache entries as opaque bytes. Implementations bound every
// call with a short timeout and return errors instead of hiding them.
// Callers fail open: an error from Get is treated as a miss and an error
// from Set means the entry is not stored, so the request still succeeds.
type Store interface {
	// Get returns the value for key. A missing key returns found == false
	// and a nil error.
	Get(ctx context.Context, key string) (val []byte, found bool, err error)
	// Set stores val under key. The entry expires after ttl.
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	// Ping checks that the store is reachable.
	Ping(ctx context.Context) error
	// Close releases the connections.
	Close() error
}
