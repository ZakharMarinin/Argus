// Package handlers - HTTP-слой дашборда: страницы, htmx-партиалы и точки
// графиков.
//
// Он только читает через порт UseCase и отдаёт готовую разметку. Ни Docker,
// ни Telegram, ни записи в бэкенд здесь нет и быть не должно.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/transport/http/view"
)

// queryTimeout ограничивает поход в бэкенд. Дашборд опрашивается по таймеру,
// и висящий запрос копил бы соединения вместо того, чтобы честно отвалиться.
const queryTimeout = 5 * time.Second

// UseCase - всё, что дашборд читает о состоянии мониторинга.
//
// ТЗ §6.2 задаёт три метода; здесь их больше. История (алерты, совпадения,
// простои) постранична и живёт на /history, сводка берёт только сутки;
// интервалы простоя выводятся на чтении - в схеме их нет.
type UseCase interface {
	TargetStatus(ctx context.Context) ([]entity.TargetSummary, error)
	AlertCounts(ctx context.Context) (entity.AlertCounts, error)
	ContainerDetail(ctx context.Context, name string) (entity.ContainerDetail, error)
	LatencySeries(ctx context.Context) ([]entity.LatencyPoint, error)
	Incidents(ctx context.Context) ([]entity.Incident, entity.IncidentStats, error)
	LogMatches(ctx context.Context, target string) (entity.LogMatchReport, error)
	HistoryAlerts(ctx context.Context, q entity.HistoryQuery) (entity.Page[entity.Alert], error)
	HistoryLogMatches(ctx context.Context, q entity.HistoryQuery) (entity.Page[entity.LogMatch], error)
	Recent(ctx context.Context) (entity.Recent, error)
	Ping(ctx context.Context) error
}

// Handler держит зависимости всех ручек дашборда.
type Handler struct {
	uc      UseCase
	tpl     *view.Templates
	present view.Presenter
	log     *slog.Logger
}

// NewHandler собирает обработчики.
func NewHandler(log *slog.Logger, uc UseCase, tpl *view.Templates, present view.Presenter) *Handler {
	return &Handler{uc: uc, tpl: tpl, present: present, log: log}
}

// Templates отдаёт набор шаблонов - роутеру он нужен под раздачу статики.
func (h *Handler) Templates() *view.Templates { return h.tpl }

// ---------------------------------------------------------------------------
// Снимок состояния
// ---------------------------------------------------------------------------

// snapshot - то, что нужно любой странице: список целей и счётчики алертов.
type snapshot struct {
	targets []entity.TargetSummary
	counts  entity.AlertCounts
	logs    entity.LogMatchReport
}

func (h *Handler) snapshot(ctx context.Context) (snapshot, error) {
	targets, err := h.uc.TargetStatus(ctx)
	if err != nil {
		return snapshot{}, err
	}

	counts, err := h.uc.AlertCounts(ctx)
	if err != nil {
		return snapshot{}, err
	}

	logs, err := h.uc.LogMatches(ctx, "")
	if err != nil {
		return snapshot{}, err
	}

	return snapshot{targets: targets, counts: counts, logs: logs}, nil
}

// layout собирает обвязку страницы вокруг готовой модели содержимого.
func (h *Handler) layout(snap snapshot, active, title, sub, partial string, content any) view.Layout {
	return view.Layout{
		Title:       title,
		Sub:         sub,
		Active:      active,
		PartialPath: partial,
		Nav:         h.present.Nav(snap.targets, snap.counts),
		Sidebar:     h.present.Sidebar(snap.targets, snap.counts, len(snap.logs.Matches)),
		Content:     content,
	}
}

// ---------------------------------------------------------------------------
// Ошибки и рендер
// ---------------------------------------------------------------------------

// pageError отвечает на ошибку чтения из бэкенда.
//
// «Бэкенд ещё не готов» и «бэкенд недоступен» - штатные состояния при первом
// запуске или рестарте: показываем экран ожидания, а не 500. Всё остальное -
// настоящая ошибка, и прятать её нельзя.
func (h *Handler) pageError(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, entity.ErrNotReady) {
		h.log.Error("запрос к бэкенду", "path", r.URL.Path, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	h.log.Warn("данные ещё не готовы", "path", r.URL.Path, "err", err)
	w.WriteHeader(http.StatusServiceUnavailable)
	h.page(w, "waiting", h.present.WaitingLayout())
}

// partialError отвечает на ошибку внутри партиала.
//
// Тела нет намеренно: htmx не подменяет содержимое при не-2xx, поэтому на
// экране остаются последние известные данные, а чип в шапке через полминуты
// сам напишет «нет связи с сервером». Это честнее, чем подсунуть пустой блок
// вместо живых данных.
func (h *Handler) partialError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, entity.ErrNotReady) {
		h.log.Warn("партиал: данных нет", "path", r.URL.Path, "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}

	h.log.Error("партиал", "path", r.URL.Path, "err", err)
	w.WriteHeader(http.StatusInternalServerError)
}

func (h *Handler) page(w http.ResponseWriter, name string, data view.Layout) {
	err := h.tpl.Page(w, name, data)
	if err != nil {
		h.log.Error("рендер страницы", "page", name, "err", err)
	}
}

func (h *Handler) partial(w http.ResponseWriter, name string, data any) {
	err := h.tpl.Partial(w, name, data)
	if err != nil {
		h.log.Error("рендер партиала", "partial", name, "err", err)
	}
}

func (h *Handler) json(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	err := json.NewEncoder(w).Encode(payload)
	if err != nil {
		h.log.Error("кодирование JSON", "err", err)
	}
}

// Healthz отвечает 200, только если бэкенд отвечает.
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	err := h.uc.Ping(ctx)
	if err != nil {
		http.Error(w, "backend unavailable", http.StatusServiceUnavailable)

		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
