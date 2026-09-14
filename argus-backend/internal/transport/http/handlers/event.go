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

// GetListEvents godoc
// @Summary      Список событий
// @Description  Возвращает события с фильтрацией по таргету/типу либо постранично
// @Tags         events
// @Produce      json
// @Param        target_id query    string false "ID таргета"
// @Param        type      query    string false "Тип события"
// @Param        limit     query    int    false "Лимит"
// @Param        offset    query    int    false "Отступ"
// @Success      200  {array}  entity.ContainerEvent
// @Failure      400  {object} response.ErrorResponse
// @Failure      500  {object} response.ErrorResponse
// @Router       /events [get].
func (h *Handler) GetListEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	q := r.URL.Query()

	var (
		events []*entity.ContainerEvent
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

		events, err = h.uc.GetEventsByTarget(ctx, targetID)
	case q.Get("type") != "":
		events, err = h.uc.GetEventsByType(ctx, entity.EventType(q.Get("type")))
	default:
		limit, offset := parsePagination(r)
		events, err = h.uc.GetAllEvents(ctx, limit, offset)
	}

	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, events)
}

// GetEvent godoc
// @Summary      Получить событие
// @Description  Возвращает событие по ID
// @Tags         events
// @Produce      json
// @Param        id   path      int true "ID события"
// @Success      200  {object} entity.ContainerEvent
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /events/{id} [get].
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	event, err := h.uc.GetEvent(ctx, id)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, event)
}

// DeleteEvent godoc
// @Summary      Удалить событие
// @Description  Удаляет событие по ID
// @Tags         events
// @Param        id   path      int true "ID события"
// @Success      204  "No Content"
// @Failure      400  {object} response.ErrorResponse
// @Failure      404  {object} response.ErrorResponse
// @Router       /events/{id} [delete].
func (h *Handler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.JSONError(w, r, http.StatusBadRequest, "invalid argument")

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	_, err = h.uc.DeleteEvent(ctx, id)
	if err != nil {
		response.HandleError(w, r, err)

		return
	}

	render.NoContent(w, r)
}
