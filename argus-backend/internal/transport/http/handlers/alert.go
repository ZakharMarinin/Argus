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
// @Summary      Создать алерт
// @Description  Создает новый алерт
// @Tags         alerts
// @Accept       json
// @Produce      json
// @Param        request body entity.Alert true "Данные алерта"
// @Success      201  {object} entity.Alert
// @Failure      400  {object} response.ErrorResponse
// @Router       /alerts [post].
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
// @Summary      Список алертов
// @Description  Возвращает алерты с фильтрацией по важности/отправке либо постранично
// @Tags         alerts
// @Produce      json
// @Param        severity query    int  false "Важность"
// @Param        notified query    bool false "Отправлен ли алерт"
// @Param        limit    query    int  false "Лимит"
// @Param        offset   query    int  false "Отступ"
// @Success      200  {array}  entity.Alert
// @Failure      400  {object} response.ErrorResponse
// @Failure      500  {object} response.ErrorResponse
// @Router       /alerts [get].
func (h *Handler) GetListAlerts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	q := r.URL.Query()

	var (
		alerts []*entity.Alert
		err    error
	)

	switch {
	case q.Get("severity") != "":
		var severity int

		severity, err = strconv.Atoi(q.Get("severity"))
		if err != nil {
			response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

			return
		}

		alerts, err = h.uc.GetAlertsBySeverity(ctx, entity.Severity(severity))
	case q.Get("notified") != "":
		var notified bool

		notified, err = strconv.ParseBool(q.Get("notified"))
		if err != nil {
			response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

			return
		}

		alerts, err = h.uc.GetAlertsByNotification(ctx, notified)
	default:
		limit, offset := parsePagination(r)
		alerts, err = h.uc.GetAllAlerts(ctx, limit, offset)
	}

	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, alerts)
}

// GetAlert godoc
// @Summary      Получить алерт
// @Description  Возвращает алерт по ID
// @Tags         alerts
// @Produce      json
// @Param        id   path      int true "ID алерта"
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
// @Summary      Удалить алерт
// @Description  Удаляет алерт по ID
// @Tags         alerts
// @Param        id   path      int true "ID алерта"
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
