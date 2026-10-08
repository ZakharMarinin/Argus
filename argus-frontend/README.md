# argus-frontend

The Argus web dashboard: Go templates, raw CSS, htmx and Alpine.js. No SPA
framework and no build pipeline.

The dashboard talks only to the `argus-backend` HTTP API: it reads everything
it shows, and the only thing it writes is creating and deleting uptime
targets. It has no state or database of its own, so it can be stopped,
rebuilt and restarted independently of the `collector`.

## Running

```bash
cp config.example.yaml config.yaml
echo 'ARGUS_BACKEND_URL=http://localhost:8099' > .env

go run ./cmd/web     # default config is config.yaml; use -config path for another one
```

`.env` in the current working directory is read on startup, but it doesn't
override environment variables that are already set. The only setting that can
come from the environment is the backend address: `ARGUS_BACKEND_URL` overrides
`backend.base_url` from `config.yaml`. Everything else is read from the config
file. The backend address is the only required value, set either way.

The dashboard starts even if argus-backend is unavailable at the moment: it
shows a waiting screen and comes back to life on its own once the backend
answers.

For development, set `web.template_dir: "./web"`: templates and static files
are then read from disk, and CSS changes show up after a plain F5. In
production the field is empty, and everything comes from `go:embed`.

## Backend for development

The dashboard needs a live argus-backend: it has no database of its own and
reads everything from the collector's `/v1/api`. The simplest setup is to start
the collector with Postgres from the repository root and run the dashboard
itself with `go run`:

```bash
# from the repository root; argus-backend/.env and argus-backend/config.yaml
# prepared as described in argus-backend/README.md
docker compose up -d --build argus     # Postgres + collector on 127.0.0.1:8099

cd argus-frontend
go run ./cmd/web                       # ARGUS_BACKEND_URL=http://localhost:8099 in .env
```

The `argus-web` container from the same compose file listens on the same
`:8088`, so don't start it during development (or stop it with
`docker compose stop argus-web`).

Checking the edge states:

- **waiting screen** - `docker compose stop argus`: the dashboard shows the
  waiting screen and comes back on its own after `docker compose start argus`;
- **empty states** - a fresh database with no targets and no labelled
  containers;
- **data** - add an uptime target through "New target" and put the label on
  any container (the same modal explains how).

## Tests

```bash
go test ./...            # config, rendering, empty states, routes
go test ./internal/usecase/...   # aggregates on top of an httptest server for /v1/api
```

The tests need neither Postgres nor a live argus-backend.

## Structure

```
cmd/web/main.go                     # flags, logger, signals, graceful shutdown
internal/
├── app/                            # App (server, shutdown) + dependency wiring
├── config/config.go                # YAML + ${ENV} + .env, validated on startup
├── domain/entity/                  # domain types, ErrNotReady / ErrNotFound / ErrBackendTooOld
├── usecase/                        # aggregates: uptime, outages, p95, restarts; Backend port
├── gateway/backend/                # HTTP client for argus-backend (/v1/api): DTO → entity
└── transport/http/
    ├── router/                     # routes and static file serving
    ├── handlers/                   # pages, partials, JSON for charts, targets
    └── view/                       # Presenter, view models, template rendering
web/
├── embed.go                        # go:embed for templates and static files
├── templates/                      # layout, pages and partials for hx-get
└── static/                         # CSS, vendored htmx/Alpine/Chart.js, fonts, GIFs
```

Dependencies point inward: `usecase` knows nothing about HTTP, `entity` knows
nothing about HTTP or HTML, and the backend's JSON never leaves
`internal/gateway/backend`.

## Routes

| Route | Serves |
|---|---|
| `GET /` | overview: "is something broken?" at a glance |
| `GET /history/alerts?type=&sev=&page=` | alert history |
| `GET /history/logs?target=&sev=&page=` | log match history + chart for the last 24 hours |
| `GET /history/outages?page=` | outages over the last 24 hours |
| `GET /alerts` | 301 to `/history/alerts` (old bookmarks) |
| `GET /containers/{name}` | container details |
| `GET /partials/summary` | overall state + mascot with the reason |
| `GET /partials/logs` | card with matches over the last 24 hours, with hourly bars |
| `GET /partials/targets` | list of uptime targets |
| `GET /partials/containers` | container grid, problem containers first |
| `GET /partials/host` | host CPU, RAM and disk for the sidebar (latest sample from `/v1/api/host`) |
| `GET /partials/recent` | 8 most recent events: outages, alerts and matches of warning and above |
| `GET /partials/history/{alerts,logs,outages}` | history tab body with filters and page |
| `GET /partials/container/{name}/meta` | state tiles |
| `GET /partials/container/{name}/events` | Docker events |
| `GET /partials/container/{name}/timeline` | outage bar for the last 24 hours |
| `GET /partials/container/{name}/logs` | the container's 20 most recent matches |
| `GET /api/latency` | overview chart points, JSON |
| `GET /api/log-matches?target=` | matches by hour and severity, JSON |
| `GET /api/container/{name}/activity` | the container's Docker events and matches by hour, JSON |
| `GET /healthz` | 200, only if argus-backend answers |
| `GET /static/…` | embedded static files |
| `POST /targets` | create an uptime target: name and URL go to `POST /v1/api/targets` |
| `DELETE /targets/{id}` | delete a target through `/v1/api` |

There are exactly two write endpoints, and both are only about uptime targets.
Everything else is read-only.

## How it works

**Partials replace themselves.** Each one renders its own root element
together with its `hx-get`/`hx-trigger` and is swapped via `outerHTML`. A page
just calls `{{template "targets" .Targets}}`, and after the swap the partial
keeps polling the same address, including the current history filters.

**The overview is triage; history lives under `/history`.** The overview has
only what answers the question "is something broken?": the state with the
mascot, latency, the log card, targets, containers and the 8 most recent
notable events. Everything long (alerts, matches, outages) is paged on the
history tabs, 50 rows per page. Only the first page polls: on older pages rows
would slide away under the cursor, and the chip in the header honestly says
"not auto-refreshing" there.

**The chart lives apart from polling.** htmx doesn't touch the chart card:
otherwise the `canvas` would be recreated every 10 seconds, killing the
animation and the tooltips. The points arrive in a separate JSON request once a
minute.

**Statuses go through a CSS variable.** The class `is-up`/`is-warn`/`is-down`/`is-idle`
sets `--s`, and the dot, the pill, the bar and the highlighted metric read it
without knowing which status they carry. There are no colours in the
templates. On dark panels (`.card--dark`, `.ctn-card--dark`, `.alert-row--dark`)
the same `--s` is mixed with white via `color-mix`, otherwise the status
colour loses contrast.

**Alpine handles only local interactions**: pausing auto-refresh, the
freshness counter, the modal, collapsing the sidebar and the theme switcher.
Data is brought in by htmx; there is no application state in JS.

**There is one mascot for the whole dashboard**, in the state card and in the
sidebar. Its mood is the most alarming of the signals: a down target or a
critical alert → angry, warnings → scared, log matches over 24 hours by
thresholds 0 / 1-2 / 3-4 / 5+ → happy / watching / scared / angry
(`view.LogMood`). The line under it names exactly the signal that won
(`TestMoodTakesWorstSignal`). What each mood means is in the legend that opens
when you click the strip at the bottom of the sidebar.

**History requires an up-to-date argus-backend.** The dashboard requests
`/v1/api/alerts` and `/v1/api/log-matches` with `order=desc`, `skip`, `since`
and reads the total count from `X-Total-Count`. An old backend silently
ignores these parameters and would return the oldest records as the newest, so
such a response is an "argus-backend is outdated" error (500 and a log line),
not a quiet lie.

## Theme

By default the dashboard follows the system theme. The button in the sidebar
header cycles through: **system → light → dark**. The choice is saved to
`localStorage` and survives a reload.

It is implemented with a `data-theme` attribute on `<html>` and the same design
tokens that were already in use. A small synchronous script in `<head>`
resolves the system theme to a concrete value: it sets the attribute before the
first paint, otherwise the page would flash light. Thanks to this, the dark
palette is described in CSS exactly once, with no duplicate under
`prefers-color-scheme`.

## What the dashboard shows and what it doesn't

There is one rule: the screen shows only what the collector returned. Where
there is no value, there is a dash or an empty state. A monitor that fills in
a plausible number in place of a missing one is worse than one that doesn't
work.

- **Empty database**: the overview says that argus-backend hasn't recorded a
  single target yet, every block shows its empty state, and the chart is
  replaced by "No data for the last 24 hours".
- **An hour without checks** arrives in the chart as `null` and is drawn as a
  gap, not as zero latency.
- **A target with no checks at all** shows a dash, not "100% uptime".
- **argus-backend isn't answering or isn't ready yet**: a waiting screen and
  503, not 500. The collector and the dashboard can start in any order, so
  this is a normal state. The page comes back on its own, without a manual
  reload.
- **Connection lost mid-session**: partials answer 503 with no body, htmx keeps
  the last live data, and after half a minute the chip in the header says
  "lost connection to server".

What isn't in the collector's data, and therefore isn't on the screen:

| Missing | What's shown instead |
|---|---|
| Container CPU/RAM/disk | the cards show failures over 15 minutes and matches over 24 hours; resources exist only for the host, in the sidebar (latest sample from `/v1/api/host`) |
| Container image | the line isn't shown while there is no value |
| Alert type | derived from the kind of target: `uptime` or `container` |
| "Active/closed" alert status | the only real indicator is whether it was sent to Telegram (`sent_to_tg`) |
| Container latency | nothing pings it: its page shows a chart of log matches and Docker events instead |
| A "degraded" threshold | targets have only `up`/`down`/`unknown`; the yellow status is set from facts: for a target, at least one failed check in 24 hours; for a container, a failure (non-zero exit or OOM) in the last 15 minutes. Log matches affect only the card order and the mascot |

## Design notes

**Fonts.** Two families: Inter for headings, large numbers and body text,
JetBrains Mono for data, logs and code. A separate display font for headings
only added noise at this screen density. Both are vendored with their Latin and
Cyrillic subsets (`web/static/fonts/`).

**"New target" is a form for an uptime target.** The name and URL go to the
collector's `POST /v1/api/targets`; below it, the same modal explains how to
connect a container with a label. Deleting is the × in the target's row.

## Working offline

htmx, Alpine, Chart.js and all fonts live in `web/static/` and are embedded in
the binary. There are no external links in the markup, which is checked by the
`TestStaticAssetsEmbedded` test.

| File | Version |
|---|---|
| htmx | 2.0.4 |
| Alpine.js | 3.14.8 |
| Chart.js | 4.4.1 |
