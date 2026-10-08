package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/transport/http/view"
)

func TestLogMoodThresholds(t *testing.T) {
	cases := map[int]string{0: "happy", 1: "watching", 2: "watching", 3: "scared", 4: "scared", 5: "angry", 6: "angry"}

	for n, want := range cases {
		mood, _ := view.LogMood(n)
		if mood.Key != want {
			t.Errorf("%d matches: mood %q, wanted %q", n, mood.Key, want)
		}
	}
}

func TestLogChartIsStacked(t *testing.T) {
	for _, c := range []struct {
		uc    fakeUseCase
		empty bool
	}{{filledUseCase(), false}, {fakeUseCase{}, true}} {
		var data view.ChartData

		err := json.Unmarshal(get(t, newServer(t, c.uc), "/api/log-matches").Body.Bytes(), &data)
		if err != nil {
			t.Fatalf("response did not decode: %v", err)
		}

		if !data.Stacked || data.Empty != c.empty {
			t.Errorf("stacked=%v empty=%v, wanted stacked and empty=%v", data.Stacked, data.Empty, c.empty)
		}
	}
}

func TestFutureDatedMatchesAreFlagged(t *testing.T) {
	uc := fakeUseCase{}
	uc.logs.Future = 10

	body := get(t, newServer(t, uc), "/partials/logs").Body.String()

	if !strings.Contains(body, "10 matches are dated in the future") || !strings.Contains(body, `data-count="0"`) {
		t.Error("matches from the future must be outside the counter, but named on the card")
	}
}

func TestHistoryPagerPausesPollingOnOlderPages(t *testing.T) {
	uc := filledUseCase()
	uc.logPage = &entity.Page[entity.LogMatch]{Items: make([]entity.LogMatch, 50), Total: 834, Skip: 50}

	for i := range uc.logPage.Items {
		uc.logPage.Items[i] = entity.LogMatch{ID: int64(1000 - i), Severity: "info", CreatedAt: testNow}
	}

	handler := newServer(t, uc)

	older := get(t, handler, "/partials/history/logs?page=2&target=app-worker").Body.String()
	for _, want := range []string{
		"51–100 of 834",
		"auto-refresh paused on older pages",
		`hx-get="/partials/history/logs?target=app-worker"`,
		`hx-get="/partials/history/logs?page=3&amp;target=app-worker"`,
	} {
		if !strings.Contains(older, want) {
			t.Errorf("second page: missing %q", want)
		}
	}

	if strings.Contains(older, "every 10s") {
		t.Error("second page polls: rows will slide away under the cursor")
	}

	first := get(t, handler, "/partials/history/logs").Body.String()
	if !strings.Contains(first, "every 10s") || strings.Contains(first, "auto-refresh paused") {
		t.Error("first page must poll")
	}
}

func TestAlertsRedirectsToHistory(t *testing.T) {
	w := get(t, newServer(t, filledUseCase()), "/alerts?sev=error")

	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/history/alerts?sev=error" {
		t.Errorf("code %d, Location %q", w.Code, w.Header().Get("Location"))
	}
}

func TestUnknownHistoryTabIs404(t *testing.T) {
	if w := get(t, newServer(t, filledUseCase()), "/history/nope"); w.Code != http.StatusNotFound {
		t.Errorf("response code %d, expected 404", w.Code)
	}
}

func TestMoodTakesWorstSignal(t *testing.T) {
	p := view.Presenter{Now: func() time.Time { return testNow }}
	up := []entity.TargetSummary{{ID: "a", Label: "api", Kind: entity.KindProbe, Status: entity.StatusUp}}
	down := []entity.TargetSummary{{ID: "a", Label: "api", Kind: entity.KindProbe, Status: entity.StatusDown}}

	cases := []struct {
		name    string
		targets []entity.TargetSummary
		logs    int
		mood    string
		reason  string
	}{
		{"all clean", up, 0, "happy", "logs are clean"},
		{"a couple of matches", up, 2, "watching", "2 log matches in 24h"},
		{"many matches with live targets", up, 6, "angry", "6 log matches in 24h"},
		{"target down with no logs", down, 0, "angry", "api is down"},
		{"target down and noisy logs", down, 3, "angry", "api is down"},
	}

	for _, c := range cases {
		report := entity.LogMatchReport{Matches: make([]entity.LogMatch, c.logs)}
		h := p.Overview(c.targets, entity.AlertCounts{}, report).Health

		if h.Mood.Key != c.mood || !strings.Contains(h.Reason, c.reason) {
			t.Errorf("%s: %s / %q, wanted %s / %q", c.name, h.Mood.Key, h.Reason, c.mood, c.reason)
		}
	}
}

func TestContainersNeedingAttentionComeFirst(t *testing.T) {
	p := view.Presenter{Now: func() time.Time { return testNow }}
	targets := []entity.TargetSummary{
		{ID: "1", Label: "aaa-calm", Kind: entity.KindContainer, Status: entity.StatusUp, Container: &entity.ContainerStats{}},
		{ID: "2", Label: "bbb-noisy", Kind: entity.KindContainer, Status: entity.StatusUp, Container: &entity.ContainerStats{}},
		{ID: "3", Label: "ccc-down", Kind: entity.KindContainer, Status: entity.StatusDown, Container: &entity.ContainerStats{}},
	}
	report := entity.LogMatchReport{Matches: []entity.LogMatch{{TargetID: "2"}, {TargetID: "2"}, {TargetID: "2"}}}

	cards := p.Overview(targets, entity.AlertCounts{}, report).Containers.Containers

	order := make([]string, 0, len(cards))
	for _, c := range cards {
		order = append(order, c.Name)
	}

	if strings.Join(order, ",") != "ccc-down,bbb-noisy,aaa-calm" {
		t.Errorf("order %v", order)
	}
}

func TestRecentActivityDropsQuietAndMerges(t *testing.T) {
	p := view.Presenter{Now: func() time.Time { return testNow }}
	panel := p.Recent(entity.Recent{
		Alerts: []entity.Alert{{Severity: "critical", Message: "port closed", TargetLabel: "api", CreatedAt: testNow.Add(-time.Minute)}},
		LogMatches: []entity.LogMatch{
			{Severity: "debug", Message: "noise", CreatedAt: testNow},
			{Severity: "warning", Message: "retrying", TargetLabel: "worker", TargetKind: entity.KindContainer, CreatedAt: testNow.Add(-2 * time.Minute)},
		},
		Incidents: []entity.Incident{{TargetLabel: "worker", Kind: entity.KindContainer, Cause: "die", State: entity.IncidentUnclosed, StartedAt: testNow.Add(-time.Hour)}},
	})

	kinds := make([]string, 0, len(panel.Rows))
	for _, r := range panel.Rows {
		kinds = append(kinds, r.Kind)
	}

	if strings.Join(kinds, ",") != "alert,log,outage" {
		t.Errorf("rows %v, wanted alert,log,outage without debug", kinds)
	}

	if panel.Rows[1].Href != "/containers/worker" || panel.Rows[1].TabHref != "/history/logs" {
		t.Errorf("log row links: %+v", panel.Rows[1])
	}
}
