package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/schmenniooo/proxie/internal/doctor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorInvalidConfigIsAFailedCheck(t *testing.T) {
	setEnv(t, nil)

	code, stdout, stderr := run(t.Context(), "doctor")

	assert.Equal(t, 1, code)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "fail    config")
	assert.Contains(t, stdout, "--redis-url ($PROXIE_REDIS_URL): required")
}

func TestDoctorUnparsableEnvIsAFailedCheck(t *testing.T) {
	setEnv(t, map[string]string{"PROXIE_CACHE_TTL": "soon"})

	code, stdout, _ := run(t.Context(), "doctor", "--json")

	assert.Equal(t, 1, code)
	var results []doctor.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &results))
	require.Len(t, results, 1)
	assert.Equal(t, doctor.Fail, results[0].Status)
	assert.Contains(t, results[0].Detail, "PROXIE_CACHE_TTL")
}

func TestDoctorJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	env := validEnv(mr.Addr())
	// A closed port, so the run does not depend on the network.
	env["PROXIE_UPSTREAM_URL"] = "http://127.0.0.1:1"
	setEnv(t, env)

	code, stdout, _ := run(t.Context(), "doctor", "--json")

	assert.Equal(t, 1, code, "the upstream check fails")
	var results []doctor.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &results))
	checks := map[string]string{}
	for _, r := range results {
		checks[r.Check] = r.Status
	}
	assert.Equal(t, map[string]string{
		"config":                      doctor.Pass,
		"redis":                       doctor.Pass,
		"redis maxmemory-policy":      doctor.Warn,
		"upstream http://127.0.0.1:1": doctor.Fail,
	}, checks)
}
