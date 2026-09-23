// Package usecase считает всё, что дашборд показывает.
//
// Бэкенд отдаёт только сырые списки (цели, пробы, события, алерты), поэтому
// агрегаты - аптайм за сутки, рестарты за час, счётчики алертов, почасовой
// p95 - живут здесь. Ни SQL, ни HTTP, ни HTML пакет не знает: снаружи он
// видит только порт Backend и доменные типы entity.
package usecase

import (
	"context"
	"errors"
	"fmt"

	"argus-frontend/internal/domain/entity"
)

// Backend - всё, что usecase просит у argus-backend.
//
// Порт описан здесь, а реализован в internal/gateway/backend: зависимость
// смотрит внутрь, к домену, а не наружу к HTTP.
type Backend interface {
	// Targets отдаёт все цели - и uptime-таргеты, и контейнеры.
	Targets(ctx context.Context) ([]entity.Target, error)

	// Probes отдаёт историю проверок одной цели.
	Probes(ctx context.Context, targetID string) ([]entity.ProbeOutcome, error)

	// Events отдаёт события Docker по одной цели-контейнеру.
	Events(ctx context.Context, targetID string) ([]entity.ContainerEvent, error)

	Alerts(ctx context.Context, q entity.ListQuery) (entity.Page[entity.Alert], error)
	LogMatches(ctx context.Context, q entity.ListQuery) (entity.Page[entity.LogMatch], error)

	// Ping проверяет, что бэкенд отвечает.
	Ping(ctx context.Context) error
}

// UseCase - чтение и агрегация состояния мониторинга.
type UseCase struct {
	backend Backend
}

// NewUseCase собирает сценарии поверх порта бэкенда.
func NewUseCase(b Backend) *UseCase {
	return &UseCase{backend: b}
}

// Ping проверяет, что бэкенд отвечает.
func (uc *UseCase) Ping(ctx context.Context) error {
	return notReady(uc.backend.Ping(ctx))
}

// notReady переводит любую ошибку бэкенда в entity.ErrNotReady: для дашборда
// недоступный бэкенд и бэкенд, который ещё поднимается, - одно и то же
// состояние ожидания.
func notReady(err error) error {
	if err == nil || errors.Is(err, entity.ErrBackendTooOld) {
		return err
	}

	return fmt.Errorf("%w: %w", entity.ErrNotReady, err)
}
