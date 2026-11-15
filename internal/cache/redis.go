package cache

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisOptions configures a Redis cache.
type RedisOptions struct {
	// URL is a redis:// or rediss:// (TLS) URL.
	URL *url.URL
	// Password overrides a password in URL when not empty.
	Password string
	// Timeout bounds every operation, including dialing.
	Timeout time.Duration
}

// Redis is a Cache backed by Redis. Eviction is left to Redis, which should
// run with maxmemory-policy allkeys-lru.
type Redis struct {
	client  *redis.Client
	timeout time.Duration
}

var _ Cache = (*Redis)(nil)

// NewRedis creates a Redis cache. It does not connect; connections are
// opened lazily, so an unreachable Redis does not stop proxie from starting.
func NewRedis(opts RedisOptions) (*Redis, error) {
	if opts.URL == nil {
		return nil, errors.New("redis: URL is required")
	}
	if opts.Timeout <= 0 {
		return nil, errors.New("redis: timeout must be positive")
	}
	o, err := redis.ParseURL(opts.URL.String())
	if err != nil {
		// The parse error can repeat the URL, which may hold a password.
		return nil, errors.New("redis: invalid URL")
	}
	if opts.Password != "" {
		o.Password = opts.Password
	}
	o.DialTimeout = opts.Timeout
	o.ReadTimeout = opts.Timeout
	o.WriteTimeout = opts.Timeout
	o.ContextTimeoutEnabled = true
	// A retry would exceed the timeout budget; fail open instead.
	o.MaxRetries = -1
	return &Redis{client: redis.NewClient(o), timeout: opts.Timeout}, nil
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	val, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis get: %w", err)
	}
	return val, true, nil
}

func (r *Redis) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("redis set: ttl must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := r.client.Set(ctx, key, val, ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

func (r *Redis) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	return nil
}

func (r *Redis) Close() error {
	return r.client.Close()
}
