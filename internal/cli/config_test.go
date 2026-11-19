package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigText(t *testing.T) {
	env := validEnv("cache:6379")
	env["PROXIE_ADDR"] = ":8081"
	setEnv(t, env)

	code, stdout, _ := run(t.Context(), "config", "--addr", ":9000")

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "tenants.acme.keys          1\n")
	assert.Contains(t, stdout, "tenants.acme.upstream_url  https://api.example.com\n")
	assert.Contains(t, stdout, "addr                       :9000\n", "flags win over env vars")
	assert.Contains(t, stdout, "key_headers                content-type,anthropic-version,anthropic-beta\n")
}

func TestConfigJSON(t *testing.T) {
	setEnv(t, validEnv("cache:6379"))

	code, stdout, _ := run(t.Context(), "config", "--json", "--cache-ttl", "5m")

	assert.Equal(t, 0, code)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, "5m0s", got["cache_ttl"])
	assert.InDelta(t, 10485760, got["max_request_bytes"], 0)
	assert.Equal(t, map[string]any{"acme": map[string]any{"keys": float64(1), "upstream_url": "https://api.example.com"}}, got["tenants"])
}

func TestConfigMasksSecrets(t *testing.T) {
	setEnv(t, map[string]string{
		"PROXIE_UPSTREAM_URL":   "https://user:upstreamsecret@api.example.com/v1?key=querysecret",
		"PROXIE_REDIS_URL":      "redis://user:urlsecret@cache:6379",
		"PROXIE_REDIS_PASSWORD": "envsecret",
		"PROXIE_API_KEY_ACME":   "keysecret1,keysecret2",
	})

	for _, args := range [][]string{{"config"}, {"config", "--json"}} {
		code, stdout, _ := run(t.Context(), args...)

		assert.Equal(t, 0, code)
		for _, secret := range []string{"upstreamsecret", "querysecret", "urlsecret", "envsecret", "keysecret"} {
			assert.NotContains(t, stdout, secret)
		}
	}
}

func TestConfigInvalid(t *testing.T) {
	setEnv(t, nil)

	code, stdout, stderr := run(t.Context(), "config")

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "invalid configuration")
}
