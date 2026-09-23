package handlers

import (
	"context"
	"errors"
	"net/http"

	"argus-frontend/internal/domain/entity"
)

// Точки графиков отдаются JSON'ом, а не HTML: карточка с графиком не
// участвует в htmx-поллинге, иначе Chart.js пересоздавался бы каждые 10
// секунд вместе с анимацией и подсказками.

// LatencyChart - почасовые p95 и доступность по всем uptime-таргетам.
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

// ActivityChart - почасовые события Docker одного контейнера.
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
