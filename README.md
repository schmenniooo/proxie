# proxie
A prompt caching proxy for LLM APIs, written in Go + Gin

Project management: [Linear project "Proxie"](https://linear.app/smithlabs/project/proxie-a95d16693e25/overview)

## Running

proxie is configured through environment variables, each of which can be
overridden by a flag; see the
[configuration table](docs/design.md#configuration) or `proxie --help`.
`PROXIE_REDIS_URL` and at least one `PROXIE_API_KEY_<TENANT>` are required,
plus an upstream URL for every tenant (`PROXIE_UPSTREAM_URL` as the default).

Local development only needs Docker and [Task](https://taskfile.dev). Copy
`.env.example` to `.env`, then `task run` starts proxie and Redis with Docker
Compose on port 8080 in the background (`task down` stops them). Point an
SDK at `http://localhost:8080/v1/upstream`; everything after that prefix is
forwarded to the tenant's upstream. `task test` and `task lint` run
in containers too.

## Commands

proxie is one binary with a few commands. They read the same config as the
server, so `doctor` and `config` show exactly what `serve` would use.

```
proxie                     # same as proxie serve
proxie serve --addr :9000  # the proxy; flags override PROXIE_* env vars
proxie doctor [--json]     # health checks, exit code 1 if one fails
proxie config [--json]     # resolved config, secrets masked
proxie version
```

`doctor` checks the config, Redis (reachable, latency, `maxmemory-policy` is
`allkeys-lru`), every upstream URL (DNS, TLS handshake, the base URL answers)
and, when `PROXIE_LOG_FILE` is set, that the log file is writable. Each check
ends in `pass`, `warn` or `fail`. proxie holds no provider keys, so the
upstream request is unauthenticated: 401 or 403 is the expected answer and is
shown as "reachable, auth required". It proves the upstream is reachable, not
that a key works.

For local development, run commands through Task only. Start the stack with
`task run` first; `task cli -- <command>` then runs any command inside that
running proxie container, with its env and next to its Redis, for example
`task cli -- proxie doctor` or `task cli -- proxie --help`. The image is
distroless, so `proxie` (on `PATH`) is the only executable (no shell).
`task request` sends one test request through the running proxie, built only
from the `TEST_*` values in the "Local testing" section of `.env` (proxie key,
provider key, endpoint, body; see `.env.example`). proxie ignores them, but
compose passes `.env` into its container too, so use a provider key meant for
testing. `serve` already
runs via `task run`. No proxie binary is built on the host.

The image's entrypoint is the CLI and its default command is `serve`. It is
distroless (no shell), so run the commands directly:

```
docker exec proxie proxie doctor
docker exec proxie proxie config
```

No command prints prompts, bodies or keys.

## Logging

Logs are structured (`log/slog`, JSON by default) and always go to stdout.
Prompts, bodies, headers, query strings and keys are never logged.

- **Containers (Docker, ECS, Kubernetes):** read stdout, for example with the
  `awslogs` log driver or `kubectl logs`. No extra setup.
- **VM or bare metal:** set `PROXIE_LOG_FILE=/var/log/proxie/proxie.log` (the
  directory must exist and be writable). proxie rotates the file by size
  (`PROXIE_LOG_FILE_MAX_MB`, `PROXIE_LOG_FILE_MAX_BACKUPS`,
  `PROXIE_LOG_FILE_MAX_AGE_DAYS`), so no `logrotate` is needed. Point the
  CloudWatch agent's `file_path` at the file; CloudWatch Logs Insights reads
  the JSON fields automatically.
- **Docker with a log file:** mount a volume and point `PROXIE_LOG_FILE` into
  it, for example `-v ./logs:/var/log/proxie`. The image runs as a non-root
  user, so the mounted directory must be writable for it.
