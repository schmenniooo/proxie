# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

proxie is a standalone, provider-agnostic reverse proxy (Go + Gin) that caches LLM API responses in Redis. `docs/design.md` is the source of truth for all design decisions (cache key format, tenancy, streaming, coalescing, timeouts, env vars, milestones). Read it before building anything; most of it is not implemented yet. Currently only a hello world `GET /hello` exists.

## Commands

Use [Task](https://taskfile.dev) (`Taskfile.yml`) instead of calling the Go toolchain directly. Install with `brew install go-task golangci-lint`.

```
task                                          # list tasks
task build                                    # bin/proxie
task run                                      # listens on :8080 (PORT overrides)
task test                                     # go test ./...
task test -- -run TestHello ./internal/server # single test
task lint                                     # golangci-lint (config: .golangci.yml)
task fmt                                      # gofmt + goimports via golangci-lint
task check                                    # lint, then test
```

## Conventions

- Entrypoint is `cmd/proxie/main.go`. Packages live under `internal/`.
- `internal/server/server.go` only builds the Gin engine (middleware, then `registerRoutes`). Routes are registered in `routes.go`; handlers live in `handlers.go`, which is the starting point of business logic.
- Exactly one `_test.go` per source file, next to it (`foo.go` -> `foo_test.go`). Tests use the external `_test` package and go through exported APIs (e.g. `server.New().ServeHTTP`). Do not add separate white box test files.
- Tests use `testify` (`assert`). Build requests with `httptest.NewRequestWithContext(t.Context(), ...)`; the `noctx` linter rejects `httptest.NewRequest`.

## Workflow

- Never commit to `main` directly; use a branch and a PR (open as draft, assigned to the user). The one exception was an explicit user request.
- Worktrees go under `.claude/worktrees/` inside the main checkout (`.claude/` is excluded via `.git/info/exclude`).
- Project management is in Linear, project "Proxie", key `PRO`. Every PR title starts with the key and the zero-padded milestone number: `PRO-000: ...` for the scaffold, `PRO-001: ...` for milestone 1, and so on.
- Never put test steps or a test plan in a PR description.
- Each milestone in `docs/design.md` is one small PR. Tick its checkbox in the design doc (and the PR description) in the PR that completes it.
- Co-author trailer on commits and the generated-with line on PR bodies, as given by the session attribution reminder.
