// Package router assembles the dashboard's routes.
//
// The router is the stdlib ServeMux: the dashboard has a dozen GET endpoints
// without middleware, and an external router has nothing to do here.
package router

import (
	"net/http"

	"argus-frontend/internal/transport/http/handlers"
)

// Router returns a ready handler with all routes.
func Router(h *handlers.Handler) (http.Handler, error) {
	tpl := h.Templates()

	static, err := tpl.Static()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServerFS(static), tpl.IsLive())))

	// Pages.
	mux.HandleFunc("GET /{$}", h.Overview)
	mux.HandleFunc("GET /history/{tab}", h.History)
	mux.HandleFunc("GET /alerts", h.AlertsRedirect)
	mux.HandleFunc("GET /containers/{name}", h.Container)

	// Partials for htmx polling. Each returns a piece of markup with its own
	// hx-get inside, so it replaces itself and keeps refreshing.
	mux.HandleFunc("GET /partials/summary", h.PartialSummary)
	mux.HandleFunc("GET /partials/logs", h.PartialLogs)
	mux.HandleFunc("GET /partials/targets", h.PartialTargets)
	mux.HandleFunc("GET /partials/containers", h.PartialContainers)
	mux.HandleFunc("GET /partials/recent", h.PartialRecent)
	mux.HandleFunc("GET /partials/host", h.PartialHost)
	mux.HandleFunc("GET /partials/history/{tab}", h.PartialHistory)
	mux.HandleFunc("GET /partials/container/{name}/meta", h.PartialMeta)
	mux.HandleFunc("GET /partials/container/{name}/events", h.PartialEvents)
	mux.HandleFunc("GET /partials/container/{name}/timeline", h.PartialTimeline)
	mux.HandleFunc("GET /partials/container/{name}/logs", h.PartialContainerLogs)

	// Chart points. JSON, not HTML: the chart card isn't polled,
	// otherwise Chart.js would be recreated every 10 seconds.
	mux.HandleFunc("GET /api/latency", h.LatencyChart)
	mux.HandleFunc("GET /api/log-matches", h.LogMatchChart)
	mux.HandleFunc("GET /api/container/{name}/activity", h.ActivityChart)

	// Liveness probe for docker-compose and a reverse proxy.
	mux.HandleFunc("GET /healthz", h.Healthz)

	mux.HandleFunc("POST /targets", h.CreateTarget)
	mux.HandleFunc("DELETE /targets/{id}", h.DeleteTarget)

	return http.NewCrossOriginProtection().Handler(mux), nil
}

// cacheStatic controls caching of the embedded assets.
//
// In production each file's address carries the build fingerprint, so it can
// be cached for a long time. In development mode caching only gets in the way.
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
