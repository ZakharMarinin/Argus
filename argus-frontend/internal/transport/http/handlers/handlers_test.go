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

// newServer поднимает сервер поверх подставных сценариев.
func newServer(t *testing.T, uc handlers.UseCase) http.Handler {
	t.Helper()

	tpl, err := view.ParseTemplates()
	if err != nil {
		t.Fatalf("шаблоны не разобрались: %v", err)
	}

	h := handlers.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), uc, tpl, testPresenter())

	handler, err := router.Router(h)
	if err != nil {
		t.Fatalf("роуты не собрались: %v", err)
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
// Страницы на реальных данных
// ---------------------------------------------------------------------------

func TestPagesRenderWithData(t *testing.T) {
	handler := newServer(t, filledUseCase())

	cases := []struct {
		path string
		want []string
	}{
		{"/", []string{"Uptime targets", "API health", "app-worker", "42 ms", "14min"}},
		{"/history/alerts", []string{"History", "TCP-порт не отвечает", "CRITICAL"}},
		{"/history/logs?sev=error", []string{"panic: connection refused", "all containers", "/api/log-matches", `href="/history/logs?sev=error&amp;target=app-worker"`}},
		{"/history/outages", []string{"return not recorded", "MEASURED DOWNTIME"}},
		{"/containers/app-worker", []string{"Docker events", "All logs for this container"}},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			w := get(t, handler, c.path)
			if w.Code != http.StatusOK {
				t.Fatalf("код ответа %d, ожидали 200", w.Code)
			}
			body := w.Body.String()
			for _, want := range c.want {
				if !strings.Contains(body, want) {
					t.Errorf("в выводе нет %q", want)
				}
			}
			if !strings.Contains(body, "</html>") {
				t.Error("документ не закрыт — шаблон оборвался на середине")
			}
		})
	}
}

// TestEmptyDatabaseShowsEmptyStates - главная проверка этой задачи.
//
// На пустой базе дашборд обязан честно сказать, что данных нет. Любое
// правдоподобное число здесь означало бы, что мониторинг врёт.
func TestEmptyDatabaseShowsEmptyStates(t *testing.T) {
	handler := newServer(t, fakeUseCase{})

	t.Run("сводка", func(t *testing.T) {
		body := get(t, handler, "/").Body.String()

		for _, want := range []string{
			"argus-backend hasn't recorded a single target yet",
			"No uptime targets",
			"No containers being watched",
			"Nothing happened in the last 24h",
			`data-count="0"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("нет пустого состояния %q", want)
			}
		}
		// Ни одного придуманного числа из старых заглушек.
		for _, forbidden := range []string{"99.2%", "5/6", "11 min", "11min", "docker.sock"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("на пустой базе показано выдуманное значение %q", forbidden)
			}
		}
	})

	t.Run("история", func(t *testing.T) {
		for path, want := range map[string]string{
			"/history/alerts":  "history is kept for 14 days",
			"/history/logs":    "No log matches",
			"/history/outages": "No outages in the last 24h",
		} {
			body := get(t, handler, path).Body.String()
			if !strings.Contains(body, want) {
				t.Errorf("%s: нет пустого состояния %q", path, want)
			}
		}
	})
}

// TestMissingValuesRenderAsDash проверяет, что отсутствующие значения
// показываются прочерком, а не нулём: «0 ms» и «0%» читались бы как измерение.
func TestMissingValuesRenderAsDash(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/").Body.String()

	// У SMTP relay нет ни одной проверки: latency и аптайм неизвестны.
	if !strings.Contains(body, "—") {
		t.Fatal("нет ни одного прочерка, хотя у цели нет проверок")
	}
	if strings.Contains(body, "0.00%") {
		t.Error("аптайм цели без проверок показан как 0.00% вместо прочерка")
	}
}

// ---------------------------------------------------------------------------
// База ещё не готова
// ---------------------------------------------------------------------------

// TestNotReadyShowsWaitingPage: порядок старта collector и web не гарантирован
// (ТЗ §9, §11), поэтому отсутствие схемы - штатное состояние, а не 500.
func TestNotReadyShowsWaitingPage(t *testing.T) {
	handler := newServer(t, fakeUseCase{err: entity.ErrNotReady})

	for _, path := range []string{"/", "/history/alerts", "/containers/app-worker"} {
		t.Run(path, func(t *testing.T) {
			w := get(t, handler, path)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("код ответа %d, ожидали 503", w.Code)
			}
			if !strings.Contains(w.Body.String(), "No data yet") {
				t.Error("вместо экрана ожидания отдано что-то другое")
			}
		})
	}
}

// TestRealErrorIsNotHiddenBehindWaiting: настоящую ошибку прятать за экраном
// ожидания нельзя, иначе баг в запросе будет выглядеть как «база не готова».
func TestRealErrorIsNotHiddenBehindWaiting(t *testing.T) {
	handler := newServer(t, fakeUseCase{err: errors.New("синтаксическая ошибка в запросе")})

	w := get(t, handler, "/")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("код ответа %d, ожидали 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "Данных пока нет") {
		t.Error("настоящая ошибка показана как ожидание данных")
	}
}

// TestPartialsFailWithoutBody: htmx не подменяет содержимое при не-2xx,
// поэтому на экране остаются последние живые данные, а чип в шапке сам
// сообщит о потере связи.
func TestPartialsFailWithoutBody(t *testing.T) {
	handler := newServer(t, fakeUseCase{err: entity.ErrNotReady})

	w := get(t, handler, "/partials/targets")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("код ответа %d, ожидали 503", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("партиал отдал тело на ошибке: %q", w.Body.String())
	}
}

func TestContainerNotFound(t *testing.T) {
	reader := filledUseCase()
	reader.detailErr = entity.ErrNotFound

	w := get(t, newServer(t, reader), "/containers/нет-такого")
	if w.Code != http.StatusNotFound {
		t.Errorf("код ответа %d, ожидали 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not being watched") {
		t.Error("нет понятного объяснения вместо голого 404")
	}
}

// ---------------------------------------------------------------------------
// Партиалы
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
				t.Fatalf("код ответа %d", w.Code)
			}
			// Партиал подменяет сам себя через outerHTML и обязан переиздать
			// собственный hx-get, иначе после первого свапа поллинг умрёт.
			if !strings.Contains(w.Body.String(), c.want) {
				t.Errorf("партиал не переиздал %q", c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Инциденты
// ---------------------------------------------------------------------------

// TestUnclosedOutageHasNoDuration - главный честный случай таймлайна:
// argus-backend не записывает возвращение контейнера, поэтому длительность
// такого простоя неизвестна и на экране стоит прочерк.
func TestUnclosedOutageHasNoDuration(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/partials/history/outages").Body.String()

	if !strings.Contains(body, "return not recorded") {
		t.Error("незакрытый простой не помечен на экране")
	}
	// Закрытый интервал рядом измерен - значит прочерк относится именно к
	// незакрытому, а не к пустому блоку.
	if !strings.Contains(body, "5 min") {
		t.Error("измеренный простой не показан")
	}
	if !strings.Contains(body, "—") {
		t.Error("у незакрытого простоя нет прочерка в длительности")
	}
	if strings.Contains(body, "0 s") || strings.Contains(body, "0 min") {
		t.Error("незакрытый простой показан нулём вместо прочерка")
	}
}

// TestOutageStatsExcludeUnmeasurable - сумма считается только по измеримым
// интервалам, а про остальные говорит подпись блока. Иначе сумма читалась бы
// как полная картина простоя.
func TestOutageStatsExcludeUnmeasurable(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/partials/history/outages").Body.String()

	if !strings.Contains(body, "MEASURED DOWNTIME") {
		t.Fatal("плитки суммарного простоя нет")
	}
	if !strings.Contains(body, "1 without a recorded return") {
		t.Error("подпись не называет число интервалов без записи о возврате")
	}
}

// TestEmptyOutagesShowsEmptyState - пустая история простоев даёт пустое
// состояние, а не нулевые плитки.
func TestEmptyOutagesShowsEmptyState(t *testing.T) {
	body := get(t, newServer(t, fakeUseCase{}), "/partials/history/outages").Body.String()

	if !strings.Contains(body, "No outages in the last 24h") {
		t.Error("пустого состояния нет")
	}
	if strings.Contains(body, "MEASURED DOWNTIME") {
		t.Error("плитки показаны там, где считать нечего")
	}
}

// TestTimelineMarksUnclosedWithoutFillingToNow - незакрытый простой не тянется
// до правого края полосы: это утверждало бы, что контейнер лежит до сих пор.
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
		t.Error("незакрытый простой нарисован не меткой неизвестности")
	}
	if strings.Contains(body, "is-down") {
		t.Error("незакрытый простой нарисован как подтверждённый простой")
	}
	if !strings.Contains(body, "width:0.900%") {
		t.Errorf("метка растянута вместо узкой засечки:\n%s", body)
	}
	if !strings.Contains(body, "return not recorded") {
		t.Error("легенда не называет засечку, которая на полосе есть")
	}
	if strings.Contains(body, "measured outage") {
		t.Error("легенда обещает измеренный простой, которого на полосе нет")
	}
}

// TestTimelineListsOutagesWithDurations - полоса показывает картину суток, но
// короткий простой занимает на ней доли процента. Цифры должны быть в списке
// под ней, иначе длительность видна только по наведению.
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
		t.Error("длительность измеренного простоя не попала в список")
	}
	if !strings.Contains(body, "recovered") || !strings.Contains(body, "return not recorded") {
		t.Error("состояния простоев не подписаны")
	}

	// Новые сверху: незакрытый простой свежее измеренного.
	unclosed := strings.Index(body, "return not recorded")
	recovered := strings.Index(body, "recovered")
	if unclosed > recovered {
		t.Error("список идёт не новыми сверху")
	}
	// Имя контейнера в строках не дублируется - оно в заголовке страницы.
	if strings.Contains(body, `class="incident-row__name"`) {
		t.Error("строка списка повторяет имя контейнера")
	}
}

// TestTimelineMeasuresClosedOutage - закрытый интервал занимает долю полосы,
// пропорциональную своей длительности.
func TestTimelineMeasuresClosedOutage(t *testing.T) {
	uc := filledUseCase()
	// Шесть часов из суток - ровно четверть полосы, начиная с её середины.
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
		t.Errorf("отрезок посчитан неверно:\n%s", body)
	}
	if !strings.Contains(body, "is-down") {
		t.Error("измеренный простой не помечен как простой")
	}
	// Легенда перечисляет только то, что на полосе есть: обозначение
	// незакрытого простоя под полосой без таких засечек читается как статус.
	if strings.Contains(body, "return not recorded") {
		t.Error("легенда обещает незакрытый простой, которого на полосе нет")
	}
	if !strings.Contains(body, "measured outage") {
		t.Error("легенда не называет то, что нарисовано")
	}
}

// ---------------------------------------------------------------------------
// Графики
// ---------------------------------------------------------------------------

// TestLatencyChartKeepsGaps: час без проверок должен приехать как null.
// Ноль на его месте выглядел бы как измеренная нулевая задержка.
func TestLatencyChartKeepsGaps(t *testing.T) {
	w := get(t, newServer(t, filledUseCase()), "/api/latency")
	if w.Code != http.StatusOK {
		t.Fatalf("код ответа %d", w.Code)
	}

	var data view.ChartData
	err := json.Unmarshal(w.Body.Bytes(), &data)
	if err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if data.Empty {
		t.Fatal("данные есть, но график помечен пустым")
	}
	if len(data.Series) != 2 {
		t.Fatalf("рядов %d, ожидали 2", len(data.Series))
	}
	got := data.Series[0].Values[1]
	if got != nil {
		t.Errorf("час без проверок отдан как %v, ожидали null", *got)
	}
}

// TestEmptyChartIsMarkedEmpty: на пустой базе фронтенд должен показать
// сообщение, а не пустые оси, которые выглядят как «всё по нулям».
func TestEmptyChartIsMarkedEmpty(t *testing.T) {
	w := get(t, newServer(t, fakeUseCase{}), "/api/latency")

	var data view.ChartData
	err := json.Unmarshal(w.Body.Bytes(), &data)
	if err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if !data.Empty {
		t.Error("пустой ряд не помечен как пустой")
	}
}

// TestContainerChartIsNotLatency: контейнер никто не пингует, и рисовать ему
// latency было бы выдумкой - на его странице другой ряд.
func TestContainerChartIsNotLatency(t *testing.T) {
	w := get(t, newServer(t, filledUseCase()), "/api/container/app-worker/activity")
	if w.Code != http.StatusOK {
		t.Fatalf("код ответа %d", w.Code)
	}

	var data view.ChartData
	err := json.Unmarshal(w.Body.Bytes(), &data)
	if err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	for _, s := range data.Series {
		if strings.Contains(strings.ToLower(s.Label), "latency") ||
			strings.Contains(strings.ToLower(s.Label), "p95") {
			t.Errorf("на странице контейнера нарисован ряд %q", s.Label)
		}
	}
}

// ---------------------------------------------------------------------------
// Прочее
// ---------------------------------------------------------------------------

func TestStatusClass(t *testing.T) {
	cases := map[view.Status]string{
		view.StatusUp:   "is-up",
		view.StatusWarn: "is-warn",
		view.StatusDown: "is-down",
		view.StatusIdle: "is-idle",
		"":              "is-idle",
		"мусор":         "is-idle",
	}
	for status, want := range cases {
		got := status.Class()
		if got != want {
			t.Errorf("Status(%q).Class() = %q, ожидали %q", status, got, want)
		}
	}
}

func TestHealthzReflectsDatabase(t *testing.T) {
	w := get(t, newServer(t, filledUseCase()), "/healthz")
	if w.Code != http.StatusOK {
		t.Errorf("живая база: код %d, ожидали 200", w.Code)
	}
	if w := get(t, newServer(t, fakeUseCase{err: entity.ErrNotReady}), "/healthz"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("мёртвая база: код %d, ожидали 503", w.Code)
	}
}

// TestStaticAssetsEmbedded следит, чтобы вендоренные библиотеки и шрифты
// попали в бинарник: ТЗ §4.4 требует работы без интернета.
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
			t.Errorf("%s не вшит: %v", path, err)
		}
	}
}

// TestThemeIsResolvedBeforePaint: тему проставляет инлайновый скрипт в <head>
// до первой отрисовки, иначе страница мигает светлым.
func TestThemeIsResolvedBeforePaint(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/").Body.String()

	head := body[:strings.Index(body, "</head>")]
	if !strings.Contains(head, "argus:theme") {
		t.Error("скрипт темы не найден в <head>")
	}
	if !strings.Contains(head, "localStorage") {
		t.Error("выбор темы не сохраняется")
	}
	// Скрипт должен идти до стилей, иначе смысл в нём теряется.
	if strings.Index(head, "argusTheme") > strings.Index(head, "argus.css") {
		t.Error("скрипт темы стоит после стилей — страница мигнёт")
	}
}

// TestSidebarListsTroubledContainersFirst: в сайдбаре важен не алфавит и не
// порядок из базы, а то, на что смотреть в первую очередь.
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
			Container: &entity.ContainerStats{Restarts1h: 3, LastEvent: "restart"},
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
		t.Fatalf("не все контейнеры попали в сайдбар: calm=%d restarting=%d stopped=%d",
			calm, restarting, stopped)
	}
	if calm < restarting || calm < stopped {
		t.Error("спокойный контейнер оказался выше требующих внимания")
	}
	// Внутри группы сохраняется порядок бэкенда: он уже ранжирован.
	if restarting > stopped {
		t.Error("порядок бэкенда внутри группы не сохранился")
	}
}
