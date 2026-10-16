package response

import (
	"argus-collector/internal/domain/entity"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/render"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

func JSONError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	render.JSON(w, r, ErrorResponse{Error: msg})
}

func HandleError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	if v, ok := errors.AsType[*entity.ValidationError](err); ok {
		JSONError(w, r, http.StatusBadRequest, v.Reason)

		return
	}

	switch {
	case errors.Is(err, entity.ErrNotFound):
		JSONError(w, r, http.StatusNotFound, entity.ErrNotFound.Error())

	case errors.Is(err, entity.ErrAlreadyExists):
		JSONError(w, r, http.StatusConflict, entity.ErrAlreadyExists.Error())

	default:
		log.Error("server error: ", "error", err)
		JSONError(w, r, http.StatusInternalServerError, "internal server error")
	}
}
