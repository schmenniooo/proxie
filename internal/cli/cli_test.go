package cli_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/schmenniooo/proxie/internal/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setEnv replaces every PROXIE_ variable of the process with vars for the
// duration of the test. Tests using it cannot run in parallel.
func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "PROXIE_") {
			t.Setenv(k, "") // restores the variable after the test
			require.NoError(t, os.Unsetenv(k))
		}
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

// validEnv returns a valid environment with Redis at redisAddr.
func validEnv(redisAddr string) map[string]string {
	return map[string]string{
		"PROXIE_UPSTREAM_URL": "https://api.example.com",
		"PROXIE_REDIS_URL":    "redis://" + redisAddr,
		"PROXIE_API_KEY_ACME": "key-secret",
	}
}

func run(ctx context.Context, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = cli.Run(ctx, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunHelp(t *testing.T) {
	setEnv(t, nil)

	code, stdout, _ := run(t.Context(), "--help")

	assert.Equal(t, 0, code)
	for _, want := range []string{"serve", "doctor", "config", "version", "--cache-ttl=1h", "$PROXIE_CACHE_TTL", "--log-level"} {
		assert.Contains(t, stdout, want)
	}
}

func TestRunUnknownFlag(t *testing.T) {
	setEnv(t, nil)

	code, _, stderr := run(t.Context(), "--bogus")

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "unknown flag --bogus")
}

func TestRunDefaultsToServe(t *testing.T) {
	setEnv(t, nil)

	// Flags without a command go to serve, which fails on the missing config.
	code, _, stderr := run(t.Context(), "--addr", ":9000")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "invalid configuration")
	assert.Contains(t, stderr, "--redis-url ($PROXIE_REDIS_URL): required")
}

func TestRunUnparsableEnvIsAUsageError(t *testing.T) {
	setEnv(t, map[string]string{"PROXIE_CACHE_TTL": "soon"})

	code, _, stderr := run(t.Context(), "config")

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "PROXIE_CACHE_TTL")
}
