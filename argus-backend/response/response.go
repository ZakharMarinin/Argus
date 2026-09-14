package response

import (
	"argus-collector/internal/domain/entity"
	"errors"
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

func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, entity.ErrTargetNotFound),
		errors.Is(err, entity.ErrContainerEventNotFound),
		errors.Is(err, entity.ErrProbeNotFound),
		errors.Is(err, entity.ErrAlertNotFound),
		errors.Is(err, entity.ErrLogMatchNotFound):
		JSONError(w, r, http.StatusNotFound, err.Error())

	case errors.Is(err, entity.ErrTargetAlreadyExists),
		errors.Is(err, entity.ErrContainerEventAlreadyExists):
		JSONError(w, r, http.StatusConflict, err.Error())

	case errors.Is(err, entity.ErrInvalidSeverity),
		errors.Is(err, entity.ErrInvalidEventType):
		JSONError(w, r, http.StatusBadRequest, err.Error())

	default:
		JSONError(w, r, http.StatusInternalServerError, "internal server error")
	}
}
