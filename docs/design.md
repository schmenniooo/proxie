# proxie design

proxie is a standalone reverse proxy that caches responses of LLM APIs. It
sits between a caller application and an LLM provider. Identical requests are
answered from Redis instead of paying the provider again.

```
caller app ──▶ proxie ──▶ LLM provider (any, set by env var)
                 │
                 └──▶ Redis
```

## Scope

- A response cache: identical request, stored response. Provider-side prompt
  caching hints (for example Anthropic `cache_control`) are out of scope.
- Provider-agnostic. proxie forwards to one upstream base URL set by env var.
  There is no provider-specific code.
- Standalone microservice. It is not a Gin middleware library.
- Redis is the only store.

## Request flow

1. The caller sends a request with a proxie key (`X-Proxie-Key`) and the
   provider key in the provider's normal header (`Authorization` or
   `x-api-key`).
2. proxie checks the proxie key and resolves the tenant. The proxie key header
   is removed before forwarding. The provider key is forwarded untouched.
3. proxie builds the cache key and asks Redis.
4. Hit: the stored response is replayed.
5. Miss: the request is forwarded to the upstream, the response is passed
   back and a copy is stored.

## Tenancy and auth

- Multi-tenant. Isolation is by key prefix inside the shared Redis.
- One env var per tenant: `PROXIE_API_KEY_<TENANT>=<key>`. The suffix is the
  tenant name, lowercased.
  - Suffix may only contain letters, digits, `-` and `_`.
  - A value may hold several comma-separated keys to allow key rotation.
  - Startup fails if no tenant exists or if two tenants share a key.
  - Keys are compared in constant time and are never logged.
- The tenant list is read once at startup. Adding a tenant needs a restart.
- Known trade-off: tenants share the capacity of one instance, so a busy
  tenant can slow the others (noisy neighbor). Per-tenant rate limiting is not
  part of v1.

## Cache key

`proxie_v1_<tenant>_<sha256>`

The hash covers:

- HTTP method, path and query string.
- The canonicalized body: JSON with sorted object keys, no insignificant
  whitespace, numbers kept exactly as sent, array order preserved. Non-JSON
  bodies are hashed as raw bytes.
- An allow-list of headers that can change the response (for example
  `anthropic-version`, `anthropic-beta`, `content-type`), configurable.
- Optionally, body fields can be excluded through an ignore list (for example
  `user`, `metadata`). The list is empty by default: when in doubt, a field
  stays in the hash, which is the safe choice.

Not part of the key: auth headers, `user-agent`, tracing and request ID
headers. The `v1` allows invalidating all entries by bumping the version.

`stream: true` is part of the body, so streaming and non-streaming requests
never share an entry.

## Cache semantics

- Only `POST` requests with a 2xx response are cached. Errors are never cached.
- Everything cacheable is cached, including non-deterministic requests
  (temperature above 0). Callers opt out per request with
  `X-Proxie-Cache: bypass`.
- Every response carries `X-Proxie-Cache: HIT|MISS|BYPASS`.
- Default TTL is 1 hour. Redis expires entries on its own. Eviction is left to
  Redis (`maxmemory-policy allkeys-lru`). There is no purge endpoint in v1.
- The upstream is asked for uncompressed responses (`Accept-Encoding:
  identity`) so stored bytes are readable by every caller.

## Failure behavior

- Redis down or slow: fail open. The request is forwarded to the upstream
  uncached. Redis calls use a short timeout (200ms by default).
- If proxie itself is down, the fallback to the upstream has to live in the
  caller or the infrastructure (client retry, load balancer). A service that is
  down cannot forward anything.

## Concurrency

- Request coalescing (singleflight) for non-streaming requests: concurrent
  identical requests on a cold cache trigger one upstream call, the others wait
  and share the result.
- The upstream call is independent of any single caller's connection, so the
  first caller leaving does not fail the waiting callers. The result is cached.
- If the shared call fails, all waiting callers get that error. There is no
  automatic retry.
- Streaming requests are not coalesced.
- Known limitation: coalescing is per process. With N replicas, identical
  simultaneous requests can reach the upstream up to N times. A Redis lock
  could fix this later.

## Streaming

- A stream is detected by the response `Content-Type: text/event-stream`.
- On a miss the pieces are passed to the caller immediately while a copy is
  kept. The copy is stored only after a clean end with a 2xx status.
- On a hit the stored pieces are replayed at once, without the original timing.
- Caller hangs up mid-stream: the upstream request is cancelled and nothing is
  cached. An opt-in env toggle to finish the stream anyway may come later.
- Upstream breaks mid-stream: nothing is cached, the error is passed on.
- Entries above the size limit are not stored. The stream continues unaffected.

## Timeouts and limits

| Setting | Default |
| --- | --- |
| Upstream, non-streaming, total | 120s |
| Upstream, streaming, time to first piece | 30s |
| Upstream, streaming, max silence between pieces | 30s |
| Redis operation | 200ms |
| Max request body | 10 MB (larger bodies are forwarded uncached) |

All values are configurable by env var.

## Configuration

Environment variables, wrapped in one config struct. Names below are the
proposal and may be adjusted while building.

| Variable | Meaning | Default |
| --- | --- | --- |
| `PROXIE_UPSTREAM_URL` | Base URL requests are forwarded to | required |
| `PROXIE_API_KEY_<TENANT>` | Proxie key(s) of a tenant | at least one required |
| `PROXIE_REDIS_URL` | Redis URL (`rediss://` for TLS) | required |
| `PROXIE_REDIS_PASSWORD` | Redis password | empty |
| `PROXIE_CACHE_TTL` | Entry lifetime | `1h` |
| `PROXIE_IGNORE_FIELDS` | Body fields excluded from the hash | empty |
| `PROXIE_KEY_HEADERS` | Header allow-list for the hash | see cache key |
| `PROXIE_MAX_REQUEST_BYTES` | Max request body | `10485760` |
| `PROXIE_MAX_ENTRY_BYTES` | Max stored response | to decide |
| `PROXIE_ADDR` | Proxy listen address | `:8080` |
| `PROXIE_ADMIN_ADDR` | Admin listen address (`/healthz`, `/metrics`) | `:9090` |

## Observability

- Structured logging with `log/slog`. Prompts, bodies and keys are never logged.
- Prometheus metrics on the admin port, scrapable by Grafana or CloudWatch.
  Metrics carry a `tenant` label (hits, misses, bypasses, errors, latency).
  Many tenants would inflate label cardinality.
- `/healthz` is unauthenticated and lives on the admin port.

## Package layout

```
cmd/proxie/        main.go, starts the server
internal/server/   engine setup (server.go) and routes (routes.go)
internal/config/   env config struct
internal/store/    Store interface and Redis implementation
internal/keygen/   canonicalization and cache key building
internal/cache/    hit/miss flow, coalescing
internal/proxy/    reverse proxy to the upstream
internal/auth/     proxie key check and tenant lookup
```

Routes are the starting point of business logic. `server.go` only builds the
engine. Each source file has exactly one `_test.go` next to it.

## Delivery

- Multi-stage Dockerfile.
- GitHub Actions: tests on pull requests. On a version tag created from `main`
  the image is built and pushed to the GitHub container registry (ghcr.io).
- Work happens on branches and pull requests.

## Milestones

Each milestone is one small PR with its own tests. A checked box means the
milestone is done in the pull request that is linked or currently open.

- [x] 0. Scaffold: Go module, Gin hello world server, design doc (PR #1)
- [ ] 1. Config, structured logging, graceful shutdown
- [ ] 2. `Store` interface and Redis implementation (timeouts, fail open)
- [ ] 3. Keygen: canonicalization, key building, header allow-list, ignore list
- [ ] 4. Auth: `PROXIE_API_KEY_<TENANT>` loading and key check
- [ ] 5. Proxy core: reverse proxy, non-streaming, timeouts, body limit
- [ ] 6. Cache layer: hit/miss, TTL, `X-Proxie-Cache`, bypass, 2xx only
- [ ] 7. Coalescing for non-streaming requests
- [ ] 8. Metrics and health endpoints on the admin port
- [ ] 9. Streaming: tee, store after clean end, replay, hang-up handling
- [ ] 10. Dockerfile and CI/CD (may move earlier, right after milestone 1)

## Open items

- Value of `PROXIE_MAX_ENTRY_BYTES`.
- Per-tenant rate limiting (noisy neighbor).
- Redis lock to coalesce across replicas.
- Opt-in to finish abandoned streams.
