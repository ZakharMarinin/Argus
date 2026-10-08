package handlers

import (
	"argus-collector/internal/domain/entity"
	"argus-collector/response"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
)

// CreateTarget godoc
// @Summary      Create a target
// @Description  Registers a new target for monitoring
// @Tags         targets
// @Accept       json
// @Produce      json
// @Param        request body entity.Target true "Target data"
// @Success      201  {object} entity.Target
// @Failure      400  {object} response.ErrorResponse
// @Router       /targets [post].
//
//nolint:dupl
func (h *Handler) CreateTarget(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

		return
	}
	defer func() { _ = r.Body.Close() }()

	var target entity.Target

	err = json.Unmarshal(body, &target)
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	created, err := h.uc.CreateTarget(ctx, &target)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	render.Status(r, http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, created)
}

// GetListTargets godoc
// @Summary      List targets
// @Description  Returns targets filtered by type/status, or paged
// @Tags         targets
// @Produce      json
// @Param        type   query    string false "Target type"
// @Param        status query    string false "Target status"
// @Param        limit  query    int    false "Limit"
// @Param        offset query    int    false "Offset"
// @Success      200  {array}  entity.Target
// @Failure      500  {object} response.ErrorResponse
// @Router       /targets [get].
func (h *Handler) GetListTargets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	q := r.URL.Query()

	var (
		targets []*entity.Target
		err     error
	)

	switch {
	case q.Get("type") != "":
		targets, err = h.uc.GetTargetsByType(ctx, q.Get("type"))
	case q.Get("status") != "":
		targets, err = h.uc.GetTargetsByStatus(ctx, q.Get("status"))
	default:
		limit, offset := parsePaginationUint64(r)
		targets, err = h.uc.GetAllTargets(ctx, limit, offset)
	}

	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, targets)
}

// GetTarget godoc
// @Summary      Get a target
// @Description  Returns a target by ID
// @Tags         targets
// @Produce      json
// @Param        id   path      string true "Target ID"
// @Success      200  {object} entity.Target
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /targets/{id} [get].
func (h *Handler) GetTarget(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	target, err := h.uc.GetTarget(ctx, id)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, target)
}

// DeleteTarget godoc
// @Summary      Delete a target
// @Description  Deletes a target by ID
// @Tags         targets
// @Param        id   path      string true "Target ID"
// @Success      204  "No Content"
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /targets/{id} [delete].
func (h *Handler) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	err = h.uc.DeleteTarget(ctx, id)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	render.NoContent(w, r)
}

// UpdateTargetStatus godoc
// @Summary      Update target status
// @Description  Changes the status of an existing target
// @Tags         targets
// @Accept       json
// @Produce      json
// @Param        id      path     string true "Target ID"
// @Param        request body     object{status=string} true "New status"
// @Success      200  {object} entity.Target
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /targets/{id}/status [patch].
func (h *Handler) UpdateTargetStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

		return
	}
	defer func() { _ = r.Body.Close() }()

	var req struct {
		Status string `json:"status"`
	}

	err = json.Unmarshal(body, &req)
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	updated, err := h.uc.UpdateTargetStatus(ctx, id, req.Status)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, updated)
}
