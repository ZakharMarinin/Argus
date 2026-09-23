package handlers

import (
	"context"
	"errors"
	"net/http"

	"argus-frontend/internal/domain/entity"
)

func (h *Handler) LogMatchChart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	report, err := h.uc.LogMatches(ctx, r.URL.Query().Get("target"))
	if errors.Is(err, entity.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		h.partialError(w, r, err)

		return
	}

	h.json(w, h.present.LogMatchChart(report.Hourly))
}
