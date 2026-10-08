// Package web holds the dashboard's templates and static files, embedded in the binary.
//
// There is no code here on purpose: templates are rendered by
// internal/transport/http/view, and the directory stays next to them so that
// web.template_dir in the config points to the same place in dev and in production.
package web

import "embed"

// Assets holds the templates and static files embedded in the binary.
//
// Spec §4.4: htmx, Alpine and Chart.js are vendored as files and served from
// here rather than pulled from a CDN, so the dashboard opens on a host with no internet.
//
//go:embed templates static
var Assets embed.FS
