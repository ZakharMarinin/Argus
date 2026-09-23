// Package view - модели представления дашборда Argus и их рендер.
//
// Пакет ничего не знает ни про HTTP API бэкенда, ни про роуты: он принимает
// доменные структуры entity, раскладывает их Presenter'ом по своим типам и
// рендерит шаблоны из web/. Единственное место, где данные становятся
// текстом.
package view

import (
	"fmt"
	"html/template"
)

// Status - состояние цели в терминах дашборда. Значение попадает в CSS-класс,
// который задаёт пару --s / --s-tint; дальше маркер, пилюля и полоска
// разбирают их. Ни один шаблон не хардкодит цвет.
type Status string

const (
	StatusUp   Status = "up"   // всё отвечает
	StatusWarn Status = "warn" // деградация: рестарты, флаки в пробах
	StatusDown Status = "down" // не отвечает
	StatusIdle Status = "idle" // состояние неизвестно (ТЗ §9: не то же самое, что down)
)

// Class возвращает CSS-класс статуса.
func (s Status) Class() string {
	switch s {
	case StatusUp, StatusWarn, StatusDown, StatusIdle:
		return "is-" + string(s)
	default:
		return "is-idle"
	}
}

// Mood - настроение маскота. Один источник правды для картинки и подписи:
// и карточка на сводке, и превью в переключателе берут его отсюда.
type Mood struct {
	Key   string
	Title string
	Note  string
	Src   string
	When  string
}

// Moods - весь набор настроений, ключ совпадает с именем гифки.
var Moods = map[string]Mood{
	"happy": {
		Key: "happy", Src: "/static/img/argus-happy.gif",
		Title: "All quiet",
		Note:  "All targets are up, containers are alive, no alerts.",
		When:  "Everything is up, no alerts and no log matches in 24h.",
	},
	"watching": {
		Key: "watching", Src: "/static/img/argus-watching.gif",
		Title: "Watching",
		Note:  "Checks are running on schedule, no anomalies so far.",
		When:  "1–2 log matches in 24h — worth a glance, nothing loud.",
	},
	"scared": {
		Key: "scared", Src: "/static/img/argus-scared.gif",
		Title: "Something's off",
		Note:  "There are warnings: a flaky check, or a container just restarted.",
		When:  "A flaky check, a restart in the last hour, a warning alert, or 3–4 log matches.",
	},
	"angry": {
		Key: "angry", Src: "/static/img/argus-angry.gif",
		Title: "Critical failure",
		Note:  "There are critical alerts or targets that are down. Worth checking the history.",
		When:  "A target is down, a critical alert fired, or 5+ log matches in 24h.",
	},
	// Спящий Argus означает «следить пока не за чем»: бэкенд не отдал ни
	// одной цели. Про паузу автообновления говорит чип в шапке, а не маскот.
	"sleeping": {
		Key: "sleeping", Src: "/static/img/argus-sleeping.gif",
		Title: "Nothing to watch yet",
		Note:  "There isn't a single target yet. Argus will wake up once collector records the first check or sees a container.",
		When:  "Nothing is being watched yet.",
	},
}

// MoodOrder задаёт порядок кнопок в переключателе.
var MoodOrder = []string{"happy", "watching", "scared", "angry", "sleeping"}

// ---------------------------------------------------------------------------
// Обвязка страницы
// ---------------------------------------------------------------------------

// Layout - всё, что нужно layout.html: сайдбар и модель конкретной страницы
// в Content.
type Layout struct {
	Title  string
	Sub    string
	Active string // ключ текущего пункта меню

	// PartialPath показывается в чипе `hx-get ...` в шапке. Это не декор:
	// видно, какой партиал сейчас поллится.
	PartialPath string

	Nav     []NavItem
	Sidebar Sidebar

	Content any

	NewTarget TargetForm
}

type TargetForm struct {
	Name  string
	URL   string
	Error string
}

// NavItem - пункт меню в сайдбаре.
type NavItem struct {
	Key   string
	Label string
	Href  string
	Badge string
}

// Sidebar - постоянная часть сайдбара: маскот, версия, ресурсы хоста и
// сворачиваемый список контейнеров.
type Sidebar struct {
	Mood       Mood
	Legend     []LegendMood
	Build      string
	Containers []NavItem
	Host       HostPanel
}

type LegendMood struct {
	Mood
	Active bool
}

// HostPanel - метрики хоста (CPU/RAM/диск).
//
// В схеме бэкенда нет ни таблицы, ни колонки под них - собственного
// host-коллектора первая версия не предусматривает. Блок остаётся местом
// под него, но чисел не выдумывает (Available=false).
type HostPanel struct {
	Available bool
	Note      string
	Meters    []Meter
}

// Meter - одна полоска использования ресурса.
type Meter struct {
	Label   string
	Value   string
	Percent int
	Status  Status
}

// ---------------------------------------------------------------------------
// Сводка
// ---------------------------------------------------------------------------

// Overview - модель главной страницы.
type Overview struct {
	Health     Health
	Targets    TargetList
	Containers ContainerList
	Chart      Chart
	Logs       LogPulse
	Recent     RecentPanel
}

// Health - строка общего состояния.
//
// В схеме нет жизненного цикла алерта: «активен/закрыт» взять неоткуда.
// Поэтому считаются алерты за последние сутки, и подпись говорит именно это.
type Health struct {
	State        string // OK | DEGRADED | WATCH | NO DATA
	TargetsUp    int
	TargetsTotal int
	AlertsDay    int
	Bar          []HealthSeg
	Tiles        []Tile
	// Empty - бэкенд не отдал ни одной цели.
	Empty      bool
	Mood       Mood
	MoodStatus Status
	Reason     string
}

// Tone переводит State в значение data-health для фонового оттенка страницы.
func (h Health) Tone() string {
	switch h.State {
	case "OK":
		return "ok"
	case "WATCH":
		return "watch"
	case "DEGRADED":
		return "degraded"
	default:
		return "empty"
	}
}

// HealthSeg - сегмент полоски здоровья.
type HealthSeg struct {
	Status  Status
	Percent int
}

// Tile - одна из плотных цифр строки состояния.
type Tile struct {
	Value  string
	Label  string
	Status Status
}

// Chart - параметры карточки с графиком. Данные тянутся отдельным
// JSON-запросом, чтобы htmx-поллинг партиалов не пересоздавал canvas каждые
// 10 секунд.
type Chart struct {
	DataURL string
	Title   string
	Legend  []LegendItem
	// Hint поясняет, что именно нарисовано, если это неочевидно.
	Hint      string
	EmptyNote string
}

// LegendItem - подпись ряда с цветовой меткой.
type LegendItem struct {
	Label string
	// Color - имя токена палитры: accent, up, warn, down, idle.
	Color string
}

// TargetList - блок uptime-таргетов.
type TargetList struct {
	Note    string // «проверка каждые 30 с · таймаут 5 с»
	Targets []Target
}

// Target - одна uptime-цель.
type Target struct {
	ID      string
	Name    string
	URL     string
	Status  Status
	Label   string // up | degraded | down | no data
	Latency string
	Uptime  string
	// Strip - исходы последних проверок, слева направо (старые → новые).
	Strip []Status
}

// ContainerList - блок контейнеров.
type ContainerList struct {
	Note       string // «5 из 6 running»
	Containers []Container
}

// Container - одна карточка контейнера.
type Container struct {
	Name   string
	Note   string
	Href   string
	Status Status
	Label  string // up | warn | down | no data
	// Dark поднимает карточку на тёмную панель - контейнеры, которые точно
	// требуют внимания (не отвечают), а не просто предупреждение.
	Dark    bool
	Metrics []Metric
}

// Metric - значение вида restarts/last-event у карточки контейнера.
type Metric struct {
	Label string
	Value string
	// Status подсвечивает само значение. Пустой - нейтральное число.
	Status Status
}

// ---------------------------------------------------------------------------
// Алерты
// ---------------------------------------------------------------------------

// AlertPage - фильтры и список вместе: партиал отдаёт их одним куском,
// чтобы после клика по фильтру перерисовалось и активное состояние чипа.
type AlertPage struct {
	Types      []Chip
	Severities []Chip
	Alerts     []Alert
	// Query - текущие фильтры в виде строки запроса. Партиал переиспользует
	// её в собственном hx-get, поэтому поллинг сохраняет выбранный фильтр.
	Query template.URL
	Pager Pager
	Empty string
}

// Chip - кнопка фильтра.
type Chip struct {
	Label  string
	Href   template.URL
	Active bool
}

// Alert - строка истории алертов.
type Alert struct {
	Time     string
	Message  string
	Target   string
	Type     string
	Severity string
	State    string
	Status   Status
	MoodSrc  string
	// Alarm поднимает строку на тёмную панель. Достаётся свежим critical/error -
	// если раздать его всей истории, приём перестанет значить «смотри сюда».
	Alarm bool
}

// ---------------------------------------------------------------------------
// Детали контейнера
// ---------------------------------------------------------------------------

// Detail - страница одного контейнера.
type Detail struct {
	Name     string
	Hero     Hero
	Meta     MetaPanel
	Chart    Chart
	Timeline Timeline
	Events   EventPanel
	Logs     LogFeed
}

// Hero - тёмная плашка с маскотом и текущим поводом для беспокойства.
type Hero struct {
	Badge   string // «WARNING · RESTARTED»
	Title   string
	Rule    string // сработавшее событие, моноширинным
	MoodSrc string
	Status  Status
}

// MetaPanel - плитки состояния контейнера.
type MetaPanel struct {
	URL   string
	Tiles []MetaTile
}

// MetaTile - плитка «СТАТУС / РЕСТАРТЫ / ...».
type MetaTile struct {
	Label  string
	Value  string
	Status Status
}

// EventPanel - история событий Docker по контейнеру.
//
// Таблица событий хранит не только рестарты, но и die/oom/kill/destroy,
// поэтому панель называется по событиям, а не по одним перезапускам.
type EventPanel struct {
	URL    string
	Events []EventRow
}

// EventRow - одно событие Docker.
type EventRow struct {
	Time   string
	Reason string
	Kind   string
	Status Status
}

// ---------------------------------------------------------------------------
// Инциденты
// ---------------------------------------------------------------------------

// IncidentPanel - блок «сколько и как долго всё лежало».
type IncidentPanel struct {
	Note  string
	Stats []MetaTile
	Rows  []IncidentRow
}

// IncidentRow - один интервал простоя.
//
// Duration - готовая строка, и у незакрытого интервала это прочерк: конца
// в данных нет, а нарисованное число выглядело бы как измерение.
type IncidentRow struct {
	Name     string
	Href     string // ссылка на страницу контейнера; у проб пустая
	Kind     string
	Status   Status
	Label    string // recovered | still down | return not recorded
	Cause    string
	Started  string
	Duration string
}

// Timeline - полоса простоев за сутки на странице контейнера.
//
// Полоса статична между свапами htmx и не требует ни Chart.js, ни JS: это
// набор блоков с посчитанными left/width.
type Timeline struct {
	URL    string
	Title  string
	Hint   string
	Bands  []TimelineBand
	Legend []LegendItem
	// Rows - те же интервалы списком, новыми сверху.
	//
	// Полоса показывает картину суток, но короткий простой занимает на ней
	// доли процента: три падения подряд сливаются в одну засечку. Список
	// читается при любом масштабе, поэтому цифры живут в нём, а не в
	// подсказках по наведению.
	Rows []IncidentRow
	// Note заменяет полосу, когда рисовать нечего.
	Note string
}

// TimelineBand - один отрезок полосы.
type TimelineBand struct {
	Status Status
	// Style несёт left и width уже посчитанными: html/template вырезал бы
	// их из атрибута style как небезопасные.
	Style template.CSS
	// Title - подсказка по наведению: причина и длительность.
	Title string
}

// ---------------------------------------------------------------------------
// Состояние «данных ещё нет»
// ---------------------------------------------------------------------------

// Empty описывает пустой экран.
type Empty struct {
	Mood  Mood
	Title string
	Note  string
}

// WaitingForData - экран «данных ещё нет».
func WaitingForData(reason string) Empty {
	return Empty{
		Mood:  Moods["sleeping"],
		Title: "No data yet",
		Note:  reason,
	}
}

// ---------------------------------------------------------------------------
// Хелперы шаблонов
// ---------------------------------------------------------------------------

// styleSpan отдаёт положение и ширину отрезка полосы как typed CSS.
//
// Проценты дробные: интервал в минуту на суточной полосе в целых процентах
// исчез бы вовсе, а простой, которого не видно, - это простой, о котором не
// узнали.
func styleSpan(left, width float64) template.CSS {
	left = clampPercent(left)
	width = clampPercent(width)

	if left+width > 100 {
		width = 100 - left
	}

	// G203: оба значения - числа, уже ограниченные диапазоном 0..100.
	return template.CSS(fmt.Sprintf("left:%.3f%%;width:%.3f%%", left, width)) //nolint:gosec
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}

	if v > 100 {
		return 100
	}

	return v
}

// styleWidth отдаёт ширину как typed CSS, иначе html/template вырежет
// вычисленное значение из атрибута style.
func styleWidth(percent int) template.CSS {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// G203: значение — целое число, уже ограниченное диапазоном 0..100.
	return template.CSS(fmt.Sprintf("width:%d%%", percent)) //nolint:gosec
}

func styleHeight(percent float64) template.CSS {
	return template.CSS(fmt.Sprintf("height:%.1f%%", clampPercent(percent))) //nolint:gosec // G203: число, ограниченное 0..100
}
