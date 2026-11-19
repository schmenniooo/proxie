package cli_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

func TestServeStartsAndStops(t *testing.T) {
	mr := miniredis.RunT(t)
	env := validEnv(mr.Addr())
	env["PROXIE_ADDR"] = "127.0.0.1:0"
	setEnv(t, env)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	code, stdout, _ := run(ctx, "serve", "--log-format", "text")

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "msg=listening")
	assert.Contains(t, stdout, `msg="proxie stopped"`)
	assert.NotContains(t, stdout, "key-secret")
}

func TestServeInvalidConfig(t *testing.T) {
	setEnv(t, map[string]string{"PROXIE_LOG_FORMAT": "xml"})

	code, stdout, stderr := run(t.Context(), "serve")

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "PROXIE_API_KEY_<TENANT>: at least one tenant required")
	assert.Contains(t, stderr, "--log-format ($PROXIE_LOG_FORMAT): must be json or text")
}

func TestServeListenError(t *testing.T) {
	mr := miniredis.RunT(t)
	setEnv(t, validEnv(mr.Addr()))

	// The Redis port is already taken.
	code, stdout, _ := run(t.Context(), "serve", "--addr", mr.Addr())

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "listen failed")
}
