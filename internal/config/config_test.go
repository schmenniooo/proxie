package config_test

import (
	"bytes"
	"log/slog"
	"maps"
	"net/url"
	"testing"
	"time"

	"github.com/schmenniooo/proxie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// env returns KEY=value entries for the required variables plus overrides.
// An empty override value means "set but empty"; use unset to remove a
// variable.
func env(overrides map[string]string, unset ...string) []string {
	vars := map[string]string{
		"PROXIE_UPSTREAM_URL": "https://api.example.com",
		"PROXIE_REDIS_URL":    "redis://localhost:6379",
		"PROXIE_API_KEY_ACME": "acme-key",
	}
	maps.Copy(vars, overrides)
	for _, k := range unset {
		delete(vars, k)
	}
	environ := make([]string, 0, len(vars))
	for k, v := range vars {
		environ = append(environ, k+"="+v)
	}
	return environ
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(nil))
	require.NoError(t, err)

	assert.Equal(t, map[string]config.Tenant{
		"acme": {Name: "acme", Keys: []string{"acme-key"}, UpstreamURL: mustURL(t, "https://api.example.com")},
	}, cfg.Tenants)
	assert.Equal(t, "redis://localhost:6379", cfg.RedisURL.String())
	assert.Empty(t, cfg.RedisPassword)
	assert.Equal(t, time.Hour, cfg.CacheTTL)
	assert.Empty(t, cfg.IgnoreFields)
	assert.Equal(t, config.DefaultKeyHeaders, cfg.KeyHeaders)
	assert.Equal(t, int64(10<<20), cfg.MaxRequestBytes)
	assert.Equal(t, int64(10<<20), cfg.MaxEntryBytes)
	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, ":9090", cfg.AdminAddr)
	assert.Equal(t, 120*time.Second, cfg.UpstreamTimeout)
	assert.Equal(t, 30*time.Second, cfg.StreamFirstByteTimeout)
	assert.Equal(t, 30*time.Second, cfg.StreamIdleTimeout)
	assert.Equal(t, 200*time.Millisecond, cfg.RedisTimeout)
	assert.Equal(t, 25*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, config.Log{
		Level:          slog.LevelInfo,
		Format:         "json",
		FileMaxMB:      100,
		FileMaxBackups: 5,
		FileMaxAgeDays: 14,
	}, cfg.Log)
}

func TestLoadOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		"PROXIE_UPSTREAM_URL":              "http://upstream:8000/v1",
		"PROXIE_REDIS_URL":                 "rediss://cache:6380/2",
		"PROXIE_REDIS_PASSWORD":            "hunter2",
		"PROXIE_CACHE_TTL":                 "15m",
		"PROXIE_IGNORE_FIELDS":             " user , metadata ,,",
		"PROXIE_KEY_HEADERS":               "Content-Type, OpenAI-Beta",
		"PROXIE_MAX_REQUEST_BYTES":         "2048",
		"PROXIE_MAX_ENTRY_BYTES":           "4096",
		"PROXIE_ADDR":                      "127.0.0.1:8081",
		"PROXIE_ADMIN_ADDR":                ":9091",
		"PROXIE_UPSTREAM_TIMEOUT":          "1m",
		"PROXIE_STREAM_FIRST_BYTE_TIMEOUT": "10s",
		"PROXIE_STREAM_IDLE_TIMEOUT":       "5s",
		"PROXIE_REDIS_TIMEOUT":             "50ms",
		"PROXIE_SHUTDOWN_TIMEOUT":          "5s",
		"PROXIE_LOG_LEVEL":                 "DEBUG",
		"PROXIE_LOG_FORMAT":                "Text",
		"PROXIE_LOG_FILE":                  "/var/log/proxie/proxie.log",
		"PROXIE_LOG_FILE_MAX_MB":           "10",
		"PROXIE_LOG_FILE_MAX_BACKUPS":      "0",
		"PROXIE_LOG_FILE_MAX_AGE_DAYS":     "0",
	}))
	require.NoError(t, err)

	assert.Equal(t, "http://upstream:8000/v1", cfg.Tenants["acme"].UpstreamURL.String())
	assert.Equal(t, "rediss://cache:6380/2", cfg.RedisURL.String())
	assert.Equal(t, "hunter2", cfg.RedisPassword)
	assert.Equal(t, 15*time.Minute, cfg.CacheTTL)
	assert.Equal(t, []string{"user", "metadata"}, cfg.IgnoreFields)
	assert.Equal(t, []string{"content-type", "openai-beta"}, cfg.KeyHeaders)
	assert.Equal(t, int64(2048), cfg.MaxRequestBytes)
	assert.Equal(t, int64(4096), cfg.MaxEntryBytes)
	assert.Equal(t, "127.0.0.1:8081", cfg.Addr)
	assert.Equal(t, ":9091", cfg.AdminAddr)
	assert.Equal(t, time.Minute, cfg.UpstreamTimeout)
	assert.Equal(t, 10*time.Second, cfg.StreamFirstByteTimeout)
	assert.Equal(t, 5*time.Second, cfg.StreamIdleTimeout)
	assert.Equal(t, 50*time.Millisecond, cfg.RedisTimeout)
	assert.Equal(t, 5*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, config.Log{
		Level:          slog.LevelDebug,
		Format:         "text",
		File:           "/var/log/proxie/proxie.log",
		FileMaxMB:      10,
		FileMaxBackups: 0,
		FileMaxAgeDays: 0,
	}, cfg.Log)
}

func TestLoadRequiredVars(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"PROXIE_REDIS_URL"} {
		t.Run("unset "+name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(env(nil, name))
			require.Error(t, err)
			assert.Contains(t, err.Error(), name+": required")
		})
		t.Run("empty "+name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(env(map[string]string{name: "  "}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), name+": required")
		})
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"PROXIE_UPSTREAM_URL":          "ftp://example.com",
		"PROXIE_UPSTREAM_URL_ACME":     "example.com/v1",
		"PROXIE_REDIS_URL":             "https://localhost:6379",
		"PROXIE_CACHE_TTL":             "1 hour",
		"PROXIE_REDIS_TIMEOUT":         "-1s",
		"PROXIE_SHUTDOWN_TIMEOUT":      "0s",
		"PROXIE_MAX_REQUEST_BYTES":     "10MB",
		"PROXIE_MAX_ENTRY_BYTES":       "0",
		"PROXIE_ADDR":                  "8080",
		"PROXIE_ADMIN_ADDR":            "localhost",
		"PROXIE_LOG_LEVEL":             "verbose",
		"PROXIE_LOG_FORMAT":            "xml",
		"PROXIE_LOG_FILE_MAX_MB":       "0",
		"PROXIE_LOG_FILE_MAX_BACKUPS":  "-1",
		"PROXIE_LOG_FILE_MAX_AGE_DAYS": "two",
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(env(map[string]string{name: value}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), name+":")
		})
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{"PROXIE_CACHE_TTL": "soon"}, "PROXIE_API_KEY_ACME", "PROXIE_REDIS_URL"))
	require.Error(t, err)

	assert.Contains(t, err.Error(), "PROXIE_API_KEY_<TENANT>: at least one tenant required")
	assert.Contains(t, err.Error(), "PROXIE_REDIS_URL: required")
	assert.Contains(t, err.Error(), "PROXIE_CACHE_TTL:")
}

func TestLoadInvalidURLErrorHidesPassword(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{"PROXIE_REDIS_URL": "redis://:s3cret@host:bad port"}))
	require.Error(t, err)

	assert.NotContains(t, err.Error(), "s3cret")
}

func TestLoadKeyHeadersSetButEmptyMeansNone(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{"PROXIE_KEY_HEADERS": ""}))
	require.NoError(t, err)

	assert.Empty(t, cfg.KeyHeaders)
	assert.NotNil(t, cfg.KeyHeaders)
}

func TestLoadDoesNotShareDefaultKeyHeaders(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(nil))
	require.NoError(t, err)
	cfg.KeyHeaders[0] = "changed"

	assert.Equal(t, "content-type", config.DefaultKeyHeaders[0])
}

func TestConfigLogValueMasksSecrets(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		"PROXIE_UPSTREAM_URL":   "https://user:upstreamsecret@api.example.com/v1?key=querysecret",
		"PROXIE_REDIS_URL":      "redis://user:urlsecret@cache:6379",
		"PROXIE_REDIS_PASSWORD": "envsecret",
		"PROXIE_API_KEY_ACME":   "keysecret1,keysecret2",
	}))
	require.NoError(t, err)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)

	for _, secret := range []string{"upstreamsecret", "querysecret", "urlsecret", "envsecret", "keysecret"} {
		assert.NotContains(t, buf.String(), secret)
	}
	assert.Contains(t, buf.String(), "api.example.com/v1")
	assert.Contains(t, buf.String(), `"redis_password_set":true`)
	assert.Contains(t, buf.String(), `"tenants":{"acme":{"keys":2,`)
	assert.Contains(t, buf.String(), "cache:6379")
}

func TestConfigLogValueShowsReadableDurations(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(nil))
	require.NoError(t, err)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)

	assert.Contains(t, buf.String(), `"cache_ttl":"1h0m0s"`)
	assert.Contains(t, buf.String(), `"redis_timeout":"200ms"`)
}

func TestLoadTenants(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		"PROXIE_API_KEY_ACME":            " key-1 , key-2 ,,",
		"PROXIE_API_KEY_Big-Corp_2":      "big-key",
		"PROXIE_UPSTREAM_URL_BIG-CORP_2": "http://vllm.internal:8000/v1",
	}))
	require.NoError(t, err)

	assert.Equal(t, map[string]config.Tenant{
		"acme": {
			Name:        "acme",
			Keys:        []string{"key-1", "key-2"},
			UpstreamURL: mustURL(t, "https://api.example.com"),
		},
		"big-corp_2": {
			Name:        "big-corp_2",
			Keys:        []string{"big-key"},
			UpstreamURL: mustURL(t, "http://vllm.internal:8000/v1"),
		},
	}, cfg.Tenants)
}

func TestLoadDefaultUpstreamOptionalWhenEveryTenantHasOne(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		"PROXIE_UPSTREAM_URL_ACME": "https://api.anthropic.com",
	}, "PROXIE_UPSTREAM_URL"))
	require.NoError(t, err)

	assert.Equal(t, "https://api.anthropic.com", cfg.Tenants["acme"].UpstreamURL.String())
}

func TestLoadRejectsInvalidTenants(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		vars  map[string]string
		unset []string
		want  string
	}{
		"no tenant": {
			unset: []string{"PROXIE_API_KEY_ACME"},
			want:  "PROXIE_API_KEY_<TENANT>: at least one tenant required",
		},
		"empty key list": {
			vars: map[string]string{"PROXIE_API_KEY_ACME": " , "},
			want: "PROXIE_API_KEY_ACME: must hold at least one key",
		},
		"invalid tenant name": {
			vars: map[string]string{"PROXIE_API_KEY_AC.ME": "k"},
			want: "PROXIE_API_KEY_AC.ME: tenant name may only contain",
		},
		"empty tenant name": {
			vars: map[string]string{"PROXIE_API_KEY_": "k"},
			want: "PROXIE_API_KEY_: tenant name may only contain",
		},
		"same tenant in different case": {
			vars: map[string]string{"PROXIE_API_KEY_acme": "other-key"},
			want: "PROXIE_API_KEY_acme: same tenant as PROXIE_API_KEY_ACME",
		},
		"key shared between tenants": {
			vars: map[string]string{"PROXIE_API_KEY_BETA": "acme-key"},
			want: `PROXIE_API_KEY_BETA: a key is also used by tenant "acme"`,
		},
		"upstream for unknown tenant": {
			vars: map[string]string{"PROXIE_UPSTREAM_URL_ACEM": "https://api.example.com"},
			want: `PROXIE_UPSTREAM_URL_ACEM: no PROXIE_API_KEY_<TENANT> variable for tenant "acem"`,
		},
		"tenant without upstream": {
			unset: []string{"PROXIE_UPSTREAM_URL"},
			want:  `PROXIE_UPSTREAM_URL_ACME: required for tenant "acme" because PROXIE_UPSTREAM_URL is not set`,
		},
		"same upstream tenant in different case": {
			vars: map[string]string{
				"PROXIE_UPSTREAM_URL_ACME": "https://a.example.com",
				"PROXIE_UPSTREAM_URL_acme": "https://b.example.com",
			},
			want: "PROXIE_UPSTREAM_URL_acme: same tenant as PROXIE_UPSTREAM_URL_ACME",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(env(tt.vars, tt.unset...))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLoadSharedKeyErrorHidesKey(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{"PROXIE_API_KEY_BETA": "acme-key"}))
	require.Error(t, err)

	assert.NotContains(t, err.Error(), "acme-key")
}

func TestLoadInvalidDefaultUpstreamIsNotReportedAsMissing(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{"PROXIE_UPSTREAM_URL": "ftp://example.com"}))
	require.Error(t, err)

	assert.Contains(t, err.Error(), "PROXIE_UPSTREAM_URL: must be an absolute")
	assert.NotContains(t, err.Error(), "is not set")
}
