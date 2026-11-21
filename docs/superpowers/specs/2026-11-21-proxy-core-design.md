# Proxy core design (milestone 5, SMI-11)

## Intent

Forward every authenticated request to the tenant's upstream and pass the
response back, as the base that caching (M6), coalescing (M7) and streaming
(M9) build on. `docs/design.md` stays the source of truth; this note only
pins down how M5 implements it.

## Goals

- `internal/proxy`: a forwarder with no provider-specific code.
- Upstream is the tenant's `UpstreamURL` (per tenant since #14, not the single
  `PROXIE_UPSTREAM_URL` the ticket mentions).
- Total upstream timeout `PROXIE_UPSTREAM_TIMEOUT` (120s).
- `Accept-Encoding: identity` on every upstream request.
- Request bodies up to `PROXIE_MAX_REQUEST_BYTES` (10 MB) are buffered; larger
  ones are streamed through unchanged and marked not cacheable.

## Non-goals

Caching, `X-Proxie-Cache`, coalescing, streaming timeouts, metrics, retries.

## Anti-goals

- Leaking secrets: never log URLs with queries, headers, bodies or keys
  (`*url.Error` carries the full URL, so only its inner error is logged).
- Rejecting large bodies with 413: the design says they are forwarded.

## Approaches

- **Own forwarder (picked):** `Forward` returns the upstream `*http.Response`;
  the handler writes it. Caching wraps `Forward`, coalescing can run it
  detached from any one caller and share a buffered result.
- `httputil.ReverseProxy`: less code now, but writes straight to one caller's
  connection, which works against M6/M7.

## Design

**API (`internal/proxy`)**

- `New(Options{Timeout time.Duration}) *Forwarder`
- `(*Forwarder).Forward(ctx, tenant config.Tenant, r *http.Request) (*http.Response, error)`
  builds the upstream request and sends it. The timeout context is cancelled
  when the returned body is closed, so it covers the whole exchange.
- `ReadBody(r *http.Request, limit int64) (Body, error)` reads up to
  `limit+1` bytes. Within the limit: `Body{Bytes: b}` and `r.Body` is replaced
  by a reader over `b`. Over it: `Body{Oversize: true}` and `r.Body` becomes
  the read prefix followed by the rest of the original body. M6 will hash
  `Bytes` only when `!Oversize`.

**Upstream request**

- URL: upstream scheme and host; path is the upstream path joined with the
  request path (one `/` between them); query is the upstream query followed by
  the request query.
- Headers: copied, minus hop-by-hop headers (`Connection` and the headers it
  names, `Keep-Alive`, `Proxy-Connection`, `Proxy-Authorization`, `TE`,
  `Trailer`, `Transfer-Encoding`, `Upgrade`). `Host` is the upstream host.
  `Accept-Encoding: identity` is set. No `X-Forwarded-For` is added (the
  provider does not need the caller's IP). Provider keys pass untouched;
  `X-Proxie-Key` is already removed by the auth middleware.
- Client: clone of `http.DefaultTransport` with `DisableCompression`,
  `MaxIdleConnsPerHost: 100`; redirects are not followed but passed back.
- The caller's context is the parent, so a caller hanging up cancels the
  upstream call (M7 changes this for coalesced requests).

**Response and errors (handler in `internal/server`)**

- Status, headers (minus hop-by-hop) and body are copied to the caller.
- Timeout: `504` with `{"error": "upstream timeout"}`. Caller gone: nothing is
  written. Other errors: `502` with `{"error": "upstream unavailable"}`.
  Errors are logged at warn with tenant, without the URL.
- A copy error mid-body aborts the connection and is logged.

**Wiring**

- `NoRoute` becomes the proxy handler, so every method and path is proxied
  after the auth middleware. `GET /hello` and `notFoundHandler` are removed;
  unknown paths get the upstream's own 404.
- `server.New(logger, authenticator, forwarder)`; `serve.go` builds the
  forwarder from config.
- Docs: tick milestone 5 in `docs/design.md`, update the CLAUDE.md summary.

## Known trade-off

Until M9 the 120s total timeout also applies to streams, so streams longer
than that get cut.

## Left to the builder

Exact file split inside `internal/proxy` (keeping one `_test.go` per file),
test cases, error message wording.
