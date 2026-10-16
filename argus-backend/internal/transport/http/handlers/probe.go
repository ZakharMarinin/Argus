package handlers

import (
	"argus-collector/internal/domain/entity"
	"argus-collector/response"
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
)

// GetListProbes godoc
// @Summary      List probes
// @Description  Returns probes filtered by target/result, or paged
// @Tags         probes
// @Produce      json
// @Param        target_id query    string false "Target ID"
// @Param        success   query    bool   false "Whether the probe succeeded"
// @Param        limit     query    int    false "Limit"
// @Param        offset    query    int    false "Offset"
// @Success      200  {array}  entity.Probe
// @Failure      400  {object} response.ErrorResponse
// @Failure      500  {object} response.ErrorResponse
// @Router       /probes [get].
func (h *Handler) GetListProbes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	q := r.URL.Query()

	var (
		probes []*entity.Probe
		err    error
	)

	switch {
	case q.Get("target_id") != "":
		var targetID uuid.UUID

		targetID, err = uuid.Parse(q.Get("target_id"))
		if err != nil {
			response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

			return
		}

		probes, err = h.uc.GetProbesByTarget(ctx, targetID)
	case q.Get("success") != "":
		var success bool

		success, err = strconv.ParseBool(q.Get("success"))
		if err != nil {
			response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

			return
		}

		probes, err = h.uc.GetProbesBySuccess(ctx, success)
	default:
		limit, offset := parsePagination(r)
		probes, err = h.uc.GetAllProbes(ctx, limit, offset)
	}

	if err != nil {
		response.HandleError(w, r, h.log, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, probes)
}

// GetProbe godoc
// @Summary      Get a probe
// @Description  Returns a probe by ID
// @Tags         probes
// @Produce      json
// @Param        id   path      int true "Probe ID"
// @Success      200  {object} entity.Probe
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /probes/{id} [get].
func (h *Handler) GetProbe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	probe, err := h.uc.GetProbe(ctx, id)
	if err != nil {
		response.HandleError(w, r, h.log, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, probe)
}

// DeleteProbe godoc
// @Summary      Delete a probe
// @Description  Deletes a probe by ID
// @Tags         probes
// @Param        id   path      int true "Probe ID"
// @Success      204  "No Content"
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /probes/{id} [delete].
func (h *Handler) DeleteProbe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	_, err = h.uc.DeleteProbe(ctx, id)
	if err != nil {
		response.HandleError(w, r, h.log, err)

		return
	}

	render.NoContent(w, r)
}
