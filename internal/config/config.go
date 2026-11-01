// Package config loads the proxie configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DefaultKeyHeaders is the header allow-list used for the cache key when
// PROXIE_KEY_HEADERS is unset.
var DefaultKeyHeaders = []string{"content-type", "anthropic-version", "anthropic-beta"}

// Config is the complete proxie configuration.
type Config struct {
	UpstreamURL   *url.URL
	RedisURL      *url.URL
	RedisPassword string

	CacheTTL     time.Duration
	IgnoreFields []string
	KeyHeaders   []string

	MaxRequestBytes int64
	MaxEntryBytes   int64

	Addr      string
	AdminAddr string

	UpstreamTimeout        time.Duration
	StreamFirstByteTimeout time.Duration
	StreamIdleTimeout      time.Duration
	RedisTimeout           time.Duration
	ShutdownTimeout        time.Duration

	Log Log
}

// Log configures logging.
type Log struct {
	Level  slog.Level
	Format string // "json" or "text"

	// File is an optional log file path. Logs always go to stdout as well.
	File           string
	FileMaxMB      int
	FileMaxBackups int
	FileMaxAgeDays int
}

// LogValue renders the config for logging with secrets masked.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("upstream_url", c.UpstreamURL.String()),
		slog.String("redis_url", c.RedisURL.Redacted()),
		slog.Bool("redis_password_set", c.RedisPassword != ""),
		slog.String("cache_ttl", c.CacheTTL.String()),
		slog.Any("ignore_fields", c.IgnoreFields),
		slog.Any("key_headers", c.KeyHeaders),
		slog.Int64("max_request_bytes", c.MaxRequestBytes),
		slog.Int64("max_entry_bytes", c.MaxEntryBytes),
		slog.String("addr", c.Addr),
		slog.String("admin_addr", c.AdminAddr),
		slog.String("upstream_timeout", c.UpstreamTimeout.String()),
		slog.String("stream_first_byte_timeout", c.StreamFirstByteTimeout.String()),
		slog.String("stream_idle_timeout", c.StreamIdleTimeout.String()),
		slog.String("redis_timeout", c.RedisTimeout.String()),
		slog.String("shutdown_timeout", c.ShutdownTimeout.String()),
		slog.String("log_level", c.Log.Level.String()),
		slog.String("log_format", c.Log.Format),
		slog.String("log_file", c.Log.File),
	)
}

// Load reads the configuration through lookup, which is normally
// os.LookupEnv. All invalid or missing variables are reported together.
func Load(lookup func(string) (string, bool)) (Config, error) {
	l := &loader{lookup: lookup}
	cfg := Config{
		UpstreamURL:   l.url("PROXIE_UPSTREAM_URL", "http", "https"),
		RedisURL:      l.url("PROXIE_REDIS_URL", "redis", "rediss"),
		RedisPassword: l.string("PROXIE_REDIS_PASSWORD", ""),

		CacheTTL:     l.duration("PROXIE_CACHE_TTL", time.Hour),
		IgnoreFields: l.list("PROXIE_IGNORE_FIELDS", nil, false),
		KeyHeaders:   l.list("PROXIE_KEY_HEADERS", DefaultKeyHeaders, true),

		MaxRequestBytes: int64(l.int("PROXIE_MAX_REQUEST_BYTES", 10<<20, 1)),
		MaxEntryBytes:   int64(l.int("PROXIE_MAX_ENTRY_BYTES", 10<<20, 1)),

		Addr:      l.addr("PROXIE_ADDR", ":8080"),
		AdminAddr: l.addr("PROXIE_ADMIN_ADDR", ":9090"),

		UpstreamTimeout:        l.duration("PROXIE_UPSTREAM_TIMEOUT", 120*time.Second),
		StreamFirstByteTimeout: l.duration("PROXIE_STREAM_FIRST_BYTE_TIMEOUT", 30*time.Second),
		StreamIdleTimeout:      l.duration("PROXIE_STREAM_IDLE_TIMEOUT", 30*time.Second),
		RedisTimeout:           l.duration("PROXIE_REDIS_TIMEOUT", 200*time.Millisecond),
		ShutdownTimeout:        l.duration("PROXIE_SHUTDOWN_TIMEOUT", 25*time.Second),

		Log: Log{
			Level:          l.level("PROXIE_LOG_LEVEL", slog.LevelInfo),
			Format:         l.oneOf("PROXIE_LOG_FORMAT", "json", "json", "text"),
			File:           l.string("PROXIE_LOG_FILE", ""),
			FileMaxMB:      l.int("PROXIE_LOG_FILE_MAX_MB", 100, 1),
			FileMaxBackups: l.int("PROXIE_LOG_FILE_MAX_BACKUPS", 5, 0),
			FileMaxAgeDays: l.int("PROXIE_LOG_FILE_MAX_AGE_DAYS", 14, 0),
		},
	}
	if err := errors.Join(l.errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// loader parses variables and collects every error instead of stopping at
// the first one.
type loader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (l *loader) fail(name, format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf("%s: %s", name, fmt.Sprintf(format, args...)))
}

// value returns the trimmed value and whether it is set and not empty.
func (l *loader) value(name string) (string, bool) {
	v, ok := l.lookup(name)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (l *loader) string(name, def string) string {
	if v, ok := l.value(name); ok {
		return v
	}
	return def
}

// url parses a required absolute URL with one of the given schemes.
func (l *loader) url(name string, schemes ...string) *url.URL {
	v, ok := l.value(name)
	if !ok {
		l.fail(name, "required")
		return nil
	}
	u, err := url.Parse(v)
	if err != nil {
		// The parse error repeats the URL, which may hold a password.
		l.fail(name, "not a valid URL")
		return nil
	}
	if !slices.Contains(schemes, u.Scheme) || u.Host == "" {
		l.fail(name, "must be an absolute %s URL", strings.Join(schemes, " or "))
		return nil
	}
	return u
}

func (l *loader) duration(name string, def time.Duration) time.Duration {
	v, ok := l.value(name)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		l.fail(name, "must be a positive duration like 30s, got %q", v)
		return def
	}
	return d
}

func (l *loader) int(name string, def, minimum int) int {
	v, ok := l.value(name)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < minimum {
		l.fail(name, "must be an integer >= %d, got %q", minimum, v)
		return def
	}
	return n
}

func (l *loader) addr(name, def string) string {
	v := l.string(name, def)
	if _, _, err := net.SplitHostPort(v); err != nil {
		l.fail(name, "must be host:port like :8080, got %q", v)
	}
	return v
}

func (l *loader) level(name string, def slog.Level) slog.Level {
	v, ok := l.value(name)
	if !ok {
		return def
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(v)); err != nil {
		l.fail(name, "must be debug, info, warn or error, got %q", v)
		return def
	}
	return lvl
}

func (l *loader) oneOf(name, def string, allowed ...string) string {
	v := strings.ToLower(l.string(name, def))
	if !slices.Contains(allowed, v) {
		l.fail(name, "must be one of %s, got %q", strings.Join(allowed, ", "), v)
		return def
	}
	return v
}

// list parses a comma-separated list. Unset means def; set but empty means an
// empty list.
func (l *loader) list(name string, def []string, lower bool) []string {
	v, ok := l.lookup(name)
	if !ok {
		return slices.Clone(def)
	}
	items := []string{}
	for item := range strings.SplitSeq(v, ",") {
		item = strings.TrimSpace(item)
		if lower {
			item = strings.ToLower(item)
		}
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}
