package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/schmenniooo/proxie/internal/config"
)

// maxIdleConnsPerHost is the pool of kept-alive connections per upstream.
// Go's default of 2 is far too small for a proxy talking to few hosts.
const maxIdleConnsPerHost = 100

// hopHeaders apply to a single connection and are never forwarded, in
// either direction (RFC 9110, section 7.6.1).
var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Connection",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// ErrInvalidPath means the request path contains a ".." segment, which
// could step out of the upstream base path. Such requests are not sent.
var ErrInvalidPath = errors.New("request path must not contain .. segments")

// Options configures a Forwarder.
type Options struct {
	// Timeout bounds the whole upstream exchange, including reading the
	// response body.
	Timeout time.Duration
}

// Forwarder sends requests to a tenant's upstream.
type Forwarder struct {
	client  *http.Client
	timeout time.Duration
}

// New returns a Forwarder with its own connection pool.
func New(opts Options) *Forwarder {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Ask for and pass on the bytes exactly as the upstream sends them.
	transport.DisableCompression = true
	transport.MaxIdleConnsPerHost = maxIdleConnsPerHost
	return &Forwarder{
		client: &http.Client{
			Transport: transport,
			// Redirects are passed back to the caller, not followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		timeout: opts.Timeout,
	}
}

// Forward sends r to the tenant's upstream and returns its response. The
// timeout covers the whole exchange: it ends when the returned body is
// closed, which the caller must do. Cancelling ctx cancels the upstream
// call.
//
// Returned errors never contain the upstream URL, which may carry
// credentials in its query.
func (f *Forwarder) Forward(ctx context.Context, tenant config.Tenant, r *http.Request) (*http.Response, error) {
	if tenant.UpstreamURL == nil {
		return nil, fmt.Errorf("tenant %q has no upstream URL", tenant.Name)
	}
	// Checked on the decoded path, so %2e%2e and %2F are caught as well.
	if slices.Contains(strings.Split(r.URL.Path, "/"), "..") {
		return nil, ErrInvalidPath
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)

	out, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL(tenant.UpstreamURL, r.URL).String(), r.Body)
	if err != nil {
		cancel()
		return nil, hideURL(err)
	}
	out.ContentLength = r.ContentLength
	out.Header = r.Header.Clone()
	RemoveHopHeaders(out.Header)
	out.Header.Set("Accept-Encoding", "identity")

	resp, err := f.client.Do(out)
	if err != nil {
		cancel()
		return nil, hideURL(err)
	}
	resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// upstreamURL joins the upstream base URL with the path and query of the
// request. The caller's percent-encoding is kept as sent.
func upstreamURL(base, req *url.URL) *url.URL {
	u := *base
	u.Path = strings.TrimSuffix(base.Path, "/") + req.Path
	u.RawPath = strings.TrimSuffix(base.EscapedPath(), "/") + req.EscapedPath()
	var query []string
	for _, q := range []string{base.RawQuery, req.RawQuery} {
		if q != "" {
			query = append(query, q)
		}
	}
	u.RawQuery = strings.Join(query, "&")
	u.Fragment, u.RawFragment = "", ""
	return &u
}

// RemoveHopHeaders deletes the hop-by-hop headers from h, including the
// ones named in its Connection header.
func RemoveHopHeaders(h http.Header) {
	for _, v := range h.Values("Connection") {
		for name := range strings.SplitSeq(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				h.Del(name)
			}
		}
	}
	for _, name := range hopHeaders {
		h.Del(name)
	}
}

// hideURL strips the *url.Error wrapper, whose message holds the full URL.
func hideURL(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("upstream %s: %w", strings.ToLower(ue.Op), ue.Err)
	}
	return err
}

// cancelOnClose releases the timeout context once the body is closed.
type cancelOnClose struct {
	io.ReadCloser

	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
