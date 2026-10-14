package handlers_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/transport/http/handlers"
	"argus-frontend/internal/transport/http/router"
	"argus-frontend/internal/transport/http/view"
	"argus-frontend/web"
)

func testPresenter() view.Presenter {
	return view.Presenter{
		Build:     "argus-web test",
		Retention: 14 * 24 * time.Hour,
		Now:       func() time.Time { return testNow },
	}
}

// newServer starts a server on top of fake use cases.
func newServer(t *testing.T, uc handlers.UseCase) http.Handler {
	t.Helper()

	tpl, err := view.ParseTemplates()
	if err != nil {
		t.Fatalf("templates did not parse: %v", err)
	}

	h := handlers.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), uc, tpl, testPresenter())

	handler, err := router.Router(h)
	if err != nil {
		t.Fatalf("routes did not build: %v", err)
	}

	return handler
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

	return w
}

// ---------------------------------------------------------------------------
// Pages with real data
// ---------------------------------------------------------------------------

func TestPagesRenderWithData(t *testing.T) {
	handler := newServer(t, filledUseCase())

	cases := []struct {
		path string
		want []string
	}{
		{"/", []string{"Uptime targets", "API health", "app-worker", "42 ms", "5 min", "measured downtime, 24h · 1 unmeasured"}},
		{"/history/alerts", []string{"History", "TCP port is not responding", "CRITICAL"}},
		{"/history/logs?sev=error", []string{"panic: connection refused", "all containers", "/api/log-matches", `href="/history/logs?sev=error&amp;target=app-worker"`}},
		{"/history/outages", []string{"return not recorded", "MEASURED DOWNTIME"}},
		{"/containers/app-worker", []string{"Docker events", "All logs for this container"}},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			w := get(t, handler, c.path)
			if w.Code != http.StatusOK {
				t.Fatalf("response code %d, expected 200", w.Code)
			}
			body := w.Body.String()
			for _, want := range c.want {
				if !strings.Contains(body, want) {
					t.Errorf("output is missing %q", want)
				}
			}
			if !strings.Contains(body, "</html>") {
				t.Error("document not closed: the template broke off halfway")
			}
		})
	}
}

// TestAlertHistoryShowsDeliveryStatus: cooldown is rendered as a suppression,
// distinct from "sent to Telegram", and an unknown status shows up as is.
func TestAlertHistoryShowsDeliveryStatus(t *testing.T) {
	uc := filledUseCase()
	uc.alerts = append(uc.alerts,
		entity.Alert{ID: 3, TargetLabel: "SMTP relay", TargetKind: entity.KindProbe, Severity: "critical",
			Message: "still down", Status: entity.AlertStatusCooldown, CreatedAt: testNow.Add(-3 * time.Minute)},
		entity.Alert{ID: 4, TargetLabel: "SMTP relay", TargetKind: entity.KindProbe, Severity: "info",
			Message: "odd one", Status: "queued", CreatedAt: testNow.Add(-4 * time.Minute)},
	)

	body := get(t, newServer(t, uc), "/history/alerts").Body.String()
	for _, want := range []string{
		`<span class="alert-row__state alert-row__state--sent">sent to Telegram</span>`,
		`<span class="alert-row__state alert-row__state--cooldown">suppressed (cooldown)</span>`,
		`<span class="alert-row__state">not sent</span>`,
		`<span class="alert-row__state">queued</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("output is missing %s", want)
		}
	}
}

// TestEmptyDatabaseShowsEmptyStates is the main check of this task.
//
// On an empty database the dashboard must honestly say there is no data. Any
// plausible number here would mean the monitoring is lying.
func TestEmptyDatabaseShowsEmptyStates(t *testing.T) {
	handler := newServer(t, fakeUseCase{})

	t.Run("overview", func(t *testing.T) {
		body := get(t, handler, "/").Body.String()

		for _, want := range []string{
			"argus-backend hasn't recorded a single target yet",
			"No uptime targets",
			"No containers being watched",
			"Nothing happened in the last 24h",
			`data-count="0"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("missing empty state %q", want)
			}
		}
		// Not a single made-up number from the old placeholders.
		for _, forbidden := range []string{"99.2%", "5/6", "11 min", "11min", "docker.sock"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("a made-up value %q is shown on an empty database", forbidden)
			}
		}
	})

	t.Run("history", func(t *testing.T) {
		for path, want := range map[string]string{
			"/history/alerts":  "history is kept for 14 days",
			"/history/logs":    "No log matches",
			"/history/outages": "No outages in the last 24h",
		} {
			body := get(t, handler, path).Body.String()
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing empty state %q", path, want)
			}
		}
	})
}

// TestMissingValuesRenderAsDash checks that missing values are shown as a
// dash, not zero: "0 ms" and "0%" would read as a measurement.
func TestMissingValuesRenderAsDash(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/").Body.String()

	// SMTP relay has no checks at all: latency and uptime are unknown.
	if !strings.Contains(body, "—") {
		t.Fatal("not a single dash, although the target has no checks")
	}
	if strings.Contains(body, "0.00%") {
		t.Error("uptime of a target without checks is shown as 0.00% instead of a dash")
	}
}

// ---------------------------------------------------------------------------
// Database not ready yet
// ---------------------------------------------------------------------------

// TestNotReadyShowsWaitingPage: the start order of collector and web isn't guaranteed
// (spec §9, §11), so a missing schema is a normal state, not a 500.
func TestNotReadyShowsWaitingPage(t *testing.T) {
	handler := newServer(t, fakeUseCase{err: entity.ErrNotReady})

	for _, path := range []string{"/", "/history/alerts", "/containers/app-worker"} {
		t.Run(path, func(t *testing.T) {
			w := get(t, handler, path)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("response code %d, expected 503", w.Code)
			}
			if !strings.Contains(w.Body.String(), "No data yet") {
				t.Error("something other than the waiting screen was returned")
			}
		})
	}
}

// TestRealErrorIsNotHiddenBehindWaiting: a real error must not be hidden behind
// the waiting screen, otherwise a bug in a query would look like "database not ready".
func TestRealErrorIsNotHiddenBehindWaiting(t *testing.T) {
	handler := newServer(t, fakeUseCase{err: errors.New("syntax error in query")})

	w := get(t, handler, "/")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("response code %d, expected 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "No data yet") {
		t.Error("a real error is shown as waiting for data")
	}
}

// TestPartialsFailWithoutBody: htmx doesn't swap content on a non-2xx,
// so the last live data stays on screen, and the chip in the header reports
// the lost connection by itself.
func TestPartialsFailWithoutBody(t *testing.T) {
	handler := newServer(t, fakeUseCase{err: entity.ErrNotReady})

	w := get(t, handler, "/partials/targets")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("response code %d, expected 503", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("partial returned a body on error: %q", w.Body.String())
	}
}

func TestContainerNotFound(t *testing.T) {
	reader := filledUseCase()
	reader.detailErr = entity.ErrNotFound

	w := get(t, newServer(t, reader), "/containers/no-such-container")
	if w.Code != http.StatusNotFound {
		t.Errorf("response code %d, expected 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not being watched") {
		t.Error("no clear explanation instead of a bare 404")
	}
}

// ---------------------------------------------------------------------------
// Partials
// ---------------------------------------------------------------------------

func TestPartialsReemitTheirPolling(t *testing.T) {
	handler := newServer(t, filledUseCase())

	cases := []struct{ path, want string }{
		{"/partials/summary", `hx-get="/partials/summary"`},
		{"/partials/logs", `hx-get="/partials/logs"`},
		{"/partials/targets", `hx-get="/partials/targets"`},
		{"/partials/containers", `hx-get="/partials/containers"`},
		{"/partials/recent", `hx-get="/partials/recent"`},
		{"/partials/history/alerts?type=container&sev=warning", `hx-get="/partials/history/alerts?sev=warning&amp;type=container"`},
		{"/partials/history/logs?sev=error&target=app-worker", `hx-get="/partials/history/logs?sev=error&amp;target=app-worker"`},
		{"/partials/history/outages", `hx-get="/partials/history/outages"`},
		{"/partials/container/app-worker/meta", `hx-get="/partials/container/app-worker/meta"`},
		{"/partials/container/app-worker/events", `hx-get="/partials/container/app-worker/events"`},
		{"/partials/container/app-worker/timeline", `hx-get="/partials/container/app-worker/timeline"`},
		{"/partials/container/app-worker/logs", `hx-get="/partials/container/app-worker/logs"`},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			w := get(t, handler, c.path)
			if w.Code != http.StatusOK {
				t.Fatalf("response code %d", w.Code)
			}
			// A partial replaces itself via outerHTML and must re-emit its
			// own hx-get, otherwise polling dies after the first swap.
			if !strings.Contains(w.Body.String(), c.want) {
				t.Errorf("partial did not re-emit %q", c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Incidents
// ---------------------------------------------------------------------------

// TestUnclosedOutageHasNoDuration is the main honest case of the timeline:
// argus-backend doesn't record the container's return, so the duration of
// such an outage is unknown and the screen shows a dash.
func TestUnclosedOutageHasNoDuration(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/partials/history/outages").Body.String()

	if !strings.Contains(body, "return not recorded") {
		t.Error("unclosed outage is not marked on screen")
	}
	// The closed interval next to it is measured, so the dash belongs to the
	// unclosed one and not to an empty block.
	if !strings.Contains(body, "5 min") {
		t.Error("measured outage is not shown")
	}
	if !strings.Contains(body, "—") {
		t.Error("unclosed outage has no dash for its duration")
	}
	if strings.Contains(body, "0 s") || strings.Contains(body, "0 min") {
		t.Error("unclosed outage is shown as zero instead of a dash")
	}
}

// TestOutageStatsExcludeUnmeasurable: the total is computed only over measurable
// intervals, and the block caption covers the rest. Otherwise the total would read
// as the full picture of downtime.
func TestOutageStatsExcludeUnmeasurable(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/partials/history/outages").Body.String()

	if !strings.Contains(body, "MEASURED DOWNTIME") {
		t.Fatal("no total downtime tile")
	}
	if !strings.Contains(body, "1 without a recorded return") {
		t.Error("caption doesn't name the number of intervals without a recovery record")
	}
}

// TestEmptyOutagesShowsEmptyState: an empty outage history gives an empty
// state, not zero tiles.
func TestEmptyOutagesShowsEmptyState(t *testing.T) {
	body := get(t, newServer(t, fakeUseCase{}), "/partials/history/outages").Body.String()

	if !strings.Contains(body, "No outages in the last 24h") {
		t.Error("no empty state")
	}
	if strings.Contains(body, "MEASURED DOWNTIME") {
		t.Error("tiles are shown where there is nothing to count")
	}
}

// TestTimelineMarksUnclosedWithoutFillingToNow: an unclosed outage doesn't stretch
// to the right edge of the bar: that would claim the container is still down.
func TestTimelineMarksUnclosedWithoutFillingToNow(t *testing.T) {
	detail := filledUseCase()
	detail.detail.Incidents = []entity.Incident{
		{
			TargetLabel: "app-worker", Kind: entity.KindContainer, Cause: "die",
			StartedAt: testNow.Add(-6 * time.Hour), LastKnownAt: testNow.Add(-6 * time.Hour),
			State: entity.IncidentUnclosed,
		},
	}

	body := get(t, newServer(t, detail), "/partials/container/app-worker/timeline").Body.String()

	if !strings.Contains(body, "is-idle") {
		t.Error("unclosed outage is not drawn as an unknown mark")
	}
	if strings.Contains(body, "is-down") {
		t.Error("unclosed outage is drawn as a confirmed outage")
	}
	if !strings.Contains(body, "width:0.900%") {
		t.Errorf("mark is stretched instead of a narrow tick:\n%s", body)
	}
	if !strings.Contains(body, "return not recorded") {
		t.Error("legend doesn't name the tick that is on the bar")
	}
	if strings.Contains(body, "measured outage") {
		t.Error("legend promises a measured outage that isn't on the bar")
	}
}

// TestTimelineListsOutagesWithDurations: the bar shows the picture of the day, but
// a short outage takes up a fraction of a percent on it. The numbers must be in the
// list under it, otherwise the duration is visible only on hover.
func TestTimelineListsOutagesWithDurations(t *testing.T) {
	uc := filledUseCase()
	stopped := testNow.Add(-2 * time.Hour)
	uc.detail.Incidents = []entity.Incident{
		{
			TargetLabel: "app-worker", Kind: entity.KindContainer, Cause: "kill",
			StartedAt: stopped, LastKnownAt: stopped,
			EndedAt: new(stopped.Add(7 * time.Minute)), State: entity.IncidentClosed,
		},
		{
			TargetLabel: "app-worker", Kind: entity.KindContainer, Cause: "die",
			StartedAt: testNow.Add(-20 * time.Minute), LastKnownAt: testNow.Add(-20 * time.Minute),
			State: entity.IncidentUnclosed,
		},
	}

	body := get(t, newServer(t, uc), "/partials/container/app-worker/timeline").Body.String()

	if !strings.Contains(body, "7 min") {
		t.Error("duration of the measured outage did not make it into the list")
	}
	if !strings.Contains(body, "recovered") || !strings.Contains(body, "return not recorded") {
		t.Error("outage states are not labelled")
	}

	// Newest first: the unclosed outage is more recent than the measured one.
	unclosed := strings.Index(body, "return not recorded")
	recovered := strings.Index(body, "recovered")
	if unclosed > recovered {
		t.Error("list is not ordered newest first")
	}
	// The container name isn't repeated in the rows: it is in the page heading.
	if strings.Contains(body, `class="incident-row__name"`) {
		t.Error("list row repeats the container name")
	}
}

// TestTimelineMeasuresClosedOutage: a closed interval takes up a share of the bar
// proportional to its duration.
func TestTimelineMeasuresClosedOutage(t *testing.T) {
	uc := filledUseCase()
	// Six hours out of 24 is exactly a quarter of the bar, starting from its middle.
	start := testNow.Add(-12 * time.Hour)
	uc.detail.Incidents = []entity.Incident{
		{
			TargetLabel: "app-worker", Kind: entity.KindContainer, Cause: "die",
			StartedAt: start, LastKnownAt: start,
			EndedAt: new(start.Add(6 * time.Hour)), State: entity.IncidentClosed,
		},
	}

	body := get(t, newServer(t, uc), "/partials/container/app-worker/timeline").Body.String()

	if !strings.Contains(body, "left:50.000%;width:25.000%") {
		t.Errorf("segment computed incorrectly:\n%s", body)
	}
	if !strings.Contains(body, "is-down") {
		t.Error("measured outage is not marked as an outage")
	}
	// The legend lists only what is on the bar: a legend entry for an unclosed
	// outage under a bar without such ticks reads as a status.
	if strings.Contains(body, "return not recorded") {
		t.Error("legend promises an unclosed outage that isn't on the bar")
	}
	if !strings.Contains(body, "measured outage") {
		t.Error("legend doesn't name what is drawn")
	}
}

// ---------------------------------------------------------------------------
// Charts
// ---------------------------------------------------------------------------

// TestLatencyChartKeepsGaps: an hour without checks must arrive as null.
// A zero in its place would look like a measured zero latency.
func TestLatencyChartKeepsGaps(t *testing.T) {
	w := get(t, newServer(t, filledUseCase()), "/api/latency")
	if w.Code != http.StatusOK {
		t.Fatalf("response code %d", w.Code)
	}

	var data view.ChartData
	err := json.Unmarshal(w.Body.Bytes(), &data)
	if err != nil {
		t.Fatalf("response did not decode: %v", err)
	}
	if data.Empty {
		t.Fatal("there is data, but the chart is marked empty")
	}
	if len(data.Series) != 2 {
		t.Fatalf("%d series, expected 2", len(data.Series))
	}
	got := data.Series[0].Values[1]
	if got != nil {
		t.Errorf("an hour without checks returned as %v, expected null", *got)
	}
}

// TestEmptyChartIsMarkedEmpty: on an empty database the frontend must show a
// message, not empty axes that look like "all zeros".
func TestEmptyChartIsMarkedEmpty(t *testing.T) {
	w := get(t, newServer(t, fakeUseCase{}), "/api/latency")

	var data view.ChartData
	err := json.Unmarshal(w.Body.Bytes(), &data)
	if err != nil {
		t.Fatalf("response did not decode: %v", err)
	}
	if !data.Empty {
		t.Error("empty series is not marked empty")
	}
}

// TestContainerChartIsNotLatency: nothing pings a container, and drawing latency
// for it would be made up: its page has a different series.
func TestContainerChartIsNotLatency(t *testing.T) {
	w := get(t, newServer(t, filledUseCase()), "/api/container/app-worker/activity")
	if w.Code != http.StatusOK {
		t.Fatalf("response code %d", w.Code)
	}

	var data view.ChartData
	err := json.Unmarshal(w.Body.Bytes(), &data)
	if err != nil {
		t.Fatalf("response did not decode: %v", err)
	}
	for _, s := range data.Series {
		if strings.Contains(strings.ToLower(s.Label), "latency") ||
			strings.Contains(strings.ToLower(s.Label), "p95") {
			t.Errorf("container page draws series %q", s.Label)
		}
	}
}

// ---------------------------------------------------------------------------
// Other
// ---------------------------------------------------------------------------

func TestStatusClass(t *testing.T) {
	cases := map[view.Status]string{
		view.StatusUp:   "is-up",
		view.StatusWarn: "is-warn",
		view.StatusDown: "is-down",
		view.StatusIdle: "is-idle",
		"":              "is-idle",
		"garbage":       "is-idle",
	}
	for status, want := range cases {
		got := status.Class()
		if got != want {
			t.Errorf("Status(%q).Class() = %q, expected %q", status, got, want)
		}
	}
}

func TestHealthzReflectsDatabase(t *testing.T) {
	w := get(t, newServer(t, filledUseCase()), "/healthz")
	if w.Code != http.StatusOK {
		t.Errorf("live database: code %d, expected 200", w.Code)
	}
	if w := get(t, newServer(t, fakeUseCase{err: entity.ErrNotReady}), "/healthz"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("dead database: code %d, expected 503", w.Code)
	}
}

// TestStaticAssetsEmbedded makes sure the vendored libraries and fonts end up
// in the binary: spec §4.4 requires working without internet.
func TestStaticAssetsEmbedded(t *testing.T) {
	want := []string{
		"static/css/argus.css",
		"static/js/htmx.min.js",
		"static/js/alpine.min.js",
		"static/js/chart.min.js",
		"static/js/app.js",
		"static/fonts/inter-cyrillic.woff2",
		"static/fonts/jetbrainsmono-cyrillic.woff2",
		"static/img/argus-happy.gif",
	}
	for _, path := range want {
		_, err := web.Assets.ReadFile(path)
		if err != nil {
			t.Errorf("%s is not embedded: %v", path, err)
		}
	}
}

// TestThemeIsResolvedBeforePaint: the theme is set by an inline script in <head>
// before the first paint, otherwise the page flashes light.
func TestThemeIsResolvedBeforePaint(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/").Body.String()

	head := body[:strings.Index(body, "</head>")]
	if !strings.Contains(head, "argus:theme") {
		t.Error("theme script not found in <head>")
	}
	if !strings.Contains(head, "localStorage") {
		t.Error("theme choice is not saved")
	}
	// The script must come before the styles, otherwise it loses its point.
	if strings.Index(head, "argusTheme") > strings.Index(head, "argus.css") {
		t.Error("theme script comes after the styles: the page will flash")
	}
}

// TestSidebarListsTroubledContainersFirst: in the sidebar what matters is neither the
// alphabet nor the database order, but what to look at first.
func TestSidebarListsTroubledContainersFirst(t *testing.T) {
	reader := fakeUseCase{targets: []entity.TargetSummary{
		{
			ID: "container:calm", Kind: entity.KindContainer,
			Label: "calm", Status: entity.StatusUp, UpdatedAt: testNow,
			Container: &entity.ContainerStats{LastEvent: "start"},
		},
		{
			ID: "container:restarting", Kind: entity.KindContainer,
			Label: "restarting", Status: entity.StatusUp, UpdatedAt: testNow,
			Container: &entity.ContainerStats{Crashes: 3, LastEvent: "restart"},
		},
		{
			ID: "container:stopped", Kind: entity.KindContainer,
			Label: "stopped", Status: entity.StatusDown, UpdatedAt: testNow,
			Container: &entity.ContainerStats{LastEvent: "die"},
		},
	}}

	body := get(t, newServer(t, reader), "/").Body.String()
	sidebar := body[:strings.Index(body, "</aside>")]

	calm := strings.Index(sidebar, "/containers/calm")
	restarting := strings.Index(sidebar, "/containers/restarting")
	stopped := strings.Index(sidebar, "/containers/stopped")
	if calm < 0 || restarting < 0 || stopped < 0 {
		t.Fatalf("not all containers made it into the sidebar: calm=%d restarting=%d stopped=%d",
			calm, restarting, stopped)
	}
	if calm < restarting || calm < stopped {
		t.Error("a calm container ended up above those needing attention")
	}
	// The backend order is kept within a group: it is already ranked.
	if restarting > stopped {
		t.Error("backend order within the group was not kept")
	}
}
