# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

proxie is a standalone, provider-agnostic reverse proxy (Go + Gin) that caches LLM API responses in Redis. `docs/design.md` is the source of truth for all design decisions (cache key format, tenancy, streaming, coalescing, timeouts, env vars, milestones). Read it before building anything; most of it is not implemented yet. Currently config, logging, graceful shutdown, the Redis cache (`internal/cache`), proxie key auth (`internal/auth`, enforced on every route by `requireProxieKey`), the Kong CLI (`serve`, `doctor`, `config`, `version`) and the proxy core (`internal/proxy`, everything under `/v1/upstream/` is forwarded to the tenant's upstream) exist.

## Commands

Use [Task](https://taskfile.dev) (`Taskfile.yml`) instead of calling the Go toolchain directly. Every task runs in Docker via `compose.yaml` (Go and golangci-lint images pinned to the Dockerfile and CI versions), so the host only needs Docker and Task (`brew install go-task`).

```
task                                          # list tasks
task build                                    # proxie Docker image
task run                                      # proxie + Redis via compose in the background, settings from .env.proxie
task cli -- proxie doctor                     # any command inside the running proxie container (task run first)
task request                                  # test request through the running proxie, from the TEST_* values in .env.test
task down                                     # stop the compose containers
task test                                     # go test ./...
task test -- -run TestProxyForwardsRequest ./internal/server # single test
task lint                                     # golangci-lint (config: .golangci.yml)
task fmt                                      # gofmt + goimports via golangci-lint
task check                                    # lint, then test
```

Local env files, both gitignored, each with a committed `*.example` that explains it: `.env.proxie` holds proxie's own settings and is loaded into the proxie container (never put provider keys there; proxie holds none). `.env.test` holds the `task request` values, including a provider key, and is loaded only by the curl test container.

## Conventions

- Entrypoint is `cmd/proxie/main.go`, which only hands off to `internal/cli` (Kong root command, one file per command). Packages live under `internal/`.
- `internal/server/server.go` only builds the Gin engine (middleware, then `registerRoutes`). Routes are registered in `routes.go`; handlers live in `handlers.go`, which is the starting point of business logic. Middleware is in `middleware.go`, serving and graceful shutdown in `run.go`.
- Config comes from env vars and flags via `internal/config` (see `docs/design.md`). `Config` carries Kong tags (`name`, `env`, `default`, `help`); a new setting is a new tagged field plus its check in `Complete`. Kong only parses; `Complete` loads tenants and validates, collecting all errors. URL fields use `config.URL`, whose decoding never fails, so Kong never echoes a URL (and its password) in an error. Log with the injected `*slog.Logger`; never log prompts, bodies, headers, query strings or keys.
- Exactly one `_test.go` per source file, next to it (`foo.go` -> `foo_test.go`). Tests use the external `_test` package and go through exported APIs (e.g. `server.New().ServeHTTP`). Do not add separate white box test files.
- Tests use `testify` (`assert`). Build requests with `httptest.NewRequestWithContext(t.Context(), ...)`; the `noctx` linter rejects `httptest.NewRequest`.

## Workflow

- Never commit to `main` directly; use a branch and a PR (open as draft, assigned to the user). The one exception was an explicit user request.
- Worktrees go under `.claude/worktrees/` inside the main checkout (`.claude/` is excluded via `.git/info/exclude`).
- Project management is in Linear, project "Proxie". For now PR titles use the `SMI` prefix (the Linear team is called SmithLabs, so its issues are `SMI-<n>`), e.g. `SMI-16: Dockerfile and CI/CD`. Earlier PRs used `PRO-<zero-padded milestone>` (`PRO-000` for the scaffold); that scheme is paused.
- Never put the ticket key (`SMI-<n>`, `PRO-<n>`) in commit messages. It belongs in the PR title only.
- Never put test steps or a test plan in a PR description.
- Each milestone in `docs/design.md` is one small PR. Tick its checkbox in the design doc (and the PR description) in the PR that completes it.
- Co-author trailer on commits and the generated-with line on PR bodies, as given by the session attribution reminder.
