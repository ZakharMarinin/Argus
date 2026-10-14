package handlers

import (
	"argus-collector/internal/domain/entity"
	"argus-collector/response"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// CreateAlert godoc
// @Summary      Create an alert
// @Description  Creates a new alert
// @Tags         alerts
// @Accept       json
// @Produce      json
// @Param        request body entity.Alert true "Alert data"
// @Success      201  {object} entity.Alert
// @Failure      400  {object} response.ErrorResponse
// @Router       /alerts [post].
//
//nolint:dupl
func (h *Handler) CreateAlert(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

		return
	}
	defer func() { _ = r.Body.Close() }()

	var alert entity.Alert

	err = json.Unmarshal(body, &alert)
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	created, err := h.uc.CreateAlert(ctx, &alert)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	render.Status(r, http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, created)
}

// GetListAlerts godoc
// @Summary      List alerts
// @Description  Returns alerts; filters can be combined. The total number of matching records is in the X-Total-Count header
// @Tags         alerts
// @Produce      json
// @Param        target_id query    []string 			false "Target ID, can be repeated" collectionFormat(multi)
// @Param        host      query    bool     			false "Only host alerts (no target)"
// @Param        severity  query    int      			false "Severity"
// @Param        status    query    entity.AlertStatus  false "Status filter: sent, cooldown, not_sent"
// @Param        since     query    string   			false "Not earlier than this moment, RFC 3339"
// @Param        order     query    string   			false "Order by id" Enums(asc, desc)
// @Param        limit     query    int      			false "Limit"
// @Param        skip      query    int      			false "How many records to skip"
// @Param        offset    query    int      			false "Only records with an id greater than this"
// @Success      200  {array}  entity.Alert
// @Header       200  {integer} X-Total-Count "Number of records matching the filter"
// @Failure      400  {object} response.ErrorResponse
// @Failure      500  {object} response.ErrorResponse
// @Router       /alerts [get].
func (h *Handler) GetListAlerts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	f, err := parseListFilter(r)
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

		return
	}

	if raw := r.URL.Query().Get("status"); raw != "" {
		ok := entity.AlertStatus(raw).Valid()
		if !ok {
			response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

			return
		}
		status := entity.AlertStatus(raw)
		f.Status = &status
	}

	alerts, total, err := h.uc.FindAlerts(ctx, f)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	setTotal(w, total)
	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, alerts)
}

// GetAlert godoc
// @Summary      Get an alert
// @Description  Returns an alert by ID
// @Tags         alerts
// @Produce      json
// @Param        id   path      int true "Alert ID"
// @Success      200  {object} entity.Alert
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /alerts/{id} [get].
func (h *Handler) GetAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	alert, err := h.uc.GetAlert(ctx, id)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, alert)
}

// DeleteAlert godoc
// @Summary      Delete an alert
// @Description  Deletes an alert by ID
// @Tags         alerts
// @Param        id   path      int true "Alert ID"
// @Success      204  "No Content"
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /alerts/{id} [delete].
func (h *Handler) DeleteAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	_, err = h.uc.DeleteAlert(ctx, id)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	render.NoContent(w, r)
}
