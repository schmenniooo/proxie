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

// Authenticator maps proxie keys to tenants.
//
// Every key is stored as its SHA-256 hash. A request key is hashed once and
// looked up, so the cost does not grow with the number of keys and the raw
// key is never compared: lookup timing can only depend on the hash, which
// reveals nothing usable about the key.
type Authenticator struct {
	tenants map[[sha256.Size]byte]config.Tenant
}

// New builds an Authenticator from the configured tenants. It fails if a key
// belongs to more than one tenant (config already rejects this; New checks
// again so it is safe to call with any input). Errors never repeat a key.
func New(tenants map[string]config.Tenant) (*Authenticator, error) {
	a := &Authenticator{tenants: map[[sha256.Size]byte]config.Tenant{}}
	for _, name := range slices.Sorted(maps.Keys(tenants)) {
		t := tenants[name]
		for _, key := range t.Keys {
			if key == "" {
				return nil, fmt.Errorf("tenant %q has an empty key", t.Name)
			}
			h := sha256.Sum256([]byte(key))
			if owner, dup := a.tenants[h]; dup && owner.Name != t.Name {
				return nil, fmt.Errorf("tenant %q shares a key with tenant %q", t.Name, owner.Name)
			}
			a.tenants[h] = t
		}
	}
	return a, nil
}

// Lookup returns the tenant that owns key. An empty or unknown key matches
// no tenant.
func (a *Authenticator) Lookup(key string) (config.Tenant, bool) {
	if key == "" {
		return config.Tenant{}, false
	}
	t, ok := a.tenants[sha256.Sum256([]byte(key))]
	return t, ok
}
