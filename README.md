# proxie
A prompt caching proxy for LLM APIs, written in Go + Gin

Project management: [Linear project "Proxie"](https://linear.app/smithlabs/project/proxie-a95d16693e25/overview)

## Running

proxie is configured only through environment variables; see the
[configuration table](docs/design.md#configuration). `PROXIE_UPSTREAM_URL` and
`PROXIE_REDIS_URL` are required. For local runs copy `.env.example` to `.env`
and use `task run`.

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
