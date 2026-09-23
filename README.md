<div align="center">

<img src="docs/media/logo.png" width="96" alt="Argus">

# Argus

**Self-hosted uptime and Docker monitoring for a single host.**
Two small Go services, one Postgres, and a dashboard that refuses to make numbers up.

</div>

![Argus overview black](docs/media/overview_black.png)
![Argus overview white](docs/media/overview_white.png)

## What it is

Argus watches one machine and answers three questions: *is my service responding*, *are my containers staying up*, and *what happened while I wasn't looking*.

It is built for the case where Prometheus + Grafana + Alertmanager is three services too many — a single VPS running a handful of containers. You label the containers you care about, point Argus at a health-check URL, and get a dashboard plus Telegram alerts. No agents, no exporters, no query language.

Everything it knows comes from three sources: HTTP health checks it performs itself, the Docker event stream, and the logs of the containers it watches. Nothing is inferred, sampled, or smoothed.

![A failed health check becomes an outage, a Telegram alert, and a measured recovery](docs/media/incident.svg)

## The rule that shapes everything

**Only what is actually in the data reaches the screen.** A monitoring tool that shows a plausible number in place of a missing one is worse than a broken one, so Argus doesn't:

| Situation | What Argus shows | What it refuses to show |
| --- | --- | --- |
| An hour with no checks | a gap in the chart | a zero, drawn as if latency were 0 ms |
| A target that has never been checked | `—` | `100% uptime` |
| A container that went down, came back, but the return was never recorded | `return not recorded`, duration `—` | a downtime counter that keeps growing |
| An empty database | empty states in every block | demo numbers |

The same rule applies to sums: the outage summary adds up only the intervals it could actually measure, and says out loud how many it couldn't.

## How it works

```
    HTTP health checks   Docker events         container logs
    (GET, every 60s)     (die / oom / kill /   (stdout, stderr,
            │             restart / start …)    regex rules)
            │                    │                   │
            ▼                    ▼                   ▼
    ┌──────────────────────────────────────────────────┐
    │  collector  (argus-backend)       REST API :8099 │
    │  prober · watcher · matcher · alerter · cleaner  │
    └──────────────────────────────────────────────────┘
            │                              │
            ▼                              ▼
       PostgreSQL                    Telegram bot
            │
            │  GET /v1/api/…  (read-only)
            ▼
    ┌──────────────────────────────────────────────────┐
    │  dashboard  (argus-frontend)  :8088              │
    │  htmx · Alpine · Chart.js on Go templates         │
    └──────────────────────────────────────────────────┘
```

**The collector** owns everything that writes.

- The *prober* sends a `GET` to every probe target every 60 seconds and records latency and outcome; a response under HTTP 300 counts as up. One failed check doesn't page anyone: the target goes down after `failure_threshold` failures in a row (2 by default), and the first successful check brings it back up.
- The *watcher* subscribes to the Docker event stream and records container lifecycle events as they happen — no polling, no missed restarts. It also notices a crash loop: 5 exits within 5 minutes by default.
- The *matcher* follows the output of every watched container and records the lines that match your rules.
- The *alerter* groups new alerts by target, sends one Telegram message per target, and marks them as sent.
- The *cleaner* deletes probes, events and log matches older than 14 days, once a day. Alerts are kept.

**The dashboard** is a separate process that only reads, over the collector's own REST API. It holds no state, has no write endpoints, and can be restarted or rebuilt while the collector keeps recording. If the collector is down, the dashboard says so instead of showing stale numbers as if they were live.

The two processes share nothing but the API. Start them in any order.

## What you get

### Outage history with real durations

Argus doesn't store "incidents" — it reconstructs them on read from probe results and Docker events. A run of failed checks becomes one outage; a `die` closed by a `start` becomes one outage with a measured duration. One stop of a container is one outage, even though Docker reports it as a `kill` and a `die` a second apart.

They live under *History → Outages*, with totals on top: how many outages there were, how much downtime was actually measured, and the longest one.

![Outage history](docs/media/outages.png)

### A per-container page that admits what it doesn't know

The 24-hour bar shows measured outages. Its background is deliberately neutral, not green: Docker only reports failures, so a quiet stretch means *no events were recorded*, which is not the same as proof the container was up. Below the bar, every outage is listed with its cause and duration, because a seven-minute outage is 0.5% of a day and unreadable on any bar.

![Container detail](docs/media/container.png)

The same page has the container's Docker events per hour, the raw event list, and the log lines it matched. Click a container in the sidebar and you land there. Containers that need attention sort above the calm ones.

![Overview to container](docs/media/container.svg)

### Alert history you can actually narrow down

*History → Alerts* filters by target type (uptime or container) and severity. Filters are links, so a filtered view has its own URL, and the list swaps in place without a reload. Critical and error alerts from the last hour are drawn as dark rows so they stand out; everything older gets a thin border in its severity colour. The list is paged 50 rows at a time, and only the first page refreshes itself, so older rows never slide away under the cursor. An empty result says "No alerts" and admits it can't tell whether the filter matched nothing or nothing was recorded yet.

![Alert filters](docs/media/alert-filters.svg)

### Log lines that match your rules

The matcher follows stdout and stderr of every watched container and checks each new line against a list of regular expressions from the collector's config. A line that matches is stored verbatim with the rule's severity and raises an alert — `Log line matched rule "panic|FATAL"` — which goes to Telegram like any other.

```yaml
matcher:
  rules:
    - pattern: "panic|FATAL"            # Go regexp
      severity: critical                # critical / error / warning / info / debug
      container: "*"                    # every watched container
    - pattern: "(?i)connection refused"
      severity: error
      container: "argus-db"             # or one container, by name
```

Only new lines are read: Argus doesn't rescan old logs on startup. If the log stream drops, the matcher reconnects and resumes from the last line it saw, so a reconnect neither repeats nor skips a line. A container that starts later is picked up as soon as Docker reports the `start`.

The overview counts matched lines over the last 24 hours, and the count feeds the mascot: one or two matches and Argus is watching, five or more and it's angry. Each container's page shows its own matches. *History → Log matches* has hourly bars split by severity, a container filter, severity chips, and every matched line as it was written, 50 per page.

![Log matches](docs/media/log_matches.png)

### Charts that keep their gaps

Latency p95 and uptime across all probe targets, Docker events per hour for a single container, and log matches per hour split by severity. The charts are the one part of the page htmx never replaces — points arrive as JSON and datasets update in place, so tooltips and animation survive.

![Charts](docs/media/charts.svg)

### A mascot that tells you the mood before you read a number

<p>
<img src="docs/media/mascot-happy.gif" width="72" alt="happy">
<img src="docs/media/mascot-watching.gif" width="72" alt="watching">
<img src="docs/media/mascot-scared.gif" width="72" alt="scared">
<img src="docs/media/mascot-angry.gif" width="72" alt="angry">
<img src="docs/media/mascot-sleeping.gif" width="72" alt="sleeping">
</p>

The mood comes from the same data as everything else, and the loudest signal wins:

| Mood | When |
| --- | --- |
| All quiet | every target is up, no alerts, no log matches in 24 hours |
| Watching | 1–2 log matches in 24 hours |
| Something's off | a restart in the last hour, a failed check in 24 hours, a warning alert, or 3–4 log matches |
| Critical failure | a target is down, a critical alert in 24 hours, or 5+ log matches |
| Asleep | nothing to watch yet, or no answer from the collector |

Asleep never means all is well. Next to the mood, the card says which signal chose it: `web-frontend is down`, `3 log matches in 24h`.

### Details that matter at 3am

- **Live without reloads.** Every panel re-fetches itself and swaps in place: every 10 seconds for live numbers, every 15–30 seconds for history lists. A chip in the header shows the age of the data, pauses polling when you click it, and says `lost connection to server` when requests start failing.
- **Only real changes flash.** A panel that swapped identical content stays still, so a blinking screen always means something actually changed.
- **Works offline.** htmx, Alpine, Chart.js and both fonts are vendored into the binary. The dashboard opens on a host with no internet.
- **Dark and light.** The theme is resolved by an inline script before first paint, so the page never flashes the wrong palette.
- **Honest failure.** When the collector is unreachable, pages show a waiting screen that checks back every 5 seconds and reloads itself once the collector answers. Panels already on screen keep the last live data instead of blanking out.

## Getting started

### 1. Start the collector

```bash
cd argus-backend
cp .example.env .env      # Postgres credentials, BOT_TOKEN and CHAT_ID for Telegram
docker compose up -d      # Postgres 16 + goose migrations + collector on :8099
```

The collector needs `/var/run/docker.sock` mounted to watch containers and read their logs — the compose file does this. Without `BOT_TOKEN` and `CHAT_ID` the alerter doesn't start: everything is still recorded, and alerts stay `not sent`. Swagger UI for the collector's API is at `http://localhost:8099/swagger/index.html`.

### 2. Start the dashboard

```bash
cd argus-frontend
export ARGUS_BACKEND_URL=http://localhost:8099
go run ./cmd/web -config config.yaml    # dashboard on :8088
```

### 3. Tell Argus what to watch

Label the containers you care about:

```yaml
services:
  web:
    image: my-app
    labels:
      argus.monitor: "true"                        # watch this container's lifecycle
      argus.probe.url: "http://web:8080/health"    # and health-check this URL
```

| Label | Effect |
| --- | --- |
| `argus.monitor: "true"` | Registers the container. Its `die` / `oom` / `kill` / `restart` / `start` / `destroy` events are recorded from then on. |
| `argus.probe.url: <url>` | Also registers an HTTP probe target, checked every 60 seconds. Optional. |

The watcher picks up labelled containers as they start — no restart of Argus needed.

An uptime target that isn't a container has no label to hang off, so it's created through the collector's API. The dashboard's *New target* button shows the same snippet:

```bash
curl -X POST http://localhost:8099/v1/api/targets \
  -H 'Content-Type: application/json' \
  -d '{"name":"my-app","source":"https://myapp.local/health","target_type":"probe"}'
```

## Alerts

![Alert history](docs/media/alerts.png)

Alerts are raised by the collector and delivered by the Telegram bot. Put the bot's token in `BOT_TOKEN` and send it `/start`: it replies with the chat id that goes into `CHAT_ID`.

| What happened | Severity | Alert text |
| --- | --- | --- |
| Container ran out of memory | `critical` | `Container was killed after running out of memory (image: …)` |
| Container exited 5 times within 5 minutes | `critical` | `Container is crash-looping: 5 restarts within 5m0s` |
| Container exited with a non-zero code | `error` | `Container exited unexpectedly with code 137 (image: …)` |
| Health check failed twice in a row | `error` | the reason, see below |
| A down target answers again | `info` | `Service is back up` |
| A log line matched a rule | the rule's | `Log line matched rule "panic\|FATAL"` |
| Clean exit (code 0), `kill`, `restart`, `start`, `destroy` | — | no alert, only an event on the container's page |

Health check failures come with a readable reason rather than a raw Go error: `Health check timed out`, `Health check returned HTTP 502`, `Host web-frontend could not be resolved`, `Service is unreachable: connection refused`, `TLS certificate verification failed`.

Every alert goes to Telegram, whatever its severity. The alerter checks for new ones every 5 seconds and sends one message per target, headed by the worst severity in it. The same alert for the same target isn't repeated within `telegram.cooldown` (5 minutes): it's counted into the next message as `+N suppressed` instead.

Each row in the history shows its delivery state: `sent to Telegram` once the alerter has handled it, `not sent` while it's waiting or when Telegram isn't configured.

## Configuration

**Collector** — `argus-backend/config.yaml`:

| Key | Default | Meaning |
| --- | --- | --- |
| `http_server.addr` | `0.0.0.0:8099` | where the REST API listens |
| `prober.probe_interval` | `60s` | how often probe targets are checked |
| `prober.probe_timeout` | `5s` | per-check timeout |
| `prober.failure_threshold` | `2` | failed checks in a row before a target is down |
| `watcher.crash_loop_restarts` | `5` | exits that count as a crash loop… |
| `watcher.crash_loop_window` | `5m` | …within this window |
| `matcher.rules` | — | log rules: `pattern` (regexp), `severity`, `container` (name or `*`) |
| `retention.keep` | `336h` | how long probes, events and log matches are kept (14 days) |
| `retention.interval` | `24h` | how often old rows are deleted |
| `telegram.notify_interval` | `5s` | how often pending alerts are pushed |
| `telegram.cooldown` | `5m` | how long the same alert isn't repeated |

Secrets come from the environment or `.env`: `POSTGRES_URL`, `BOT_TOKEN`, `CHAT_ID`, and `CONFIG_PATH` for the config file itself.

**Dashboard** — `argus-frontend/config.yaml`:

| Key | Default | Meaning |
| --- | --- | --- |
| `backend.base_url` | — | collector address, required |
| `backend.timeout_seconds` | `5` | per-request timeout to the collector |
| `web.listen_addr` | `:8080` | where the dashboard listens (this repo's config sets `:8088`) |
| `web.template_dir` | empty | set to `./web` to load templates and CSS from disk during development |
| `web.retention_days` | `14` | shown on the History page; set it to match the collector's `retention.keep` |

## Deliberately not in this version

Named here because a monitoring tool that hides its blind spots is the problem it claims to solve:

- **No CPU, RAM or disk**, for the host or for containers. Nothing collects them, so the dashboard shows a note instead of an empty gauge.
- **No alert lifecycle.** There is no acknowledge or resolve — only "sent to Telegram". The screen says exactly that.
- **No latency for containers.** Nothing pings a container, so its page shows Docker event activity instead of a made-up response time.
- **Alerts are never pruned.** Retention deletes probes, events and log matches; alert history keeps growing until you delete rows through the API.
- **No way to add targets from the dashboard.** It stays read-only: containers register themselves by label, and standalone uptime targets are created through the collector's API.
- **One host.** No clustering, no federation, no remote agents.

## Under the hood

Two Go modules, clean architecture in both, no framework in either.

```
argus-backend/     collector: prober, watcher, matcher, alerter, cleaner, REST API
  internal/prober … retention   the background workers
  internal/domain/entity        domain types
  internal/usecase              business logic, repository ports
  internal/storage              PostgreSQL (pgx)
  internal/transport/http       chi router, handlers, swagger
  migrations/                   goose

argus-frontend/    dashboard: read-only
  internal/domain/entity        domain types, no dependencies
  internal/usecase              aggregation: uptime, outages, p95
  internal/gateway/backend      HTTP client to the collector
  internal/transport/http       router, handlers, presenter, templates
  web/                          templates and static assets, go:embed
```

Ports are declared by the consumer, so dependencies point inward: the aggregation layer knows nothing about HTTP, and the domain layer knows nothing about HTTP or HTML. The dashboard connects over the API only — it has no database driver at all. Its only dependencies load the config file and `.env`; everything else is the standard library.

Stack: Go, PostgreSQL 16, htmx, Alpine.js, Chart.js, raw CSS. No SPA framework, no build pipeline, no CDN.
