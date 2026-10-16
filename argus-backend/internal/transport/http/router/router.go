package router

import (
	"argus-collector/internal/transport/http/handlers"
	"argus-collector/internal/transport/http/middleware"
	"log/slog"

	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger"
)

func Router(log *slog.Logger, r *chi.Mux, h *handlers.Handler) {
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	r.Get("/healthz", h.Healthz)

	r.Route("/v1/api", func(r chi.Router) {
		r.Use(middleware.New(log))
		
		r.Get("/host", h.GetHost)

		r.Route("/targets", func(r chi.Router) {
			r.Post("/", h.CreateTarget)
			r.Get("/", h.GetListTargets)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetTarget)
				r.Delete("/", h.DeleteTarget)
				r.Patch("/status", h.UpdateTargetStatus)
			})
		})

		r.Route("/probes", func(r chi.Router) {
			r.Get("/", h.GetListProbes)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetProbe)
				r.Delete("/", h.DeleteProbe)
			})
		})

		r.Route("/events", func(r chi.Router) {
			r.Get("/", h.GetListEvents)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetEvent)
				r.Delete("/", h.DeleteEvent)
			})
		})

		r.Route("/log-matches", func(r chi.Router) {
			r.Get("/", h.GetListLogMatches)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetLogMatch)
				r.Delete("/", h.DeleteLogMatch)
			})
		})

		r.Route("/alerts", func(r chi.Router) {
			r.Post("/", h.CreateAlert)
			r.Get("/", h.GetListAlerts)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetAlert)
				r.Delete("/", h.DeleteAlert)
			})
		})
	})
}
