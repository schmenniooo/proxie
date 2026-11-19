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
Compose on port 8080 (`task down` stops them). `task test` and `task lint` run
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
ends in `pass`, `warn` or `fail`.

Locally, `task build` puts the CLI into `bin/proxie`, and `task cli -- doctor`
runs a command inside the compose setup next to its Redis.

The image's entrypoint is the CLI and its default command is `serve`. It is
distroless (no shell), so run the commands directly:

```
docker exec proxie /proxie doctor
docker exec proxie /proxie config
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
