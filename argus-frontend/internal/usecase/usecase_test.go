package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/gateway/backend"
	"argus-frontend/internal/usecase"
)

// Фикстуры повторяют JSON бэкенда, а не DTO клиента: клиент их не
// экспортирует, и тест сознательно идёт через настоящий HTTP - иначе
// перевод микросекунд в миллисекунды и числовой severity остались бы
// непроверенными.
type targetJSON struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	TargetType string    `json:"target_type"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type probeJSON struct {
	ID        int       `json:"id"`
	TargetID  string    `json:"target_id"`
	Success   bool      `json:"success"`
	Latency   int64     `json:"latency"`
	CreatedAt time.Time `json:"created_at"`
}

type eventJSON struct {
	ID        int       `json:"id"`
	TargetID  string    `json:"target_id"`
	Event     string    `json:"event"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

type alertJSON struct {
	ID        int       `json:"id"`
	TargetID  string    `json:"target_id,omitempty"`
	Severity  int       `json:"severity"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// fakeBackend изображает argus-backend ровно настолько, насколько нужно
// UseCase: /v1/api/targets, /v1/api/targets/{id}, /v1/api/probes,
// /v1/api/events, /v1/api/alerts, все с фильтром ?target_id=.
type fakeBackend struct {
	targets []targetJSON
	probes  map[string][]probeJSON // по target_id
	events  map[string][]eventJSON
	alerts  []alertJSON

	logMatches []logMatchJSON
	old        bool
	host       any
}

type logMatchJSON struct {
	ID        int       `json:"id"`
	TargetID  string    `json:"target_id"`
	Severity  int       `json:"severity"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

func listPage[T any](w http.ResponseWriter, r *http.Request, old bool, items []T, meta func(T) (int, string, int, time.Time)) {
	q := r.URL.Query()

	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil {
		limit = 50
	}

	if old {
		after, _ := strconv.Atoi(q.Get("offset"))
		out := []T{}
		for _, it := range items {
			if id, _, _, _ := meta(it); id > after && len(out) < limit {
				out = append(out, it)
			}
		}
		_ = json.NewEncoder(w).Encode(out)

		return
	}

	since, _ := time.Parse(time.RFC3339, q.Get("since"))
	skip, _ := strconv.Atoi(q.Get("skip"))

	matched := []T{}
	for _, it := range items {
		_, target, severity, created := meta(it)
		if len(q["target_id"]) > 0 && !slices.Contains(q["target_id"], target) {
			continue
		}
		if q.Get("host") == "true" && target != "" {
			continue
		}
		if q.Get("severity") != "" && q.Get("severity") != strconv.Itoa(severity) {
			continue
		}
		if !since.IsZero() && created.Before(since) {
			continue
		}
		matched = append(matched, it)
	}

	sort.SliceStable(matched, func(i, j int) bool {
		a, _, _, _ := meta(matched[i])
		b, _, _, _ := meta(matched[j])

		return a > b
	})

	w.Header().Set("X-Total-Count", strconv.Itoa(len(matched)))

	end := min(skip+limit, len(matched))
	if skip > len(matched) {
		skip = end
	}

	_ = json.NewEncoder(w).Encode(matched[skip:end])
}

func (f fakeBackend) server(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/api/targets/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/v1/api/targets/"):]
		for _, tg := range f.targets {
			if tg.ID == id {
				if r.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)

					return
				}
				_ = json.NewEncoder(w).Encode(tg)

				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/v1/api/targets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			f.create(w, r)

			return
		}
		_ = json.NewEncoder(w).Encode(f.targets)
	})
	mux.HandleFunc("/v1/api/host", func(w http.ResponseWriter, r *http.Request) {
		if f.host == nil {
			w.WriteHeader(http.StatusNotFound)

			return
		}
		_ = json.NewEncoder(w).Encode(f.host)
	})
	mux.HandleFunc("/v1/api/probes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(f.probes[r.URL.Query().Get("target_id")])
	})
	mux.HandleFunc("/v1/api/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(f.events[r.URL.Query().Get("target_id")])
	})
	mux.HandleFunc("/v1/api/alerts", func(w http.ResponseWriter, r *http.Request) {
		listPage(w, r, f.old, f.alerts, func(a alertJSON) (int, string, int, time.Time) {
			return a.ID, a.TargetID, a.Severity, a.CreatedAt
		})
	})
	mux.HandleFunc("/v1/api/log-matches", func(w http.ResponseWriter, r *http.Request) {
		listPage(w, r, f.old, f.logMatches, func(m logMatchJSON) (int, string, int, time.Time) {
			return m.ID, m.TargetID, m.Severity, m.CreatedAt
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func (f fakeBackend) create(w http.ResponseWriter, r *http.Request) {
	var body targetJSON
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch {
	case body.TargetType != "probe":
		w.WriteHeader(http.StatusInternalServerError)
	case body.Name == "":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"name is required"}`))
	case body.Source == "https://dup.local":
		w.WriteHeader(http.StatusConflict)
	default:
		w.WriteHeader(http.StatusCreated)
	}
}

func newUseCase(t *testing.T, fb fakeBackend) *usecase.UseCase {
	t.Helper()
	srv := fb.server(t)

	return usecase.NewUseCase(backend.NewClient(srv.URL, 2*time.Second))
}

// now - точка отсчёта для тестовых данных. UseCase считает окна (24ч, 1ч)
// от настоящего time.Now() (клиент в него не встроен, инъекция часов ради
// тестов такому маленькому агрегату - лишняя абстракция), поэтому и
// фикстуры должны быть относительно реального «сейчас», а не произвольной
// даты.
var now = time.Now()

// TestTargetStatusAggregatesFromRawProbes проверяет, что аптайм за 24ч и
// последняя латентность считаются из сырых проб, а не выдумываются - это
// ядро переезда с Postgres-агрегатов на HTTP + Go.
func TestTargetStatusAggregatesFromRawProbes(t *testing.T) {
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "t1", Name: "api", TargetType: "probe", Status: "up", UpdatedAt: now},
		},
		probes: map[string][]probeJSON{
			"t1": {
				{ID: 1, TargetID: "t1", Success: true, Latency: 42_000, CreatedAt: now.Add(-30 * time.Hour)}, // старше 24ч, не считается
				{ID: 2, TargetID: "t1", Success: true, Latency: 10_000, CreatedAt: now.Add(-2 * time.Hour)},
				{ID: 3, TargetID: "t1", Success: false, Latency: 20_000, CreatedAt: now.Add(-1 * time.Hour)},
			},
		},
	}

	targets, err := newUseCase(t, fb).TargetStatus(context.Background())
	if err != nil {
		t.Fatalf("TargetStatus: %v", err)
	}
	if len(targets) != 1 || targets[0].Probe == nil {
		t.Fatalf("targets = %+v", targets)
	}

	p := targets[0].Probe
	if p.Checks24h != 2 || p.Successes24h != 1 {
		t.Errorf("Checks24h=%d Successes24h=%d, хотели 2 и 1 (проба старше 24ч не должна учитываться)", p.Checks24h, p.Successes24h)
	}
	if p.LastLatencyMS == nil || *p.LastLatencyMS != 20 {
		t.Errorf("LastLatencyMS = %v, хотели 20мс (20000 микросекунд последней пробы)", p.LastLatencyMS)
	}
}

func TestContainerCrashesCountOnlyUnplannedReturns(t *testing.T) {
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up", UpdatedAt: now},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", Details: "exitCode=1", CreatedAt: now.Add(-40 * time.Minute)},
				{ID: 2, TargetID: "c1", Event: "start", CreatedAt: now.Add(-39 * time.Minute)},

				{ID: 3, TargetID: "c1", Event: "kill", Details: "signal=15", CreatedAt: now.Add(-12 * time.Minute)},
				{ID: 4, TargetID: "c1", Event: "die", Details: "exitCode=137", CreatedAt: now.Add(-12*time.Minute + 10*time.Second)},
				{ID: 5, TargetID: "c1", Event: "start", CreatedAt: now.Add(-11 * time.Minute)},

				{ID: 6, TargetID: "c1", Event: "die", Details: "exitCode=0", CreatedAt: now.Add(-10 * time.Minute)},
				{ID: 7, TargetID: "c1", Event: "restart", CreatedAt: now.Add(-9 * time.Minute)},

				{ID: 8, TargetID: "c1", Event: "die", Details: "exitCode=2", CreatedAt: now.Add(-8 * time.Minute)},
				{ID: 9, TargetID: "c1", Event: "start", CreatedAt: now.Add(-7 * time.Minute)},

				{ID: 10, TargetID: "c1", Event: "oom", CreatedAt: now.Add(-6 * time.Minute)},
				{ID: 11, TargetID: "c1", Event: "die", Details: "exitCode=137", CreatedAt: now.Add(-6 * time.Minute)},
				{ID: 12, TargetID: "c1", Event: "start", CreatedAt: now.Add(-5 * time.Minute)},

				{ID: 13, TargetID: "c1", Event: "die", Details: "exitCode=1", CreatedAt: now.Add(-2 * time.Minute)},
			},
		},
	}

	targets, err := newUseCase(t, fb).TargetStatus(context.Background())
	if err != nil {
		t.Fatalf("TargetStatus: %v", err)
	}

	c := targets[0].Container
	if c.Crashes != 2 {
		t.Errorf("Crashes = %d, want 2: exit 2 and oom within 15m; old crash, docker stop, clean exit and the unreturned die don't count", c.Crashes)
	}
	if c.LastEvent != "die" || c.LastEventDetail != "exitCode=1" {
		t.Errorf("LastEvent = %q/%q", c.LastEvent, c.LastEventDetail)
	}
}

func TestHistoryAlertsFiltersByKindAndLabels(t *testing.T) {
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "t1", Name: "api", TargetType: "probe", Status: "up"},
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		alerts: []alertJSON{
			{ID: 1, TargetID: "t1", Severity: 0, Details: "down", CreatedAt: now.Add(-time.Minute)},
			{ID: 2, TargetID: "c1", Severity: 2, Details: "flaky", CreatedAt: now.Add(-2 * time.Minute)},
		},
	}

	page, err := newUseCase(t, fb).HistoryAlerts(context.Background(), entity.HistoryQuery{Kind: entity.KindProbe, Page: 1})
	if err != nil {
		t.Fatalf("HistoryAlerts: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].TargetLabel != "api" || page.Items[0].Severity != entity.SeverityCritical {
		t.Errorf("page = %+v", page)
	}
}

// TestContainerDetailNotFound - 404 бэкенда должен стать entity.ErrNotFound,
// не общим entity.ErrNotReady, иначе несуществующий контейнер выглядит как
// «база не готова».
func TestContainerDetailNotFound(t *testing.T) {
	_, err := newUseCase(t, fakeBackend{}).ContainerDetail(context.Background(), "missing")
	if err != entity.ErrNotFound {
		t.Errorf("err = %v, хотели entity.ErrNotFound", err)
	}
}

// TestLatencySeriesGapsHoursWithNoChecks - час без проверок должен остаться
// разрывом (nil), а не притвориться нулём или пропасть из ряда: 24 точки
// всегда, с честными дырками (ТЗ "На экране только то, что есть в базе").
func TestLatencySeriesGapsHoursWithNoChecks(t *testing.T) {
	hourAgo := now.Add(-time.Hour).Truncate(time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "t1", Name: "api", TargetType: "probe", Status: "up"},
		},
		probes: map[string][]probeJSON{
			"t1": {
				{ID: 1, TargetID: "t1", Success: true, Latency: 100_000, CreatedAt: hourAgo.Add(time.Minute)},
				{ID: 2, TargetID: "t1", Success: false, Latency: 200_000, CreatedAt: hourAgo.Add(2 * time.Minute)},
			},
		},
	}

	series, err := newUseCase(t, fb).LatencySeries(context.Background())
	if err != nil {
		t.Fatalf("LatencySeries: %v", err)
	}
	if len(series) != 24 {
		t.Fatalf("len(series) = %d, хотели 24 часовые точки", len(series))
	}

	var filled, gaps int

	for _, pt := range series {
		if pt.UptimePct == nil {
			gaps++

			continue
		}

		filled++

		if pt.Hour.Equal(hourAgo) && *pt.UptimePct != 50 {
			t.Errorf("час %v: uptime = %v, хотели 50%% (1 из 2 проб успешна)", pt.Hour, *pt.UptimePct)
		}
	}

	if filled != 1 || gaps != 23 {
		t.Errorf("filled=%d gaps=%d, хотели 1 заполненный час и 23 разрыва", filled, gaps)
	}
}

func TestLatencySeriesKeepsSubMillisecondP95(t *testing.T) {
	hourAgo := now.Add(-time.Hour).Truncate(time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "t1", Name: "api", TargetType: "probe", Status: "up"},
		},
		probes: map[string][]probeJSON{
			"t1": {
				{ID: 1, TargetID: "t1", Success: true, Latency: 180, CreatedAt: hourAgo.Add(time.Minute)},
				{ID: 2, TargetID: "t1", Success: true, Latency: 430, CreatedAt: hourAgo.Add(2 * time.Minute)},
			},
		},
	}

	series, err := newUseCase(t, fb).LatencySeries(context.Background())
	if err != nil {
		t.Fatalf("LatencySeries: %v", err)
	}

	for _, pt := range series {
		if !pt.Hour.Equal(hourAgo) {
			continue
		}

		if pt.P95MS == nil || *pt.P95MS != 0.43 {
			t.Errorf("p95 = %v, хотели 0.43 мс (430 микросекунд)", pt.P95MS)
		}

		return
	}

	t.Fatalf("час %v не найден в ряду", hourAgo)
}

// TestPingUnreachableIsNotReady - недоступный бэкенд не должен всплывать как
// сырая сетевая ошибка: дашборд должен уметь показать экран ожидания.
func TestPingUnreachableIsNotReady(t *testing.T) {
	deadClient := backend.NewClient("http://127.0.0.1:1", 200*time.Millisecond)
	err := usecase.NewUseCase(deadClient).Ping(context.Background())
	if !errors.Is(err, entity.ErrNotReady) {
		t.Fatalf("err = %v, хотели entity.ErrNotReady", err)
	}
}
