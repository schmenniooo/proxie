# Proxy Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Forward every authenticated request to the tenant's upstream and pass the response back (milestone 5).

**Architecture:** A new `internal/proxy` package with three focused pieces: `ReadBody` (buffer up to the limit), `Forwarder.Forward` (build and send the upstream request, return the `*http.Response`) and `WriteResponse` (copy it to the caller). The Gin `NoRoute` handler in `internal/server` glues them together and maps errors to 502/504. `GET /hello` goes away.

**Tech Stack:** Go, Gin, `net/http`, testify, `httptest`.

**Spec:** `docs/superpowers/specs/2026-11-21-proxy-core-design.md` (and `docs/design.md`).

## Global Constraints

- Run everything through Task: `task test -- <flags> <pkg>`, `task lint`, `task check`. Never call `go` directly.
- One `_test.go` per source file, external `_test` package, testify `assert`/`require`; requests via `httptest.NewRequestWithContext(t.Context(), ...)`.
- Never log prompts, bodies, headers, query strings, keys or upstream URLs.
- Timeout default `PROXIE_UPSTREAM_TIMEOUT` = `120s` (`cfg.UpstreamTimeout`); body limit `PROXIE_MAX_REQUEST_BYTES` = `10485760` (`cfg.MaxRequestBytes`). Both already exist in `internal/config`; no config changes.
- Upstream is `tenant.UpstreamURL` (`*url.URL`), never a global URL.
- Commit messages: no ticket key; end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. Bodiless requests (GET, empty POST) must reach the upstream with `Content-Length: 0`/no body, not a chunked empty body. Test in Task 1 and Task 2.
2. A body of exactly the limit is buffered; one byte more is oversize but still forwarded byte-for-byte. Test in Task 1.
3. Upstream base paths with or without trailing slash, and percent-encoded request paths (`a%2Fb`), must join without `//` and without decoding. Test in Task 2.
4. A timeout after headers were sent must break the caller's connection, not end the body cleanly (a truncated JSON looks like success). Test in Task 4.
5. A secret in the upstream URL's query (`?key=`) must never show up in an error or a log line. Test in Task 2 and Task 4.

---

### Task 1: `ReadBody`

**Files:**
- Create: `internal/proxy/body.go` (with the package doc comment)
- Test: `internal/proxy/body_test.go`

**Interfaces:**
- Produces: `type Body struct { Bytes []byte; Oversize bool }` and `func ReadBody(r *http.Request, limit int64) (Body, error)`.

- [ ] **Step 1: Write the failing tests**

- `TestReadBodyWithinLimit`: body `"hello"`, limit 10 → `Body{Bytes: []byte("hello")}`, `r.ContentLength == 5`, reading `r.Body` again yields `"hello"`.
- `TestReadBodyAtLimit`: 10-byte body, limit 10 → `Oversize == false`, `len(Bytes) == 10`.
- `TestReadBodyOverLimit`: 11-byte body, limit 10 → `Oversize == true`, `Bytes == nil`, `io.ReadAll(r.Body)` equals the full 11 bytes.
- `TestReadBodyEmpty`: nil body → `Bytes` empty, `r.Body == http.NoBody`, `r.ContentLength == 0`.
- `TestReadBodyReadError`: body is a reader returning `errors.New("boom")` → error returned.

- [ ] **Step 2: Run to verify they fail**

Run: `task test -- -run TestReadBody ./internal/proxy`
Expected: FAIL (package or `ReadBody` undefined).

- [ ] **Step 3: Implement `ReadBody` in `internal/proxy/body.go`**

Read with `io.ReadAll(io.LimitReader(r.Body, limit+1))`. Within the limit: replace `r.Body` with `io.NopCloser(bytes.NewReader(b))` (or `http.NoBody` when empty) and set `r.ContentLength`. Over it: `r.Body` = a ReadCloser whose reader is `io.MultiReader(bytes.NewReader(prefix), original)` and whose `Close` closes the original; `ContentLength` unchanged.

- [ ] **Step 4: Run to verify they pass**

Run: `task test -- -run TestReadBody ./internal/proxy`
Expected: PASS

- [ ] **Step 5: Commit** (also add the spec and this plan)

```bash
git add internal/proxy/body.go internal/proxy/body_test.go docs/superpowers
git commit -m "Add request body buffering with size limit"
```

### Task 2: `Forwarder`

**Files:**
- Create: `internal/proxy/forwarder.go`
- Test: `internal/proxy/forwarder_test.go`

**Interfaces:**
- Consumes: `config.Tenant` (`UpstreamURL *url.URL`).
- Produces:
  - `type Options struct { Timeout time.Duration }`
  - `func New(opts Options) *Forwarder`
  - `func (f *Forwarder) Forward(ctx context.Context, tenant config.Tenant, r *http.Request) (*http.Response, error)`
  - `func RemoveHopHeaders(h http.Header)` (used by Task 3)

Tests use an `httptest.NewServer` upstream that records the request it got.

- [ ] **Step 1: Write the failing tests**

- `TestForwardJoinsPathAndQuery` (table): base `http://up`, `http://up/`, `http://up/v1`, `http://up/v1/`, `http://up/v1?api-version=1`; request `/chat?a=b` → upstream `RequestURI` respectively `/chat?a=b`, `/chat?a=b`, `/v1/chat?a=b`, `/v1/chat?a=b`, `/v1/chat?api-version=1&a=b`. Request without query and base without query → no `?`.
- `TestForwardKeepsEncodedPath`: request `/v1/a%2Fb` → upstream `RequestURI` is `/v1/a%2Fb`.
- `TestForwardHeaders`: request has `Authorization`, `X-Api-Key`, `Anthropic-Version`, `Connection: X-Custom`, `X-Custom`, `Keep-Alive`, `Te`, `Upgrade`, `Proxy-Authorization`, `Accept-Encoding: gzip` → upstream sees the first three unchanged, none of the hop headers or `X-Custom`, `Accept-Encoding == "identity"`, `Host` equal to the upstream's host, no `X-Forwarded-For`.
- `TestForwardMethodAndBody`: `POST` with body `{"a":1}` → upstream sees method `POST`, same body, `ContentLength == 7`.
- `TestForwardEmptyBody`: `GET` with `http.NoBody` → upstream `ContentLength == 0`, no `Transfer-Encoding`.
- `TestForwardDoesNotFollowRedirects`: upstream answers `302` with `Location: /elsewhere` → `resp.StatusCode == 302`, `/elsewhere` never requested.
- `TestForwardTimeout`: `Timeout: 50ms`, upstream sleeps 500ms → `errors.Is(err, context.DeadlineExceeded)`.
- `TestForwardTimeoutCoversBody`: `Timeout: 50ms`, upstream writes headers, flushes, sleeps 500ms, writes more → `Forward` succeeds, `io.ReadAll(resp.Body)` returns an error.
- `TestForwardCallerCancel`: cancelled ctx → `errors.Is(err, context.Canceled)`.
- `TestForwardErrorHidesURL`: upstream `http://127.0.0.1:1/?key=s3cret` (closed port) → error non-nil, `err.Error()` does not contain `s3cret`.
- `TestForwardReturnsUpstreamStatusAndBody`: upstream `418` body `teapot` → returned unchanged.

- [ ] **Step 2: Run to verify they fail**

Run: `task test -- -run TestForward ./internal/proxy`
Expected: FAIL (`New` undefined).

- [ ] **Step 3: Implement `forwarder.go`**

- Transport: `http.DefaultTransport.(*http.Transport).Clone()` with `DisableCompression = true`, `MaxIdleConnsPerHost = 100`. Client: that transport, `CheckRedirect` returns `http.ErrUseLastResponse`, no client `Timeout` (the context does it).
- URL: copy scheme/host from the tenant URL; path = `strings.TrimSuffix(base.EscapedPath(), "/") + r.URL.EscapedPath()`, set both `Path` (unescaped) and `RawPath`; query = base `RawQuery` and request `RawQuery` joined with `&`, skipping empty ones.
- Request: `http.NewRequestWithContext(ctx, r.Method, u.String(), r.Body)`, `ContentLength = r.ContentLength`, headers cloned then `RemoveHopHeaders`, `Accept-Encoding: identity`; `Host` left as the URL host.
- Hop headers: `Connection` plus every token it lists, `Keep-Alive`, `Proxy-Connection`, `Proxy-Authenticate`, `Proxy-Authorization`, `Te`, `Trailer`, `Transfer-Encoding`, `Upgrade`.
- Timeout: `context.WithTimeout(ctx, f.timeout)`; cancel on error, otherwise wrap `resp.Body` so `Close` also calls cancel.
- Errors: unwrap `*url.Error` to its `Err` before returning, so the URL never leaves the package.

- [ ] **Step 4: Run to verify they pass**

Run: `task test -- -run TestForward ./internal/proxy`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/forwarder.go internal/proxy/forwarder_test.go
git commit -m "Add upstream forwarder with timeout and identity encoding"
```

### Task 3: `WriteResponse`

**Files:**
- Create: `internal/proxy/response.go`
- Test: `internal/proxy/response_test.go`

**Interfaces:**
- Consumes: `RemoveHopHeaders(h http.Header)` from Task 2.
- Produces: `func WriteResponse(w http.ResponseWriter, resp *http.Response) error`. It closes `resp.Body`; a non-nil error means the status was already sent and the body is incomplete.

- [ ] **Step 1: Write the failing tests** (build `*http.Response` values by hand, write into `httptest.NewRecorder()`)

- `TestWriteResponseCopiesStatusHeadersBody`: status `201`, headers `Content-Type: application/json`, `X-Request-Id: r1`, body `{"ok":true}` → recorder has all three unchanged.
- `TestWriteResponseDropsHopHeaders`: `Connection`, `Keep-Alive`, `Transfer-Encoding` set → absent in recorder.
- `TestWriteResponseKeepsContentEncoding`: `Content-Encoding: gzip` and raw gzip bytes → header present, bytes identical.
- `TestWriteResponseReportsBodyError`: body reader fails after 3 bytes → error returned, recorder code is the upstream status, body closed.

- [ ] **Step 2: Run to verify they fail**

Run: `task test -- -run TestWriteResponse ./internal/proxy`
Expected: FAIL

- [ ] **Step 3: Implement `WriteResponse` in `internal/proxy/response.go`**

Clone headers, `RemoveHopHeaders`, copy into `w.Header()`, `WriteHeader`, `io.Copy`, `resp.Body.Close()` via defer.

- [ ] **Step 4: Run to verify they pass**

Run: `task test -- -run TestWriteResponse ./internal/proxy`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/response.go internal/proxy/response_test.go
git commit -m "Add upstream response writer"
```

### Task 4: Proxy handler, wiring and docs

**Files:**
- Modify: `internal/server/server.go`, `routes.go`, `handlers.go`
- Modify: `internal/cli/serve.go:66`
- Modify tests: `internal/server/server_test.go`, `handlers_test.go`, `routes_test.go`, `middleware_test.go`
- Modify: `docs/design.md` (tick milestone 5), `CLAUDE.md` (summary line and the `TestHello` example), `Taskfile.yml:49` (`TestHello` example)

**Interfaces:**
- Consumes: `proxy.ReadBody`, `(*proxy.Forwarder).Forward`, `proxy.WriteResponse`, `server.TenantFrom`.
- Produces: `func New(logger *slog.Logger, a *auth.Authenticator, fwd *proxy.Forwarder, maxRequestBytes int64) *gin.Engine`.

- [ ] **Step 1: Rework the test helpers in `server_test.go`**

`newServer(t, logger)` starts an `httptest.NewServer` fake upstream (closed via `t.Cleanup`) answering `200 "ok"` on every path except `/nope`, which answers `404 "not found"`, and gives tenant `acme` that `UpstreamURL`. Add `newServerWithUpstream(t, logger, upstream http.Handler, timeout time.Duration, maxRequestBytes int64) *gin.Engine` for the proxy tests; `newServer` calls it with `120*time.Second` and `10485760`. Replace `/hello` in `middleware_test.go` with `/v1/x`; `TestAccessLogCountsNotFoundBytes` uses `GET /nope`. Delete `TestHelloRouteIsRegistered`, `TestHelloRouteRejectsPost`, `TestHelloHandlerBody`, `TestNotFoundHandlerBody`.

- [ ] **Step 2: Write the failing handler tests**

In `handlers_test.go`:
- `TestProxyForwardsRequest`: `POST /v1/messages?x=1` with body and `Authorization` → upstream got path, query, body, `Authorization`, no `X-Proxie-Key`; caller gets upstream status, headers and body.
- `TestProxyPassesUpstreamErrors`: upstream `429` with body → caller gets `429` and that body.
- `TestProxyForwardsOversizeBody`: `maxRequestBytes` 10, body of 25 bytes → upstream got all 25 bytes.
- `TestProxyUpstreamTimeoutReturns504`: timeout 50ms, upstream sleeps 500ms → `504`, JSON `{"error":"upstream timeout"}`.
- `TestProxyUpstreamDownReturns502`: tenant URL `http://127.0.0.1:1/?key=s3cret` → `502`, JSON `{"error":"upstream unavailable"}`, log contains `"tenant":"acme"`, log does not contain `s3cret`.
- `TestProxyTimeoutMidBodyBreaksConnection`: serve the engine with `httptest.NewServer`; upstream with `Content-Length: 100` writes 10 bytes, flushes, sleeps 500ms; timeout 50ms → client `io.ReadAll(resp.Body)` returns an error.

In `routes_test.go`:
- `TestEveryMethodIsProxied` (table: GET, POST, PUT, PATCH, DELETE) → upstream sees each method on `/v1/x`.

- [ ] **Step 3: Run to verify they fail**

Run: `task test -- ./internal/server`
Expected: FAIL (compile error on `server.New` arity).

- [ ] **Step 4: Implement**

- `server.New` takes `fwd` and `maxRequestBytes` and passes them to `registerRoutes`, which sets `r.NoRoute(proxyHandler(logger, fwd, maxRequestBytes))`.
- `handlers.go`: remove `helloHandler`, `notFoundHandler`, `greeting`. `proxyHandler` gets the tenant (`TenantFrom`; missing → 500), calls `ReadBody` (error → `400 {"error":"could not read request body"}`), `Forward` with `c.Request.Context()`. Error mapping: caller ctx already done → `c.Abort()` with nothing written; `context.DeadlineExceeded` → 504; anything else → 502. Upstream errors are logged at warn with `tenant` and `error` only. `WriteResponse` error → log at warn, then `panic(http.ErrAbortHandler)` so the connection is cut (the recovery middleware re-panics it).
- `serve.go`: `proxy.New(proxy.Options{Timeout: cfg.UpstreamTimeout})`, pass it and `cfg.MaxRequestBytes` to `server.New`.

- [ ] **Step 5: Run to verify they pass**

Run: `task check`
Expected: lint clean, all tests PASS.

- [ ] **Step 6: Update docs**

Tick `- [x] 5. Proxy core…` in `docs/design.md`. In `CLAUDE.md` replace "and a hello world `GET /hello`" with "and the proxy core (`internal/proxy`, every path is forwarded to the tenant's upstream)", and change the `TestHello` examples in `CLAUDE.md` and `Taskfile.yml` to `TestProxyForwardsRequest`.

- [ ] **Step 7: Commit**

```bash
git add internal docs CLAUDE.md Taskfile.yml
git commit -m "Proxy every route to the tenant's upstream"
```
