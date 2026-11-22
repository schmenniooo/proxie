// Package doctor runs health checks on the proxie config and its
// dependencies, using the same code paths as the server.
package doctor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/schmenniooo/proxie/internal/cache"
	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/logging"
)

// upstreamTimeout bounds each upstream check (DNS, TLS and HTTP together).
const upstreamTimeout = 10 * time.Second

// wantPolicy is the Redis eviction policy proxie needs (see docs/design.md).
const wantPolicy = "allkeys-lru"

// Outcomes of a check, in Result.Status.
const (
	Pass = "pass"
	Warn = "warn"
	Fail = "fail"
)

// Result is the outcome of one check. Detail never holds secrets, prompts,
// bodies or keys.
type Result struct {
	Check  string `json:"check"`
	Status string `json:"status"` // Pass, Warn or Fail
	Detail string `json:"detail,omitempty"`
}

// Run runs every check. loadErr is the error from completing cfg; when it is
// set, only the failed config check is returned, because the other checks
// need a valid config.
func Run(ctx context.Context, cfg config.Config, loadErr error) []Result {
	if loadErr != nil {
		return []Result{{Check: "config", Status: Fail, Detail: loadErr.Error()}}
	}
	results := []Result{{
		Check:  "config",
		Status: Pass,
		Detail: fmt.Sprintf("%d tenant(s)", len(cfg.Tenants)),
	}}
	if cfg.Log.File != "" {
		results = append(results, checkLogFile(cfg.Log))
	}
	results = append(results, checkRedis(ctx, cfg)...)
	for _, u := range upstreams(cfg.Tenants) {
		results = append(results, checkUpstream(ctx, u))
	}
	return results
}

// Failed reports whether any check failed.
func Failed(results []Result) bool {
	return slices.ContainsFunc(results, func(r Result) bool { return r.Status == Fail })
}

// WriteTable writes the results as a human-readable table.
func WriteTable(w io.Writer, results []Result) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "STATUS\tCHECK\tDETAIL")
	for _, r := range results {
		// Multi-line details (several config errors) get one row per line.
		for i, line := range strings.Split(r.Detail, "\n") {
			if i == 0 {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Status, r.Check, line)
			} else {
				_, _ = fmt.Fprintf(tw, "\t\t%s\n", line)
			}
		}
	}
	return tw.Flush()
}

// WriteJSON writes the results as a JSON array.
func WriteJSON(w io.Writer, results []Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

// checkLogFile opens the log file the way the server does.
func checkLogFile(cfg config.Log) Result {
	r := Result{Check: "log file", Status: Pass, Detail: cfg.File + " is writable"}
	_, closer, err := logging.New(cfg, io.Discard)
	if err != nil {
		r.Status, r.Detail = Fail, err.Error()
		return r
	}
	_ = closer.Close()
	return r
}

// checkRedis pings Redis and checks its eviction policy.
func checkRedis(ctx context.Context, cfg config.Config) []Result {
	ping := Result{Check: "redis", Status: Pass}
	c, err := cache.NewRedis(cache.RedisOptions{
		URL:      cfg.RedisURL.URL,
		Password: cfg.RedisPassword,
		Timeout:  cfg.RedisTimeout,
	})
	if err != nil {
		ping.Status, ping.Detail = Fail, err.Error()
		return []Result{ping}
	}
	defer func() { _ = c.Close() }()

	start := time.Now()
	if err := c.Ping(ctx); err != nil {
		ping.Status, ping.Detail = Fail, fmt.Sprintf("%s: %v", config.RedactURL(cfg.RedisURL.URL), err)
		return []Result{ping}
	}
	ping.Detail = fmt.Sprintf("%s PING in %s", config.RedactURL(cfg.RedisURL.URL), roundDuration(time.Since(start)))

	policy := Result{Check: "redis maxmemory-policy", Status: Pass, Detail: wantPolicy}
	got, err := c.MaxMemoryPolicy(ctx)
	switch {
	case err != nil:
		policy.Status = Warn
		policy.Detail = fmt.Sprintf("cannot read it (managed Redis may block CONFIG), make sure it is %s: %v", wantPolicy, err)
	case got != wantPolicy:
		policy.Status = Warn
		policy.Detail = fmt.Sprintf("is %s, want %s so Redis evicts old entries when full", got, wantPolicy)
	}
	return []Result{ping, policy}
}

// upstreams returns the distinct upstream URLs of the tenants.
func upstreams(tenants map[string]config.Tenant) []*url.URL {
	seen := map[string]*url.URL{}
	for _, t := range tenants {
		if t.UpstreamURL != nil {
			seen[t.UpstreamURL.String()] = t.UpstreamURL
		}
	}
	urls := []*url.URL{}
	for _, k := range slices.Sorted(maps.Keys(seen)) {
		urls = append(urls, seen[k])
	}
	return urls
}

// checkUpstream resolves the upstream host, does a TLS handshake for https
// and sends an unauthenticated GET to the base URL. proxie holds no provider
// keys (callers send their own), so the check proves the upstream is
// reachable, not that a key works. Any HTTP response counts as a pass;
// 401 and 403 are the expected answer and are reported as such.
func checkUpstream(ctx context.Context, u *url.URL) Result {
	r := Result{Check: "upstream " + config.RedactURL(u), Status: Pass}
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()

	host := u.Hostname()
	if _, err := net.DefaultResolver.LookupHost(ctx, host); err != nil {
		r.Status, r.Detail = Fail, "DNS: "+err.Error()
		return r
	}

	if u.Scheme == "https" {
		port := u.Port()
		if port == "" {
			port = "443"
		}
		d := &tls.Dialer{Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}
		conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
		if err != nil {
			r.Status, r.Detail = Fail, "TLS: "+err.Error()
			return r
		}
		_ = conn.Close()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		r.Status, r.Detail = Fail, "HTTP: invalid request"
		return r
	}
	client := &http.Client{
		// The status of the base URL itself is what matters.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		// A *url.Error repeats the URL, which may hold a key in its query.
		if ue := (*url.Error)(nil); errors.As(err, &ue) {
			err = ue.Err
		}
		r.Status, r.Detail = Fail, "HTTP: "+err.Error()
		return r
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	r.Detail = fmt.Sprintf("HTTP %d in %s", resp.StatusCode, roundDuration(time.Since(start)))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		r.Detail = "reachable, auth required (no provider key sent): " + r.Detail
	}
	return r
}

func roundDuration(d time.Duration) time.Duration {
	if d < time.Millisecond {
		return d.Round(time.Microsecond)
	}
	return d.Round(100 * time.Microsecond)
}
