package handlers

import (
	"argus-collector/internal/domain/entity"
	"context"
	"log/slog"
)

type UseCase interface {
	CreateTarget(ctx context.Context, target *entity.Target) (*entity.Target, error) 
}

type Handler struct {
	log *slog.Logger
	uc UseCase
}

func NewHandler(log *slog.Logger, uc UseCase) *Handler {
	return &Handler{log: log, uc: uc}
}
