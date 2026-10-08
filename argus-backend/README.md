# argus-backend

The Argus collector (Go module `argus-collector`): it watches the Docker containers and HTTP targets of a single host, looks for matching lines in container logs, stores everything in Postgres and sends alerts to Telegram. It serves an HTTP API under `/v1/api`, which the `../argus-frontend` dashboard uses.

## What's inside

- **watcher** - Docker events (start/die/oom/kill/restart/destroy), container statuses, crash-loop detection.
- **prober** - HTTP checks for targets of type `probe`; a target goes `down` after `prober.failure_threshold` failures in a row.
- **matcher** - regex rules over container logs.
- **alerter** - sends alerts to Telegram, with grouping and a cooldown.
- **host** - host CPU, memory and disk (`GET /v1/api/host`), with threshold alerts.
- **retention** - cleans up old probes, events and log matches.
- **heartbeat** - dead man's switch: while the collector and the database are alive, it hits `HEARTBEAT_URL` once every `heartbeat.interval`.

Containers are monitored when they carry the label `argus.monitor=true`. If `argus.probe.url` is set as well, an HTTP target is also created for the container.

## Quick start (Docker Compose)

```bash
cp .example.env .env              # fill in POSTGRES_*, and optionally BOT_TOKEN, CHAT_ID, HEARTBEAT_URL
cp config.example.yaml config.yaml
docker compose up -d --build
```

The collector listens on `127.0.0.1:8099`; Postgres is reachable only from the internal compose network. To start it together with the dashboard, run `docker compose up -d --build` from the repository root: the root `compose.yml` includes this file.

Inside the container, `POSTGRES_URL` must point at the `argus-postgres` service, for example `postgres://user:pass@argus-postgres:5432/argus?sslmode=disable`.

## Running locally

You need Go 1.27+ and a reachable Postgres.

```bash
cp .example.env .env              # POSTGRES_URL pointing at your local Postgres
cp config.example.yaml config.yaml
go run ./cmd/argus
```

The watcher and the matcher need access to the Docker socket. If Docker isn't available, they don't start, but the rest of the service keeps working.

## Configuration

Secrets and paths come from the environment (or from `.env`, which is loaded automatically):

| Variable | Required | Purpose |
|---|---|---|
| `CONFIG_PATH` | yes | path to `config.yaml` |
| `POSTGRES_URL` | yes | Postgres connection string |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | for compose | credentials for the Postgres container |
| `BOT_TOKEN`, `CHAT_ID` | no | Telegram. Without them the alerter doesn't start |
| `HEARTBEAT_URL` | no | URL for the dead man's switch. Without it the heartbeat doesn't start |
| `HOST_DISK_PATH` | no | overrides `host.disk_path` (in compose it is `/hostfs`) |

Everything else is set in `config.yaml`, with an example in `config.example.yaml`: the HTTP server address and timeouts, prober intervals, crash-loop settings for the watcher, retention, Telegram (`notify_interval`, `cooldown`), matcher rules, host thresholds and `heartbeat.interval`. Durations use Go's format (`336h`, `5m`); there is no `d` suffix. Severity in matcher rules is one of `critical|error|warning|info|debug`.

The config is validated on startup, and the service won't start with an invalid one.

## Migrations

Goose migrations live in `migrations/`. They are embedded in the binary and applied automatically on startup (one transaction per file, under a Postgres advisory lock). No separate CLI is needed.

A new migration is the next numbered file, `migrations/0000N_name.sql`, with `-- +goose Up` / `-- +goose Down` sections. If the database version is newer than the latest embedded migration, the service refuses to start (downgrade protection). After changing a migration or a storage query, run `make test-db`.

## API

- `GET /healthz` - healthcheck.
- `/v1/api/targets` - create (`POST`), list (`GET`), get (`GET /{id}`), delete (`DELETE /{id}`), change status (`PATCH /{id}/status`). There is no full update.
- `/v1/api/alerts` - create (`POST`), list (`GET`), get (`GET /{id}`), delete (`DELETE /{id}`). Alerts can't be updated.
- `/v1/api/probes`, `/v1/api/events`, `/v1/api/log-matches` - list (`GET`), get (`GET /{id}`), delete (`DELETE /{id}`).
- `GET /v1/api/host` - latest host CPU, memory and disk sample.
- Swagger UI: `http://127.0.0.1:8099/swagger/index.html`.

## Development

```bash
make test        # go test ./...
make test-db     # storage integration tests against a throwaway postgres:16-alpine
make cover       # coverage without generated code
go vet ./...
golangci-lint run
```

- Mocks: run `mockery` after changing the interfaces in `internal/usecase/usecase.go`.
- Swagger: run `swag init -g cmd/argus/main.go -d ./ -o ./docs` after changing handler annotations.
