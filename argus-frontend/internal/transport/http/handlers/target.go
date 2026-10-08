package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/transport/http/view"
)

func (h *Handler) CreateTarget(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	form := view.TargetForm{
		Name: strings.TrimSpace(r.FormValue("name")),
		URL:  strings.TrimSpace(r.FormValue("url")),
	}

	err := h.uc.CreateProbe(ctx, form.Name, form.URL)

	var invalid *entity.ValidationError

	switch {
	case err == nil:
		form = view.TargetForm{}
		w.Header().Set("HX-Trigger", "refreshNow, target-created")
	case errors.As(err, &invalid):
		form.Error = invalid.Reason
	default:
		h.log.Error("creating target", "err", err)
		form.Error = "The collector is unavailable, try again in a moment"
	}

	h.partial(w, "target-form", form)
}

func (h *Handler) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	err := h.uc.DeleteTarget(ctx, r.PathValue("id"))
	if err != nil {
		h.partialError(w, r, err)

		return
	}

	w.Header().Set("HX-Trigger", "refreshNow")
}
