# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Go html/template server-rendered HTML, htmx for partial-swap polling, Alpine.js for local-only interactions (theme toggle, sidebar collapse, mood preview, modal, polling pause), hand-written vanilla CSS. No SPA framework, no build pipeline or bundler, no Tailwind. All JS/CSS/fonts/images vendored under web/static and embedded into the Go binary (go:embed) so the dashboard runs on a host with no internet access.

## Users

A single technical/ops person self-hosting Argus to watch one host: its Docker containers and a handful of HTTP uptime targets. They check the dashboard when something might be wrong, or glance at it ambiently; they are comfortable with raw technical detail (status codes, container names, event types) and distrust dashboards that paper over missing data with fake numbers.

## Product Purpose

Argus is a self-hosted monitoring dashboard for one host. A separate collector process (argus-backend) polls HTTP uptime targets, watches Docker container lifecycle events, and raises alerts (relayed to Telegram); this dashboard (argus-frontend) is the read-only web UI over that data. Success is a truthful, fast-scanning picture of "is anything wrong right now" plus enough history to diagnose it without leaving the browser.

## Positioning

Not a hosted SaaS monitoring product: no accounts, no multi-tenant fleet view, no vendor lock-in. One binary, one host, data owned entirely by the operator, dashboard works with zero internet connectivity. Distinguishing trait versus generic admin templates: it never fabricates a metric it cannot back with real data (a missing value renders as an honest empty state, never a plausible-looking placeholder).

## Operating Context

- Two independent Go processes share only an HTTP API: argus-backend (collector: Docker watcher, HTTP prober, alerting, owns all writes) and argus-frontend (this dashboard: read-only, calls argus-backend's chi-routed JSON API under /v1/api).
- argus-backend exposes: `/v1/api/targets` (CRUD + status patch), `/v1/api/probes` (read/delete, written internally by the prober), `/v1/api/events` (read/delete, written internally by the watcher), `/v1/api/alerts` (CRUD). No `/log-matches` surface exists yet.
- A `Target.Status` field exists but is only ever set to `"unknown"` at creation; nothing updates it afterward. The dashboard must derive live up/down state itself from recent probe results (for uptime targets) and recent container events (for containers), not trust the stored field.
- Backend has no aggregate/time-window query endpoints (no "last 24h" filter, no percentile endpoint) - only raw lists with limit/offset and a few equality filters (target_id, success, severity, notified, type). Any 24h/hourly aggregate the dashboard shows must be computed client-side (in Go) from a bounded raw fetch.
- Container "uptime since start" and "log matches, 24h" have no backing data at all in the new backend (no stored container-start event, no log-match HTTP surface) and are dropped from the redesign rather than shown as fake or permanently-empty UI.
- Runs via docker-compose alongside argus-backend and Postgres (owned entirely by argus-backend); this process holds no DB connection of its own after the redesign.

## Capabilities and Constraints

- Read-only: never writes to argus-backend beyond what's needed to render (i.e., effectively no writes at all from this process).
- Must degrade to a clear waiting/empty state when argus-backend is unreachable or returns nothing yet, not a 500.
- Must support light/dark theme resolved before first paint via the existing inline-script + `data-theme` attribute mechanism (no FOUC, no `prefers-color-scheme` media-query duplication of the palette).
- Partial htmx endpoints must keep re-emitting their own `hx-get`/`hx-trigger` so polling survives a swap.
- Any per-container or per-target chart/history that would require an aggregate backend endpoint that doesn't exist is either computed from a bounded raw fetch or honestly omitted, never faked.
- Open/undecided: exact fetch limits and polling intervals for the new HTTP-backed data layer are an implementation detail of the parallel backend-integration work, not fixed here.

## Brand Commitments

- Name: Argus (many-eyed watcher; the pixel-art logo and mascot lean into "watching").
- Signature identity element, must survive the redesign unchanged in concept: an animated pixel-art mascot with mood states (happy, watching, scared, angry, sleeping - one GIF per mood at web/static/img/argus-*.gif) that reflects overall system health at a glance. This is the one piece of personality the brand is not willing to lose.
- Voice: dashboard UI copy is plain, technical, unembellished English; no marketing tone.

## Evidence on Hand

- Existing implementation: argus-frontend/web/templates/**, web/static/css/argus.css, web/static/js/app.js, web/view.go, web/present.go - a working htmx+Alpine dashboard (overview with health/mood/latency cards, uptime-target list, container list; filterable alert history; per-container detail page) currently read from Postgres directly. This is being replaced as visual evidence/anti-reference for the redesign per the user's request, while its architecture (Server -> Presenter -> view models -> templates, `store.Reader` interface boundary) is being kept and re-implemented against HTTP instead of Postgres in parallel.
- Backend API contract: argus-backend/internal/transport/http/handlers/*.go, argus-backend/internal/domain/entity/entity.go (ground truth for what data will actually be available to render).
- Mascot assets already exist and are final: web/static/img/argus-{happy,watching,scared,angry,sleeping}.gif, argus-logo-128.png.
- No user research, analytics, or real usage data exists beyond the operator's own use.

## Product Principles

- Never render a fabricated or guessed number. A value with no real backing data is an explicit empty state, not a zero, dash-that-looks-like-a-zero, or plausible placeholder.
- The mascot's mood is always a direct, explainable function of real system state, never decorative.
- One signal, one place: don't duplicate "is this thing healthy" logic in multiple components that could drift (status color, mascot mood, and nav badges all derive from the same computed state).
- Offline-first and dependency-light: no CDN links, no build step, no JS framework beyond htmx + Alpine; everything ships inside the Go binary.
- Prefer showing less real data over showing more invented data when the backend's capabilities fall short of the old design's ambitions.

## Accessibility & Inclusion

No product-specific accessibility requirement beyond standard web accessibility (keyboard nav, contrast, focus states) - carried over as a baseline from the existing implementation, which already has a skip link, aria attributes on interactive controls, and theme-aware contrast.
