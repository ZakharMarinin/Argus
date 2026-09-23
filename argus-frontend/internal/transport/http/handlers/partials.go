package handlers

import (
	"context"
	"errors"
	"net/http"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/transport/http/view"
)

func (h *Handler) overviewFor(ctx context.Context) (view.Overview, error) {
	snap, err := h.snapshot(ctx)
	if err != nil {
		return view.Overview{}, err
	}

	return h.present.Overview(snap.targets, snap.counts, snap.logs), nil
}

func (h *Handler) detailFor(ctx context.Context, name string) (view.Detail, error) {
	detail, err := h.uc.ContainerDetail(ctx, name)
	if err != nil {
		return view.Detail{}, err
	}

	return h.present.Detail(detail), nil
}

// PartialSummary - плашка общего состояния вместе с маскотом.
func (h *Handler) PartialSummary(w http.ResponseWriter, r *http.Request) {
	h.overviewPartial(w, r, "summary", func(o view.Overview) any { return o.Health })
}

func (h *Handler) PartialLogs(w http.ResponseWriter, r *http.Request) {
	h.overviewPartial(w, r, "logs-card", func(o view.Overview) any { return o.Logs })
}

// PartialTargets - список uptime-таргетов.
func (h *Handler) PartialTargets(w http.ResponseWriter, r *http.Request) {
	h.overviewPartial(w, r, "targets", func(o view.Overview) any { return o.Targets })
}

// PartialContainers - список контейнеров.
func (h *Handler) PartialContainers(w http.ResponseWriter, r *http.Request) {
	h.overviewPartial(w, r, "containers", func(o view.Overview) any { return o.Containers })
}

// overviewPartial - один и тот же путь для всех партиалов сводки: снимок,
// ошибка без тела, рендер выбранного куска.
func (h *Handler) overviewPartial(w http.ResponseWriter, r *http.Request, name string, pick func(view.Overview) any) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	overview, err := h.overviewFor(ctx)
	if err != nil {
		h.partialError(w, r, err)

		return
	}

	h.partial(w, name, pick(overview))
}

func (h *Handler) PartialRecent(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	recent, err := h.uc.Recent(ctx)
	if err != nil {
		h.partialError(w, r, err)

		return
	}

	h.partial(w, "recent", h.present.Recent(recent))
}

func (h *Handler) PartialHistory(w http.ResponseWriter, r *http.Request) {
	tab := r.PathValue("tab")
	if !view.HistoryTab(tab) {
		http.NotFound(w, r)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	q := r.URL.Query()
	page := view.HistoryPage{Tab: tab}

	err := h.fillHistory(ctx, &page, q)
	if err != nil {
		h.partialError(w, r, err)

		return
	}

	switch tab {
	case "alerts":
		h.partial(w, "history-alerts", page.Alerts)
	case "logs":
		h.partial(w, "log-feed", page.Logs)
	default:
		h.partial(w, "history-outages", page.Outages)
	}
}

// PartialTimeline - полоса простоев за сутки на странице контейнера.
func (h *Handler) PartialTimeline(w http.ResponseWriter, r *http.Request) {
	h.detailPartial(w, r, "timeline", func(d view.Detail) any { return d.Timeline })
}

// PartialMeta - плитки метаданных на странице контейнера.
func (h *Handler) PartialMeta(w http.ResponseWriter, r *http.Request) {
	h.detailPartial(w, r, "detail-meta", func(d view.Detail) any { return d.Meta })
}

// PartialEvents - лента событий Docker на странице контейнера.
func (h *Handler) PartialEvents(w http.ResponseWriter, r *http.Request) {
	h.detailPartial(w, r, "events", func(d view.Detail) any { return d.Events })
}

func (h *Handler) PartialContainerLogs(w http.ResponseWriter, r *http.Request) {
	h.detailPartial(w, r, "log-feed", func(d view.Detail) any { return d.Logs })
}

func (h *Handler) detailPartial(w http.ResponseWriter, r *http.Request, name string, pick func(view.Detail) any) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	detail, err := h.detailFor(ctx, r.PathValue("name"))
	if err != nil {
		h.partialError(w, r, err)

		return
	}

	h.partial(w, name, pick(detail))
}

func (h *Handler) PartialHost(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	host, err := h.uc.Host(ctx)
	if errors.Is(err, entity.ErrNotReady) {
		h.partialError(w, r, err)

		return
	}

	h.partial(w, "host-panel", h.present.HostPanel(host, err))
}
