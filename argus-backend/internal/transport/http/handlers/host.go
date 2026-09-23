package handlers

import (
	"net/http"

	"github.com/go-chi/render"
)

// GetHost godoc
// @Summary      Ресурсы хоста
// @Description  Последний замер CPU, RAM и корневого раздела. Метрика без значения - null; до первого замера sampled_at - null
// @Tags         host
// @Produce      json
// @Success      200  {object} entity.HostSnapshot
// @Router       /host [get].
func (h *Handler) GetHost(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, h.host.Snapshot())
}
