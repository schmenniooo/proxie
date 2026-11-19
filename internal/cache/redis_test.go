package cache_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/schmenniooo/proxie/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const shortTimeout = 100 * time.Millisecond

func newRedis(t *testing.T, opts cache.RedisOptions) *cache.Redis {
	t.Helper()
	if opts.Timeout == 0 {
		opts.Timeout = time.Second
	}
	s, err := cache.NewRedis(opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func redisURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestNewRedisValidation(t *testing.T) {
	tests := []struct {
		name string
		opts cache.RedisOptions
	}{
		{"missing URL", cache.RedisOptions{Timeout: time.Second}},
		{"zero timeout", cache.RedisOptions{URL: &url.URL{Scheme: "redis", Host: "localhost:6379"}}},
		{"unsupported scheme", cache.RedisOptions{URL: &url.URL{Scheme: "http", Host: "localhost:6379"}, Timeout: time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cache.NewRedis(tt.opts)
			require.Error(t, err)
		})
	}
}

func TestNewRedisErrorHidesPassword(t *testing.T) {
	_, err := cache.NewRedis(cache.RedisOptions{
		URL:     redisURL(t, "redis://:hunter2@localhost:6379/notadb"),
		Timeout: time.Second,
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "hunter2")
}

func TestNewRedisTLSDoesNotConnect(t *testing.T) {
	// rediss:// is accepted and, like redis://, connects lazily.
	newRedis(t, cache.RedisOptions{URL: redisURL(t, "rediss://user:pw@redis.example.com:6380/0")})
}

func TestRedisTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	s := newRedis(t, cache.RedisOptions{URL: &url.URL{Scheme: "redis", Host: mr.Addr()}})
	ctx := t.Context()

	require.NoError(t, s.Set(ctx, "k", []byte("v"), time.Minute))
	assert.Equal(t, time.Minute, mr.TTL("k"))

	mr.FastForward(time.Minute + time.Second)
	_, found, err := s.Get(ctx, "k")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestRedisSetRejectsNonPositiveTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	s := newRedis(t, cache.RedisOptions{URL: &url.URL{Scheme: "redis", Host: mr.Addr()}})

	// Without a TTL the entry would never expire.
	require.Error(t, s.Set(t.Context(), "k", []byte("v"), 0))
	assert.False(t, mr.Exists("k"))
}

func TestRedisPassword(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.RequireAuth("secret")
	ctx := t.Context()

	t.Run("password option", func(t *testing.T) {
		s := newRedis(t, cache.RedisOptions{URL: &url.URL{Scheme: "redis", Host: mr.Addr()}, Password: "secret"})
		require.NoError(t, s.Ping(ctx))
	})

	t.Run("password in URL", func(t *testing.T) {
		s := newRedis(t, cache.RedisOptions{URL: redisURL(t, "redis://:secret@"+mr.Addr())})
		require.NoError(t, s.Ping(ctx))
	})

	t.Run("option overrides URL", func(t *testing.T) {
		s := newRedis(t, cache.RedisOptions{URL: redisURL(t, "redis://:wrong@"+mr.Addr()), Password: "secret"})
		require.NoError(t, s.Ping(ctx))
	})

	t.Run("wrong password", func(t *testing.T) {
		s := newRedis(t, cache.RedisOptions{URL: &url.URL{Scheme: "redis", Host: mr.Addr()}, Password: "wrong"})
		require.Error(t, s.Ping(ctx))
	})
}

// silentListener accepts connections and never answers, like a hung Redis.
func silentListener(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		var conns []net.Conn
		for {
			c, err := ln.Accept()
			if err != nil { // listener closed by cleanup
				for _, c := range conns {
					_ = c.Close()
				}
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// closedAddr returns an address nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func TestRedisFailsFast(t *testing.T) {
	tests := []struct {
		name string
		addr func(*testing.T) string
	}{
		{"hung server", silentListener},
		{"connection refused", closedAddr},
	}
	ops := map[string]func(*testing.T, cache.Cache) error{
		"get": func(t *testing.T, s cache.Cache) error {
			_, found, err := s.Get(t.Context(), "k")
			assert.False(t, found)
			return err
		},
		"set": func(t *testing.T, s cache.Cache) error {
			return s.Set(t.Context(), "k", []byte("v"), time.Minute)
		},
		"ping": func(t *testing.T, s cache.Cache) error {
			return s.Ping(t.Context())
		},
	}
	for _, tt := range tests {
		for op, call := range ops {
			t.Run(tt.name+"/"+op, func(t *testing.T) {
				s := newRedis(t, cache.RedisOptions{
					URL:     &url.URL{Scheme: "redis", Host: tt.addr(t)},
					Timeout: shortTimeout,
				})
				start := time.Now()
				err := call(t, s)
				require.Error(t, err)
				// Generous bound for slow CI; without the timeout this
				// would hang far longer.
				assert.Less(t, time.Since(start), 10*shortTimeout)
			})
		}
	}
}

func TestRedisCallerContext(t *testing.T) {
	mr := miniredis.RunT(t)
	s := newRedis(t, cache.RedisOptions{URL: &url.URL{Scheme: "redis", Host: mr.Addr()}})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := s.Get(ctx, "k")
	require.ErrorIs(t, err, context.Canceled)
}

// TestRedisReal runs against a real Redis when PROXIE_TEST_REDIS_URL is set
// (task test sets it to the compose Redis) to catch differences from
// miniredis.
func TestRedisReal(t *testing.T) {
	raw := os.Getenv("PROXIE_TEST_REDIS_URL")
	if raw == "" {
		t.Skip("PROXIE_TEST_REDIS_URL not set")
	}
	s := newRedis(t, cache.RedisOptions{URL: redisURL(t, raw)})
	ctx := t.Context()
	require.NoError(t, s.Ping(ctx))

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	key := "proxie_test_" + hex.EncodeToString(b)

	_, found, err := s.Get(ctx, key)
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, s.Set(ctx, key, []byte("v"), 10*time.Second))
	got, found, err := s.Get(ctx, key)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("v"), got)

	// compose.yaml runs Redis with the eviction policy proxie needs.
	policy, err := s.MaxMemoryPolicy(ctx)
	require.NoError(t, err)
	assert.Equal(t, "allkeys-lru", policy)
}

func TestRedisMaxMemoryPolicyUnsupported(t *testing.T) {
	// miniredis has no CONFIG command, like managed Redis services that
	// block it.
	mr := miniredis.RunT(t)
	s := newRedis(t, cache.RedisOptions{URL: redisURL(t, "redis://"+mr.Addr())})

	_, err := s.MaxMemoryPolicy(t.Context())
	require.Error(t, err)
}
