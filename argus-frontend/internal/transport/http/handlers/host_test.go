package handlers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"argus-frontend/internal/domain/entity"
)

func TestHostPanelShowsMeters(t *testing.T) {
	uc := filledUseCase()
	uc.host = entity.Host{
		Hostname:  "fedora",
		SampledAt: &testNow,
		CPU:       &entity.HostMetric{Percent: 12.4, Level: entity.HostLevelOK},
		Memory:    &entity.HostMetric{Percent: 92, Used: 30 << 30, Total: 32 << 30, Level: entity.HostLevelWarning},
	}

	for _, path := range []string{"/", "/partials/host"} {
		body := get(t, newServer(t, uc), path).Body.String()
		panel := body[strings.Index(body, `id="host-panel"`):]
		if end := strings.Index(panel, "sidebar__spacer"); end > 0 {
			panel = panel[:end]
		}

		for _, want := range []string{`hx-get="/partials/host"`, "12%", "30 / 32 GiB", "meter is-warn", "fedora"} {
			if !strings.Contains(panel, want) {
				t.Errorf("%s: host panel has no %q:\n%s", path, want, panel)
			}
		}

		if !strings.Contains(panel, `<span class="meter__value">—</span>`) {
			t.Errorf("%s: disk without a reading must show a dash, not a number:\n%s", path, panel)
		}
	}
}

func TestHostPanelExplainsMissingData(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"old collector", entity.ErrNotFound, "update argus-backend"},
		{"collector down", fmt.Errorf("%w: dial", entity.ErrNotReady), "no answer from the collector"},
	}

	for _, c := range cases {
		uc := filledUseCase()
		uc.hostErr = c.err

		body := get(t, newServer(t, uc), "/").Body.String()
		if !strings.Contains(body, c.want) || strings.Contains(body, "meter__track") {
			t.Errorf("%s: want note %q and no meters", c.name, c.want)
		}
	}
}

func TestHostPartialKeepsLastDataWhenCollectorIsDown(t *testing.T) {
	uc := filledUseCase()
	uc.hostErr = fmt.Errorf("%w: dial", entity.ErrNotReady)

	w := get(t, newServer(t, uc), "/partials/host")
	if w.Code != http.StatusServiceUnavailable || w.Body.Len() != 0 {
		t.Errorf("status = %d, body = %q; htmx keeps the last panel only on a non-2xx without a body", w.Code, w.Body.String())
	}
}
