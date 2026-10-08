package handlers

import (
	"argus-collector/response"
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// GetListLogMatches godoc
// @Summary      List log matches
// @Description  Returns log matches; filters can be combined. The total number of matching records is in the X-Total-Count header
// @Tags         log-matches
// @Produce      json
// @Param        target_id query    []string false "Target ID, can be repeated" collectionFormat(multi)
// @Param        severity  query    int      false "Severity"
// @Param        since     query    string   false "Not earlier than this moment, RFC 3339"
// @Param        order     query    string   false "Order by id" Enums(asc, desc)
// @Param        limit     query    int      false "Limit"
// @Param        skip      query    int      false "How many records to skip"
// @Param        offset    query    int      false "Only records with an id greater than this"
// @Success      200  {array}  entity.LogMatch
// @Header       200  {integer} X-Total-Count "Number of records matching the filter"
// @Failure      400  {object} response.ErrorResponse
// @Failure      500  {object} response.ErrorResponse
// @Router       /log-matches [get].
func (h *Handler) GetListLogMatches(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	f, err := parseListFilter(r)
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

		return
	}

	logMatches, total, err := h.uc.FindLogMatches(ctx, f)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	setTotal(w, total)
	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, logMatches)
}

// GetLogMatch godoc
// @Summary      Get a log match
// @Description  Returns a log match by ID
// @Tags         log-matches
// @Produce      json
// @Param        id   path      int true "Log match ID"
// @Success      200  {object} entity.LogMatch
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /log-matches/{id} [get].
func (h *Handler) GetLogMatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	logMatch, err := h.uc.GetLogMatch(ctx, id)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, logMatch)
}

// DeleteLogMatch godoc
// @Summary      Delete a log match
// @Description  Deletes a log match by ID
// @Tags         log-matches
// @Param        id   path      int true "Log match ID"
// @Success      204  "No Content"
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /log-matches/{id} [delete].
func (h *Handler) DeleteLogMatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	_, err = h.uc.DeleteLogMatch(ctx, id)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	render.NoContent(w, r)
}
