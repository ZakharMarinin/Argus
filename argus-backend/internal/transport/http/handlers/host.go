package handlers

import (
	"net/http"

	"github.com/go-chi/render"
)

// GetHost godoc
// @Summary      Host resources
// @Description  Latest sample of CPU, RAM and the root partition. A metric without a value is null; before the first sample sampled_at is null
// @Tags         host
// @Produce      json
// @Success      200  {object} entity.HostSnapshot
// @Router       /host [get].
func (h *Handler) GetHost(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, h.host.Snapshot())
}

func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
