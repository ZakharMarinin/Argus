package handlers

import (
	"context"
	"errors"
	"net/http"

	"argus-frontend/internal/domain/entity"
)

// Chart points are returned as JSON, not HTML: the chart card doesn't take
// part in htmx polling, otherwise Chart.js would be recreated every 10
// seconds along with its animation and tooltips.

// LatencyChart serves hourly p95 and availability across all uptime targets.
func (h *Handler) LatencyChart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	points, err := h.uc.LatencySeries(ctx)
	if err != nil {
		h.partialError(w, r, err)

		return
	}

	h.json(w, h.present.LatencyChart(points))
}

// ActivityChart serves a single container's hourly Docker events.
func (h *Handler) ActivityChart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	detail, err := h.uc.ContainerDetail(ctx, r.PathValue("name"))
	if errors.Is(err, entity.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		h.partialError(w, r, err)

		return
	}

	h.json(w, h.present.ActivityChart(detail.Activity))
}
