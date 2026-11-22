// Package config holds the proxie configuration.
//
// Config doubles as the Kong flag set of the CLI: every setting is a flag
// that falls back to its PROXIE_* environment variable and then to its
// default (see the struct tags). Kong only parses the values; Complete then
// loads the tenants and validates everything, reporting all problems at once.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/alecthomas/kong"
)

// DefaultKeyHeaders is the header allow-list used for the cache key when
// PROXIE_KEY_HEADERS is unset. It must match the default tag of KeyHeaders.
var DefaultKeyHeaders = []string{"content-type", "anthropic-version", "anthropic-beta"}

const (
	apiKeyPrefix      = "PROXIE_API_KEY_"
	upstreamURLPrefix = "PROXIE_UPSTREAM_URL_"
)

// tenantName is the allowed form of a tenant suffix in env var names.
var tenantName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Config is the complete proxie configuration.
//
// Tenants are not flags: their env var names carry the tenant name, and keys
// on a command line would be visible in the process list.
type Config struct {
	UpstreamURL   URL    `name:"upstream-url" env:"PROXIE_UPSTREAM_URL" placeholder:"URL" help:"Default upstream base URL (for example https://api.anthropic.com) for tenants without their own PROXIE_UPSTREAM_URL_<TENANT>. The caller's path after /v1/upstream is appended to it."`
	RedisURL      URL    `name:"redis-url" env:"PROXIE_REDIS_URL" placeholder:"URL" help:"Redis URL (rediss:// for TLS). Required."`
	RedisPassword string `name:"redis-password" env:"PROXIE_REDIS_PASSWORD" placeholder:"PASSWORD" help:"Redis password. Prefer the env var, flags show up in the process list."`

	CacheTTL     time.Duration `name:"cache-ttl" env:"PROXIE_CACHE_TTL" default:"1h" help:"Entry lifetime."`
	IgnoreFields []string      `name:"ignore-fields" env:"PROXIE_IGNORE_FIELDS" placeholder:"FIELD" help:"Body fields excluded from the cache key."`
	KeyHeaders   []string      `name:"key-headers" env:"PROXIE_KEY_HEADERS" default:"content-type,anthropic-version,anthropic-beta" placeholder:"HEADER" help:"Header allow-list for the cache key. Set but empty means no headers."`

	MaxRequestBytes int64 `name:"max-request-bytes" env:"PROXIE_MAX_REQUEST_BYTES" default:"10485760" help:"Max request body."`
	MaxEntryBytes   int64 `name:"max-entry-bytes" env:"PROXIE_MAX_ENTRY_BYTES" default:"10485760" help:"Max stored response."`

	Addr      string `name:"addr" env:"PROXIE_ADDR" default:":8080" placeholder:"HOST:PORT" help:"Proxy listen address."`
	AdminAddr string `name:"admin-addr" env:"PROXIE_ADMIN_ADDR" default:":9090" placeholder:"HOST:PORT" help:"Admin listen address (/healthz, /metrics)."`

	UpstreamTimeout        time.Duration `name:"upstream-timeout" env:"PROXIE_UPSTREAM_TIMEOUT" default:"120s" help:"Upstream, non-streaming, total."`
	StreamFirstByteTimeout time.Duration `name:"stream-first-byte-timeout" env:"PROXIE_STREAM_FIRST_BYTE_TIMEOUT" default:"30s" help:"Upstream, streaming, time to first piece."`
	StreamIdleTimeout      time.Duration `name:"stream-idle-timeout" env:"PROXIE_STREAM_IDLE_TIMEOUT" default:"30s" help:"Upstream, streaming, max silence between pieces."`
	RedisTimeout           time.Duration `name:"redis-timeout" env:"PROXIE_REDIS_TIMEOUT" default:"200ms" help:"Redis operation."`
	ShutdownTimeout        time.Duration `name:"shutdown-timeout" env:"PROXIE_SHUTDOWN_TIMEOUT" default:"25s" help:"Time in-flight requests get to finish on SIGINT/SIGTERM."`

	Log Log `embed:"" prefix:"log-" group:"Logging"`

	// Tenants maps the lowercased tenant name to its settings. Complete
	// fills it from the PROXIE_API_KEY_<TENANT> variables.
	Tenants map[string]Tenant `kong:"-"`
}

// Log configures logging.
type Log struct {
	Level  slog.Level `name:"level" env:"PROXIE_LOG_LEVEL" default:"info" help:"debug, info, warn or error."`
	Format string     `name:"format" env:"PROXIE_LOG_FORMAT" default:"json" help:"json or text."`

	// File is an optional log file path. Logs always go to stdout as well.
	File           string `name:"file" env:"PROXIE_LOG_FILE" placeholder:"PATH" help:"Optional log file, written in addition to stdout."`
	FileMaxMB      int    `name:"file-max-mb" env:"PROXIE_LOG_FILE_MAX_MB" default:"100" help:"Rotate the log file at this size."`
	FileMaxBackups int    `name:"file-max-backups" env:"PROXIE_LOG_FILE_MAX_BACKUPS" default:"5" help:"Rotated log files to keep (0 keeps all)."`
	FileMaxAgeDays int    `name:"file-max-age-days" env:"PROXIE_LOG_FILE_MAX_AGE_DAYS" default:"14" help:"Delete rotated log files older than this (0 keeps them)."`
}

// Tenant is one caller of proxie, identified by its proxie keys.
type Tenant struct {
	Name string
	// Keys are the accepted proxie keys. Several allow key rotation.
	Keys []string
	// UpstreamURL is the base URL the tenant's requests are forwarded to:
	// its own PROXIE_UPSTREAM_URL_<TENANT>, or the default upstream URL. The
	// caller's path after /v1/upstream is appended to it.
	UpstreamURL *url.URL
}

// URL is a URL setting. Decoding never fails, so Kong never repeats the value
// (which may hold a password) in an error message; Complete reports invalid
// URLs instead. A nil URL means unset.
type URL struct {
	*url.URL

	invalid bool
}

// Decode implements kong.MapperValue.
func (u *URL) Decode(ctx *kong.DecodeContext) error {
	var raw string
	if err := ctx.Scan.PopValueInto("url", &raw); err != nil {
		return err
	}
	*u = parseURL(raw)
	return nil
}

func parseURL(raw string) URL {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return URL{}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return URL{invalid: true}
	}
	return URL{URL: u}
}

// LogValue renders the tenant for logging without its keys.
func (t Tenant) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("keys", len(t.Keys)),
		slog.String("upstream_url", redactURL(t.UpstreamURL)),
	)
}

// LogValue renders the config for logging with secrets masked.
func (c Config) LogValue() slog.Value {
	tenants := make([]slog.Attr, 0, len(c.Tenants))
	for _, name := range slices.Sorted(maps.Keys(c.Tenants)) {
		tenants = append(tenants, slog.Any(name, c.Tenants[name]))
	}
	return slog.GroupValue(
		slog.Attr{Key: "tenants", Value: slog.GroupValue(tenants...)},
		slog.String("redis_url", redactURL(c.RedisURL.URL)),
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

// RedactURL masks the password and drops the query and fragment, which can
// carry credentials (for example ?key=). It returns "" for nil.
func RedactURL(u *url.URL) string { return redactURL(u) }

func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	safe := *u
	safe.RawQuery = ""
	safe.Fragment = ""
	return safe.Redacted()
}

// Complete loads the tenants from environ, which is normally os.Environ()
// (KEY=value entries), normalizes the lists and validates every setting. All
// invalid or missing settings are reported together.
func (c *Config) Complete(environ []string) error {
	vars := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	v := &validator{vars: vars}

	c.Tenants = v.tenants(c.UpstreamURL)
	v.requiredURL("RedisURL", c.RedisURL, "redis", "rediss")

	c.IgnoreFields = cleanList(c.IgnoreFields, false)
	c.KeyHeaders = cleanList(c.KeyHeaders, true)

	for field, d := range map[string]time.Duration{
		"CacheTTL":               c.CacheTTL,
		"UpstreamTimeout":        c.UpstreamTimeout,
		"StreamFirstByteTimeout": c.StreamFirstByteTimeout,
		"StreamIdleTimeout":      c.StreamIdleTimeout,
		"RedisTimeout":           c.RedisTimeout,
		"ShutdownTimeout":        c.ShutdownTimeout,
	} {
		if d <= 0 {
			v.fail(label(field), "must be a positive duration like 30s, got %s", d)
		}
	}
	v.minimum("MaxRequestBytes", c.MaxRequestBytes, 1)
	v.minimum("MaxEntryBytes", c.MaxEntryBytes, 1)
	v.addr("Addr", c.Addr)
	v.addr("AdminAddr", c.AdminAddr)

	c.Log.Format = strings.ToLower(strings.TrimSpace(c.Log.Format))
	if c.Log.Format != "json" && c.Log.Format != "text" {
		v.fail(label("Log.Format"), "must be json or text, got %q", c.Log.Format)
	}
	c.Log.File = strings.TrimSpace(c.Log.File)
	v.minimum("Log.FileMaxMB", int64(c.Log.FileMaxMB), 1)
	v.minimum("Log.FileMaxBackups", int64(c.Log.FileMaxBackups), 0)
	v.minimum("Log.FileMaxAgeDays", int64(c.Log.FileMaxAgeDays), 0)

	// Sorted, so the message does not depend on map iteration order.
	slices.SortFunc(v.errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return errors.Join(v.errs...)
}

// label names a setting in error messages by its flag and env var, for
// example "--cache-ttl ($PROXIE_CACHE_TTL)". field is a Config field name,
// with "Log." for the nested logging fields.
func label(field string) string {
	t, prefix := reflect.TypeFor[Config](), ""
	if name, ok := strings.CutPrefix(field, "Log."); ok {
		t, prefix, field = reflect.TypeFor[Log](), "log-", name
	}
	f, ok := t.FieldByName(field)
	if !ok {
		panic("config: no field " + field)
	}
	return fmt.Sprintf("--%s%s ($%s)", prefix, f.Tag.Get("name"), f.Tag.Get("env"))
}

// validator collects every error instead of stopping at the first one.
type validator struct {
	vars map[string]string
	errs []error
}

func (v *validator) fail(name, format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", name, fmt.Sprintf(format, args...)))
}

func (v *validator) minimum(field string, n, minimum int64) {
	if n < minimum {
		v.fail(label(field), "must be an integer >= %d, got %d", minimum, n)
	}
}

func (v *validator) addr(field, addr string) {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		v.fail(label(field), "must be host:port like :8080, got %q", addr)
	}
}

// requiredURL checks that the URL setting field is set and valid.
func (v *validator) requiredURL(field string, u URL, schemes ...string) {
	if u.URL == nil && !u.invalid {
		v.fail(label(field), "required")
		return
	}
	v.checkURL(label(field), u, schemes...)
}

// checkURL checks a set URL. Messages never repeat the URL, which may hold
// a password.
func (v *validator) checkURL(name string, u URL, schemes ...string) bool {
	if u.invalid {
		v.fail(name, "not a valid URL")
		return false
	}
	if !slices.Contains(schemes, u.Scheme) || u.Host == "" {
		v.fail(name, "must be an absolute %s URL", strings.Join(schemes, " or "))
		return false
	}
	return true
}

// tenants builds the tenants from the PROXIE_API_KEY_<TENANT> and
// PROXIE_UPSTREAM_URL_<TENANT> variables, with def as the default upstream.
func (v *validator) tenants(def URL) map[string]Tenant {
	tenants := map[string]Tenant{}
	keyVars := map[string]string{} // tenant -> its PROXIE_API_KEY_ variable
	owners := map[string]string{}  // key -> tenant, to reject shared keys
	urlVars := map[string]string{} // tenant -> its PROXIE_UPSTREAM_URL_ variable

	for _, name := range slices.Sorted(maps.Keys(v.vars)) {
		if suffix, ok := strings.CutPrefix(name, upstreamURLPrefix); ok {
			if tenant, ok := v.tenantSuffix(name, suffix); ok {
				if prev, dup := urlVars[tenant]; dup {
					v.fail(name, "same tenant as %s", prev)
					continue
				}
				urlVars[tenant] = name
			}
			continue
		}
		suffix, ok := strings.CutPrefix(name, apiKeyPrefix)
		if !ok {
			continue
		}
		tenant, ok := v.tenantSuffix(name, suffix)
		if !ok {
			continue
		}
		if prev, dup := keyVars[tenant]; dup {
			v.fail(name, "same tenant as %s", prev)
			continue
		}
		keyVars[tenant] = name

		keys := cleanList(strings.Split(v.vars[name], ","), false)
		if len(keys) == 0 {
			v.fail(name, "must hold at least one key")
			continue
		}
		for _, key := range keys {
			if owner, dup := owners[key]; dup {
				// Never repeat the key itself.
				v.fail(name, "a key is also used by tenant %q", owner)
				continue
			}
			owners[key] = tenant
		}
		tenants[tenant] = Tenant{Name: tenant, Keys: keys}
	}

	if len(keyVars) == 0 {
		v.fail(apiKeyPrefix+"<TENANT>", "at least one tenant required")
	}

	defName := label("UpstreamURL")
	defValid := def.URL != nil && v.checkURL(defName, def, "http", "https")
	for _, tenant := range slices.Sorted(maps.Keys(urlVars)) {
		if _, ok := keyVars[tenant]; !ok {
			v.fail(urlVars[tenant], "no %s<TENANT> variable for tenant %q", apiKeyPrefix, tenant)
		}
	}
	for _, tenant := range slices.Sorted(maps.Keys(tenants)) {
		t := tenants[tenant]
		switch name, ok := urlVars[tenant]; {
		case ok:
			u := parseURL(v.vars[name])
			if u.URL == nil && !u.invalid {
				v.fail(name, "must not be empty")
			} else if v.checkURL(name, u, "http", "https") {
				t.UpstreamURL = u.URL
			}
		case defValid:
			t.UpstreamURL = def.URL
		case def.URL == nil && !def.invalid:
			v.fail(upstreamURLPrefix+strings.ToUpper(tenant),
				"required for tenant %q because %s is not set", tenant, defName)
		}
		tenants[tenant] = t
	}
	return tenants
}

// tenantSuffix validates the tenant part of an env var name and returns it
// lowercased.
func (v *validator) tenantSuffix(name, suffix string) (string, bool) {
	if !tenantName.MatchString(suffix) {
		v.fail(name, "tenant name may only contain letters, digits, - and _")
		return "", false
	}
	return strings.ToLower(suffix), true
}

// cleanList trims the items, optionally lowercases them and drops empty
// ones. It never returns nil.
func cleanList(items []string, lower bool) []string {
	out := []string{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if lower {
			item = strings.ToLower(item)
		}
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
