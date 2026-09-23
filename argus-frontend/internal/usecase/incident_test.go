package usecase_test

import (
	"context"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
)

// TestProbeIncidentMeasuresClosedDowntime - серия неудачных проб между двумя
// удачными должна стать одним интервалом с измеренной длительностью.
func TestProbeIncidentMeasuresClosedDowntime(t *testing.T) {
	start := now.Add(-3 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "t1", Name: "api", TargetType: "probe", Status: "up"},
		},
		probes: map[string][]probeJSON{
			"t1": {
				{ID: 1, TargetID: "t1", Success: true, Latency: 10_000, CreatedAt: start},
				{ID: 2, TargetID: "t1", Success: false, CreatedAt: start.Add(time.Minute)},
				{ID: 3, TargetID: "t1", Success: false, CreatedAt: start.Add(2 * time.Minute)},
				{ID: 4, TargetID: "t1", Success: true, Latency: 12_000, CreatedAt: start.Add(6 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("incidents = %+v, хотели один интервал", incidents)
	}

	i := incidents[0]
	if i.State != entity.IncidentClosed {
		t.Errorf("State = %q, хотели closed", i.State)
	}

	d, ok := i.Duration()
	if !ok || d != 5*time.Minute {
		t.Errorf("Duration = %v (ok=%v), хотели 5m: от первой неудачи до первой удачной пробы", d, ok)
	}
	if stats.Total != 5*time.Minute || stats.Longest != 5*time.Minute || stats.Unclosed != 0 {
		t.Errorf("stats = %+v, хотели 5m суммарно и ни одного незакрытого", stats)
	}
}

// TestProbeIncidentStaysOpenUntilLastFailure - продолжающийся простой считается
// до последней неудачной пробы, а не до time.Now(): если collector перестал
// проверять, счётчик не должен расти сам по себе.
func TestProbeIncidentStaysOpenUntilLastFailure(t *testing.T) {
	start := now.Add(-2 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "t1", Name: "api", TargetType: "probe", Status: "down"},
		},
		probes: map[string][]probeJSON{
			"t1": {
				{ID: 1, TargetID: "t1", Success: false, CreatedAt: start},
				{ID: 2, TargetID: "t1", Success: false, CreatedAt: start.Add(10 * time.Minute)},
			},
		},
	}

	incidents, _, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 || incidents[0].State != entity.IncidentOpen {
		t.Fatalf("incidents = %+v, хотели один open", incidents)
	}
	if incidents[0].EndedAt != nil {
		t.Error("у продолжающегося простоя не должно быть конца")
	}

	d, ok := i0Duration(t, incidents)
	if !ok || d != 10*time.Minute {
		t.Errorf("Duration = %v (ok=%v), хотели 10m - до последней неудачной пробы", d, ok)
	}
}

// TestContainerDieWithRestartIsMeasured - die, закрытый restart'ом, даёт
// точную длительность простоя контейнера.
func TestContainerDieWithRestartIsMeasured(t *testing.T) {
	died := now.Add(-90 * time.Minute)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", Details: "exit 1", CreatedAt: died},
				{ID: 2, TargetID: "c1", Event: "restart", CreatedAt: died.Add(4 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("incidents = %+v", incidents)
	}
	if incidents[0].State != entity.IncidentClosed || incidents[0].Cause != "die" {
		t.Errorf("incident = %+v, хотели closed с причиной die", incidents[0])
	}

	d, ok := i0Duration(t, incidents)
	if !ok || d != 4*time.Minute {
		t.Errorf("Duration = %v (ok=%v), хотели 4m", d, ok)
	}
	if stats.Total != 4*time.Minute || stats.Unclosed != 0 {
		t.Errorf("stats = %+v", stats)
	}
}

// TestContainerDieWithStartIsMeasured - argus-backend пишет "start" как
// событие, и именно оно закрывает простой при возврате контейнера: и после
// restart-политики, и после ручного docker start.
func TestContainerDieWithStartIsMeasured(t *testing.T) {
	died := now.Add(-2 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", Details: "exit 1", CreatedAt: died},
				{ID: 2, TargetID: "c1", Event: "start", CreatedAt: died.Add(3 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("incidents = %+v", incidents)
	}
	if incidents[0].State != entity.IncidentClosed {
		t.Errorf("State = %q, хотели closed: start - это и есть запись о возврате", incidents[0].State)
	}

	d, ok := i0Duration(t, incidents)
	if !ok || d != 3*time.Minute {
		t.Errorf("Duration = %v (ok=%v), хотели 3m", d, ok)
	}
	if stats.Unclosed != 0 {
		t.Errorf("stats = %+v, незакрытых быть не должно", stats)
	}
}

// TestContainerDieWithoutReturnHasNoDuration - главный честный случай: возврат
// контейнера argus-backend не записывает, поэтому длительность неизвестна и
// её нельзя ни посчитать, ни подмешать в сумму.
func TestContainerDieWithoutReturnHasNoDuration(t *testing.T) {
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", Details: "exit 137", CreatedAt: now.Add(-40 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 || incidents[0].State != entity.IncidentUnclosed {
		t.Fatalf("incidents = %+v, хотели один unclosed", incidents)
	}

	_, ok := incidents[0].Duration()
	if ok {
		t.Error("у незакрытого интервала не должно быть длительности")
	}
	if stats.Total != 0 || stats.Longest != 0 {
		t.Errorf("stats = %+v, незакрытый интервал не должен попадать в сумму", stats)
	}
	if stats.Count != 1 || stats.Unclosed != 1 {
		t.Errorf("stats = %+v, хотели один интервал и один незакрытый", stats)
	}
}

// TestKillAndDiePairIsOneOutage - Docker на одну остановку присылает два
// события, kill и die. Это один простой, а не два, и первый из них не должен
// оставаться незакрытым.
func TestKillAndDiePairIsOneOutage(t *testing.T) {
	stopped := now.Add(-30 * time.Minute)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "kill", Details: "signal=9", CreatedAt: stopped},
				{ID: 2, TargetID: "c1", Event: "die", Details: "exitCode=137", CreatedAt: stopped.Add(time.Second)},
				{ID: 3, TargetID: "c1", Event: "start", CreatedAt: stopped.Add(7 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("incidents = %+v, хотели один простой на пару kill+die", incidents)
	}
	if incidents[0].State != entity.IncidentClosed {
		t.Errorf("State = %q, хотели closed", incidents[0].State)
	}
	if incidents[0].Cause != "kill" {
		t.Errorf("Cause = %q, хотели первую причину падения - kill", incidents[0].Cause)
	}

	d, ok := i0Duration(t, incidents)
	if !ok || d != 7*time.Minute {
		t.Errorf("Duration = %v (ok=%v), хотели 7m от первого события падения", d, ok)
	}
	if stats.Unclosed != 0 {
		t.Errorf("stats = %+v, незакрытых быть не должно", stats)
	}
}

// TestIncidentDerivationKeepsEventOrder - сборка инцидентов не имеет права
// переупорядочивать события страницы: лента показывает новые сверху, и
// сортировка внутри вывода интервалов не должна её перетряхивать.
func TestIncidentDerivationKeepsEventOrder(t *testing.T) {
	base := now.Add(-3 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "kill", Details: "первое", CreatedAt: base},
				{ID: 2, TargetID: "c1", Event: "die", Details: "второе", CreatedAt: base.Add(time.Minute)},
				{ID: 3, TargetID: "c1", Event: "start", Details: "третье", CreatedAt: base.Add(2 * time.Minute)},
				{ID: 4, TargetID: "c1", Event: "kill", Details: "последнее", CreatedAt: base.Add(time.Hour)},
			},
		},
	}

	detail, err := newUseCase(t, fb).ContainerDetail(context.Background(), "worker")
	if err != nil {
		t.Fatalf("ContainerDetail: %v", err)
	}
	if len(detail.Events) != 4 {
		t.Fatalf("Events = %+v", detail.Events)
	}
	if detail.Events[0].Detail != "последнее" {
		t.Errorf("первым идёт %q, ожидали самое новое событие", detail.Events[0].Detail)
	}
	if detail.Events[3].Detail != "первое" {
		t.Errorf("последним идёт %q, ожидали самое старое событие", detail.Events[3].Detail)
	}
}

// TestDieAfterStartOpensNewIncident - после записи о возврате следующее
// падение начинает новый простой, а не продолжает прежний.
func TestDieAfterStartOpensNewIncident(t *testing.T) {
	first := now.Add(-5 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", CreatedAt: first},
				{ID: 2, TargetID: "c1", Event: "start", CreatedAt: first.Add(time.Hour)},
				{ID: 3, TargetID: "c1", Event: "die", CreatedAt: first.Add(2 * time.Hour)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 2 {
		t.Fatalf("incidents = %+v, хотели два интервала", incidents)
	}
	if stats.Unclosed != 1 {
		t.Errorf("stats = %+v, хотели один незакрытый - последнее падение без возврата", stats)
	}
	if !incidents[0].StartedAt.After(incidents[1].StartedAt) {
		t.Error("интервалы должны идти новыми сверху")
	}
}

// TestContainerDetailCarriesIncidents - страница контейнера получает те же
// интервалы, без отдельного запроса к бэкенду.
func TestContainerDetailCarriesIncidents(t *testing.T) {
	died := now.Add(-time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", CreatedAt: died},
				{ID: 2, TargetID: "c1", Event: "restart", CreatedAt: died.Add(time.Minute)},
			},
		},
	}

	detail, err := newUseCase(t, fb).ContainerDetail(context.Background(), "worker")
	if err != nil {
		t.Fatalf("ContainerDetail: %v", err)
	}
	if len(detail.Incidents) != 1 {
		t.Fatalf("Incidents = %+v", detail.Incidents)
	}

	d, ok := detail.Incidents[0].Duration()
	if !ok || d != time.Minute {
		t.Errorf("Duration = %v (ok=%v), хотели 1m", d, ok)
	}
}

func i0Duration(t *testing.T, incidents []entity.Incident) (time.Duration, bool) {
	t.Helper()

	return incidents[0].Duration()
}

func TestRestartWarningClearsAnHourAfterLastReturn(t *testing.T) {
	back := now.Add(-20 * time.Minute)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
			{ID: "c2", Name: "old", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", CreatedAt: now.Add(-50 * time.Minute)},
				{ID: 2, TargetID: "c1", Event: "start", CreatedAt: now.Add(-49 * time.Minute)},
				{ID: 3, TargetID: "c1", Event: "die", CreatedAt: back.Add(-time.Minute)},
				{ID: 4, TargetID: "c1", Event: "start", CreatedAt: back},
			},
			"c2": {
				{ID: 5, TargetID: "c2", Event: "die", CreatedAt: now.Add(-3 * time.Hour)},
				{ID: 6, TargetID: "c2", Event: "start", CreatedAt: now.Add(-3*time.Hour + time.Minute)},
			},
		},
	}

	targets, err := newUseCase(t, fb).TargetStatus(context.Background())
	if err != nil {
		t.Fatalf("TargetStatus: %v", err)
	}

	stats := map[string]*entity.ContainerStats{}
	for _, s := range targets {
		stats[s.Label] = s.Container
	}

	w := stats["worker"]
	if w.Restarts1h != 2 || w.RestartsClearAt == nil || !w.RestartsClearAt.Equal(back.Add(time.Hour)) {
		t.Errorf("worker = %+v, хотели 2 рестарта и снятие в %v", w, back.Add(time.Hour))
	}

	if o := stats["old"]; o.Restarts1h != 0 || o.RestartsClearAt != nil {
		t.Errorf("old = %+v, хотели без рестартов и без времени снятия", o)
	}
}
