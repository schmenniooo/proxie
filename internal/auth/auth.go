// Package auth checks proxie keys and resolves the tenant they belong to.
//
// The tenants themselves are loaded from PROXIE_API_KEY_<TENANT> by
// internal/config. Keys are environment only and never logged.
package auth

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"slices"

	"github.com/schmenniooo/proxie/internal/config"
)

// Header carries the proxie key of a request. It is removed before the
// request is forwarded.
const Header = "X-Proxie-Key"

// keyHash is the SHA-256 hash of a proxie key.
type keyHash [sha256.Size]byte

func hashKey(key string) keyHash { return sha256.Sum256([]byte(key)) }

// Authenticator answers "which tenant owns this key?".
//
// config.Tenant lists keys per tenant, which is the wrong direction for a
// request: finding the owner would mean looping over every tenant and
// comparing raw keys. byKey is the inverted index of the same data, built
// once at startup: the hash of every key points to the tenant that owns it.
// A request key is hashed once and looked up, so the cost does not grow
// with the number of keys and the raw key is never compared: lookup timing
// can only depend on the hash, which reveals nothing usable about the key.
type Authenticator struct {
	byKey map[keyHash]config.Tenant
}

// New indexes the configured tenants by key hash. It fails if a key belongs
// to more than one tenant (config already rejects this; New checks again so
// it is safe to call with any input). Errors never repeat a key.
func New(tenants map[string]config.Tenant) (*Authenticator, error) {
	byKey := make(map[keyHash]config.Tenant)
	for _, name := range slices.Sorted(maps.Keys(tenants)) {
		tenant := tenants[name]
		for _, key := range tenant.Keys {
			if key == "" {
				return nil, fmt.Errorf("tenant %q has an empty key", tenant.Name)
			}
			h := hashKey(key)
			if owner, taken := byKey[h]; taken && owner.Name != tenant.Name {
				return nil, fmt.Errorf("tenant %q shares a key with tenant %q", tenant.Name, owner.Name)
			}
			byKey[h] = tenant
		}
	}
	return &Authenticator{byKey: byKey}, nil
}

// Lookup returns the tenant that owns key. An empty or unknown key matches
// no tenant.
func (a *Authenticator) Lookup(key string) (config.Tenant, bool) {
	if key == "" {
		return config.Tenant{}, false
	}
	tenant, ok := a.byKey[hashKey(key)]
	return tenant, ok
}
