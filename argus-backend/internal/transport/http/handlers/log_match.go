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
// @Summary      Список лог-матчей
// @Description  Возвращает лог-матчи; фильтры сочетаются между собой. Общее число подходящих записей - в заголовке X-Total-Count
// @Tags         log-matches
// @Produce      json
// @Param        target_id query    []string false "ID таргета, можно несколько" collectionFormat(multi)
// @Param        severity  query    int      false "Важность"
// @Param        since     query    string   false "Не раньше этого момента, RFC 3339"
// @Param        order     query    string   false "Порядок по id" Enums(asc, desc)
// @Param        limit     query    int      false "Лимит"
// @Param        skip      query    int      false "Сколько записей пропустить"
// @Param        offset    query    int      false "Только записи с id больше указанного"
// @Success      200  {array}  entity.LogMatch
// @Header       200  {integer} X-Total-Count "Число записей под фильтром"
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
// @Summary      Получить лог-матч
// @Description  Возвращает лог-матч по ID
// @Tags         log-matches
// @Produce      json
// @Param        id   path      int true "ID лог-матча"
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
// @Summary      Удалить лог-матч
// @Description  Удаляет лог-матч по ID
// @Tags         log-matches
// @Param        id   path      int true "ID лог-матча"
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
