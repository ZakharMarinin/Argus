// Package router собирает маршруты дашборда.
//
// Роутер - stdlib ServeMux: у дашборда десяток GET-ручек без middleware,
// и внешний роутер тут нечего делать.
package router

import (
	"net/http"

	"argus-frontend/internal/transport/http/handlers"
)

// Router возвращает готовый обработчик со всеми маршрутами.
func Router(h *handlers.Handler) (http.Handler, error) {
	tpl := h.Templates()

	static, err := tpl.Static()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServerFS(static), tpl.IsLive())))

	// Страницы.
	mux.HandleFunc("GET /{$}", h.Overview)
	mux.HandleFunc("GET /history/{tab}", h.History)
	mux.HandleFunc("GET /alerts", h.AlertsRedirect)
	mux.HandleFunc("GET /containers/{name}", h.Container)

	// Партиалы под htmx-поллинг. Каждый отдаёт кусок разметки со своим
	// hx-get внутри, поэтому подменяет сам себя и продолжает обновляться.
	mux.HandleFunc("GET /partials/summary", h.PartialSummary)
	mux.HandleFunc("GET /partials/logs", h.PartialLogs)
	mux.HandleFunc("GET /partials/targets", h.PartialTargets)
	mux.HandleFunc("GET /partials/containers", h.PartialContainers)
	mux.HandleFunc("GET /partials/recent", h.PartialRecent)
	mux.HandleFunc("GET /partials/history/{tab}", h.PartialHistory)
	mux.HandleFunc("GET /partials/container/{name}/meta", h.PartialMeta)
	mux.HandleFunc("GET /partials/container/{name}/events", h.PartialEvents)
	mux.HandleFunc("GET /partials/container/{name}/timeline", h.PartialTimeline)
	mux.HandleFunc("GET /partials/container/{name}/logs", h.PartialContainerLogs)

	// Точки графиков. JSON, а не HTML: карточка с графиком не поллится,
	// иначе Chart.js пересоздавался бы каждые 10 секунд.
	mux.HandleFunc("GET /api/latency", h.LatencyChart)
	mux.HandleFunc("GET /api/log-matches", h.LogMatchChart)
	mux.HandleFunc("GET /api/container/{name}/activity", h.ActivityChart)

	// Проба живости для docker-compose и обратного прокси.
	mux.HandleFunc("GET /healthz", h.Healthz)

	mux.HandleFunc("POST /targets", h.CreateTarget)
	mux.HandleFunc("DELETE /targets/{id}", h.DeleteTarget)

	return http.NewCrossOriginProtection().Handler(mux), nil
}

// cacheStatic управляет кэшированием вшитых ассетов.
//
// В проде адрес каждого файла несёт отпечаток сборки, поэтому его можно
// кэшировать надолго. В режиме разработки кэш только мешает.
func cacheStatic(next http.Handler, live bool) http.Handler {
	value := "public, max-age=31536000, immutable"
	if live {
		value = "no-cache"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}
