// Package handlers is the dashboard's HTTP layer: pages, htmx partials and
// chart points.
//
// It works through the UseCase port and returns ready markup. There is no
// Docker and no Telegram here.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/transport/http/view"
)

// queryTimeout limits a trip to the backend. The dashboard is polled on a timer,
// and a hanging request would pile up connections instead of honestly failing.
const queryTimeout = 5 * time.Second

// UseCase is everything the dashboard reads about the monitoring state.
//
// Spec §6.2 defines three methods; there are more here. History (alerts, matches,
// outages) is paged and lives under /history, the overview takes only 24 hours;
// outage intervals are derived on read, the schema doesn't have them.
type UseCase interface {
	TargetStatus(ctx context.Context) ([]entity.TargetSummary, error)
	AlertCounts(ctx context.Context) (entity.AlertCounts, error)
	ContainerDetail(ctx context.Context, name string) (entity.ContainerDetail, error)
	LatencySeries(ctx context.Context) ([]entity.LatencyPoint, error)
	Incidents(ctx context.Context) ([]entity.Incident, entity.IncidentStats, error)
	LogMatches(ctx context.Context, target string) (entity.LogMatchReport, error)
	HistoryAlerts(ctx context.Context, q entity.HistoryQuery) (entity.Page[entity.Alert], error)
	HistoryLogMatches(ctx context.Context, q entity.HistoryQuery) (entity.Page[entity.LogMatch], error)
	Recent(ctx context.Context) (entity.Recent, error)
	Host(ctx context.Context) (entity.Host, error)
	CreateProbe(ctx context.Context, name, source string) error
	DeleteTarget(ctx context.Context, id string) error
	Ping(ctx context.Context) error
}

// Handler holds the dependencies of all dashboard endpoints.
type Handler struct {
	uc      UseCase
	tpl     *view.Templates
	present view.Presenter
	log     *slog.Logger
}

// NewHandler builds the handlers.
func NewHandler(log *slog.Logger, uc UseCase, tpl *view.Templates, present view.Presenter) *Handler {
	return &Handler{uc: uc, tpl: tpl, present: present, log: log}
}

// Templates returns the template set; the router needs it to serve static files.
func (h *Handler) Templates() *view.Templates { return h.tpl }

// ---------------------------------------------------------------------------
// State snapshot
// ---------------------------------------------------------------------------

// snapshot is what every page needs: the list of targets and the alert counters.
type snapshot struct {
	targets []entity.TargetSummary
	counts  entity.AlertCounts
	logs    entity.LogMatchReport
	host    view.HostPanel
}

func (h *Handler) snapshot(ctx context.Context) (snapshot, error) {
	targets, err := h.uc.TargetStatus(ctx)
	if err != nil {
		return snapshot{}, err
	}

	counts, err := h.uc.AlertCounts(ctx)
	if err != nil {
		return snapshot{}, err
	}

	logs, err := h.uc.LogMatches(ctx, "")
	if err != nil {
		return snapshot{}, err
	}

	host, err := h.uc.Host(ctx)

	return snapshot{targets: targets, counts: counts, logs: logs, host: h.present.HostPanel(host, err)}, nil
}

// layout builds the page scaffolding around a ready content model.
func (h *Handler) layout(snap snapshot, active, title, sub, partial string, content any) view.Layout {
	return view.Layout{
		Title:       title,
		Sub:         sub,
		Active:      active,
		PartialPath: partial,
		Nav:         h.present.Nav(snap.targets, snap.counts),
		Sidebar:     h.present.Sidebar(snap.targets, snap.counts, len(snap.logs.Matches), snap.host),
		Content:     content,
	}
}

// ---------------------------------------------------------------------------
// Errors and rendering
// ---------------------------------------------------------------------------

// pageError responds to an error reading from the backend.
//
// "Backend not ready yet" and "backend unavailable" are normal states on first
// start or restart: we show the waiting screen, not a 500. Everything else is a
// real error, and it must not be hidden.
func (h *Handler) pageError(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, entity.ErrNotReady) {
		h.log.Error("backend request", "path", r.URL.Path, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	h.log.Warn("data not ready yet", "path", r.URL.Path, "err", err)
	w.WriteHeader(http.StatusServiceUnavailable)
	h.page(w, "waiting", h.present.WaitingLayout())
}

// partialError responds to an error inside a partial.
//
// There is no body on purpose: htmx doesn't swap content on a non-2xx, so the
// last known data stays on screen, and after half a minute the chip in the header
// says "lost connection to server" by itself. That's more honest than slipping
// in an empty block instead of live data.
func (h *Handler) partialError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, entity.ErrNotReady) {
		h.log.Warn("partial: no data", "path", r.URL.Path, "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}

	h.log.Error("partial", "path", r.URL.Path, "err", err)
	w.WriteHeader(http.StatusInternalServerError)
}

func (h *Handler) page(w http.ResponseWriter, name string, data view.Layout) {
	err := h.tpl.Page(w, name, data)
	if err != nil {
		h.log.Error("page render", "page", name, "err", err)
	}
}

func (h *Handler) partial(w http.ResponseWriter, name string, data any) {
	err := h.tpl.Partial(w, name, data)
	if err != nil {
		h.log.Error("partial render", "partial", name, "err", err)
	}
}

func (h *Handler) json(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	err := json.NewEncoder(w).Encode(payload)
	if err != nil {
		h.log.Error("JSON encoding", "err", err)
	}
}

// Healthz answers 200 only if the backend responds.
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	err := h.uc.Ping(ctx)
	if err != nil {
		http.Error(w, "backend unavailable", http.StatusServiceUnavailable)

		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
