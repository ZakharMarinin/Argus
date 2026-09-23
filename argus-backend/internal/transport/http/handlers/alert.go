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
// @Summary      Список алертов
// @Description  Возвращает алерты; фильтры сочетаются между собой. Общее число подходящих записей - в заголовке X-Total-Count
// @Tags         alerts
// @Produce      json
// @Param        target_id query    []string false "ID таргета, можно несколько" collectionFormat(multi)
// @Param        host      query    bool     false "Только алерты хоста (без таргета)"
// @Param        severity  query    int      false "Важность"
// @Param        notified  query    bool     false "Отправлен ли алерт"
// @Param        since     query    string   false "Не раньше этого момента, RFC 3339"
// @Param        order     query    string   false "Порядок по id" Enums(asc, desc)
// @Param        limit     query    int      false "Лимит"
// @Param        skip      query    int      false "Сколько записей пропустить"
// @Param        offset    query    int      false "Только записи с id больше указанного"
// @Success      200  {array}  entity.Alert
// @Header       200  {integer} X-Total-Count "Число записей под фильтром"
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

	if raw := r.URL.Query().Get("notified"); raw != "" {
		notified, err := strconv.ParseBool(raw)
		if err != nil {
			response.JSONError(w, r, http.StatusBadRequest, "invalid arguments")

			return
		}

		f.Notified = &notified
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
