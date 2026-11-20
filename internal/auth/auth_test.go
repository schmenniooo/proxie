package auth_test

import (
	"testing"

	"github.com/schmenniooo/proxie/internal/auth"
	"github.com/schmenniooo/proxie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tenants() map[string]config.Tenant {
	return map[string]config.Tenant{
		"acme": {Name: "acme", Keys: []string{"acme-old", "acme-new"}},
		"beta": {Name: "beta", Keys: []string{"beta-key"}},
	}
}

func TestLookupResolvesTenant(t *testing.T) {
	a, err := auth.New(tenants())
	require.NoError(t, err)

	for key, want := range map[string]string{"acme-old": "acme", "acme-new": "acme", "beta-key": "beta"} {
		tenant, ok := a.Lookup(key)
		assert.True(t, ok, key)
		assert.Equal(t, want, tenant.Name, key)
	}
}

func TestLookupRejectsUnknownAndEmptyKeys(t *testing.T) {
	a, err := auth.New(tenants())
	require.NoError(t, err)

	for _, key := range []string{"", "nope", "acme-old ", "ACME-OLD"} {
		_, ok := a.Lookup(key)
		assert.False(t, ok, "%q", key)
	}
}

func TestNewRejectsSharedKeyWithoutRepeatingIt(t *testing.T) {
	ts := tenants()
	ts["gamma"] = config.Tenant{Name: "gamma", Keys: []string{"beta-key"}}

	_, err := auth.New(ts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `tenant "gamma" shares a key with tenant "beta"`)
	assert.NotContains(t, err.Error(), "beta-key")
}

func TestNewRejectsEmptyKey(t *testing.T) {
	_, err := auth.New(map[string]config.Tenant{"acme": {Name: "acme", Keys: []string{""}}})

	require.Error(t, err)
}
