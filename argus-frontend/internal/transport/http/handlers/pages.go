package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/transport/http/view"
)

// Overview is the overview page: health, targets, containers, chart.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	snap, err := h.snapshot(ctx)
	if err != nil {
		h.pageError(w, r, err)

		return
	}

	overview := h.present.Overview(snap.targets, snap.counts, snap.logs)

	recent, err := h.uc.Recent(ctx)
	if err != nil {
		h.pageError(w, r, err)

		return
	}

	overview.Recent = h.present.Recent(recent)

	h.page(w, "overview", h.layout(snap, "overview", "Overview",
		h.present.OverviewSub(snap.targets), "/partials/summary", overview))
}

func (h *Handler) AlertsRedirect(w http.ResponseWriter, r *http.Request) {
	target := "/history/alerts"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	http.Redirect(w, r, target, http.StatusMovedPermanently) //nolint:gosec // G710: the path is fixed, only the query string comes from the request
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	tab := r.PathValue("tab")
	if !view.HistoryTab(tab) {
		http.NotFound(w, r)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	snap, err := h.snapshot(ctx)
	if err != nil {
		h.pageError(w, r, err)

		return
	}

	q := r.URL.Query()
	page := h.present.History(tab, containerNames(snap.targets), q)

	err = h.fillHistory(ctx, &page, q)
	if err != nil {
		h.pageError(w, r, err)

		return
	}

	h.page(w, "history", h.layout(snap, "history", "History",
		h.present.AlertsSub(), "/partials/history/"+tab, page))
}

func (h *Handler) fillHistory(ctx context.Context, page *view.HistoryPage, q url.Values) error {
	switch page.Tab {
	case "alerts":
		alerts, err := h.uc.HistoryAlerts(ctx, view.HistoryQueryFor(q))
		if err != nil {
			return err
		}

		page.Alerts = h.present.HistoryAlerts(alerts, q)
	case "logs":
		matches, err := h.uc.HistoryLogMatches(ctx, view.HistoryQueryFor(q))
		if err != nil {
			return err
		}

		page.Logs = h.present.HistoryLogs(matches, q)
	default:
		incidents, stats, err := h.uc.Incidents(ctx)
		if err != nil {
			return err
		}

		page.Outages = h.present.HistoryOutages(incidents, stats, q)
	}

	return nil
}

func containerNames(targets []entity.TargetSummary) []string {
	var names []string

	for _, t := range targets {
		if t.Kind == entity.KindContainer {
			names = append(names, t.Label)
		}
	}

	return names
}

// Container is the page of a single container.
func (h *Handler) Container(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	name := r.PathValue("name")

	snap, err := h.snapshot(ctx)
	if err != nil {
		h.pageError(w, r, err)

		return
	}

	detail, err := h.uc.ContainerDetail(ctx, name)
	if errors.Is(err, entity.ErrNotFound) {
		h.notFound(w, snap, name)

		return
	}

	if err != nil {
		h.pageError(w, r, err)

		return
	}

	h.page(w, "container", h.layout(snap, "container:"+name, name,
		"container being watched · events written by collector",
		"/partials/container/"+name+"/events", h.present.Detail(detail)))
}

func (h *Handler) notFound(w http.ResponseWriter, snap snapshot, name string) {
	w.WriteHeader(http.StatusNotFound)
	h.page(w, "waiting", h.layout(snap, "", "Nothing found", "check the container name",
		"—", h.present.ContainerNotFound(name)))
}
