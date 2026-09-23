package view

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// Presenter раскладывает доменные структуры store по моделям представления.
//
// Правило одно: показывать только то, что действительно есть в бэкенде. Там,
// где значения нет, стоит прочерк или пустое состояние - подставлять
// правдоподобное число вместо отсутствующего нельзя, иначе дашборд мониторинга
// начинает врать.
type Presenter struct {
	// Build - строка версии в подвале сайдбара.
	Build string
	// Retention - срок хранения из конфига, объясняет пустую историю.
	Retention time.Duration
	// Now подменяется в тестах.
	Now func() time.Time
}

func (p Presenter) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}

	return time.Now()
}

// ---------------------------------------------------------------------------
// Статусы
// ---------------------------------------------------------------------------

// statusOf переводит состояние цели из бэкенда в состояние представления.
//
// В бэкенде есть только up/down/unknown. Жёлтый warn не выдумывается из
// порогов - он появляется там, где под него есть факт, посчитанный в
// internal/store: контейнер, который рестартовал за последний час, или
// проба, у которой за сутки была хоть одна неудача.
func statusOf(s entity.Status) Status {
	switch s {
	case entity.StatusUp:
		return StatusUp
	case entity.StatusDown:
		return StatusDown
	case entity.StatusUnknown:
		return StatusIdle
	default:
		// Неизвестное значение из бэкенда - тоже «состояние неизвестно»,
		// а не молчаливое «всё хорошо».

		return StatusIdle
	}
}

func labelOf(s entity.Status) string {
	if s == entity.StatusUnknown {
		return "no data"
	}

	return string(s)
}

// ---------------------------------------------------------------------------
// Сводка
// ---------------------------------------------------------------------------

// Overview собирает главную страницу из целей, счётчиков алертов и
// совпадений в логах за сутки.
func (p Presenter) Overview(targets []entity.TargetSummary, counts entity.AlertCounts, logs entity.LogMatchReport) Overview {
	probes, containers := split(targets)

	health := p.health(probes, containers, counts)
	health.Mood, health.Reason = p.mood(targets, counts, len(logs.Matches))
	health.MoodStatus = moodStatus(health.Mood.Key)

	return Overview{
		Health: health,
		Chart:  LatencyChartCard(),
		Logs:   p.LogPulse(logs),
		Targets: TargetList{
			Note:    p.probeNote(probes),
			Targets: p.targets(probes),
		},
		Containers: ContainerList{
			Note:       p.containerNote(containers),
			Containers: p.containers(containers, logCounts(logs.Matches)),
		},
	}
}

func logCounts(matches []entity.LogMatch) map[string]int {
	out := make(map[string]int, len(matches))
	for _, m := range matches {
		out[m.TargetID]++
	}

	return out
}

func split(targets []entity.TargetSummary) (probes, containers []entity.TargetSummary) {
	for _, t := range targets {
		switch t.Kind {
		case entity.KindProbe:
			probes = append(probes, t)
		case entity.KindContainer:
			containers = append(containers, t)
		}
	}

	return
}

func (p Presenter) health(probes, containers []entity.TargetSummary, counts entity.AlertCounts) Health {
	h := Health{
		AlertsDay: counts.Day,
		Empty:     len(probes)+len(containers) == 0,
	}
	if h.Empty {
		h.State = "NO DATA"

		return h
	}

	var up, down, unknown int
	for _, t := range append(append([]entity.TargetSummary{}, probes...), containers...) {
		switch t.Status {
		case entity.StatusUp:
			up++
		case entity.StatusDown:
			down++
		case entity.StatusUnknown:
			unknown++
		default:
			unknown++
		}
	}
	total := up + down + unknown

	h.TargetsTotal = len(probes)
	for _, t := range probes {
		if t.Status == entity.StatusUp {
			h.TargetsUp++
		}
	}

	switch {
	case down > 0:
		h.State = "DEGRADED"
	case unknown > 0:
		h.State = "WATCH"
	default:
		h.State = "OK"
	}

	h.Bar = []HealthSeg{
		{Status: StatusUp, Percent: pct(up, total)},
		{Status: StatusIdle, Percent: pct(unknown, total)},
		{Status: StatusDown, Percent: pct(down, total)},
	}

	running := 0
	for _, c := range containers {
		if c.Status == entity.StatusUp {
			running++
		}
	}

	uptime, uptimeOK := aggregateUptime(probes)
	h.Tiles = []Tile{
		{
			Value: containerCount(running, len(containers)),
			Label: "containers running",
		},
		{
			Value:  percentOrDash(uptime, uptimeOK),
			Label:  "uptime, 24h",
			Status: uptimeStatus(uptime, uptimeOK),
		},
		{
			Value:  downtimeOrDash(uptime, uptimeOK),
			Label:  "downtime over 24h, estimated",
			Status: downtimeStatus(uptime, uptimeOK),
		},
	}

	return h
}

func containerCount(running, total int) string {
	if total == 0 {
		return "—"
	}

	return fmt.Sprintf("%d/%d", running, total)
}

// aggregateUptime считает доступность по всем uptime-таргетам сразу: суммой
// проверок, а не средним из средних - иначе редко опрашиваемая цель весила бы
// столько же, сколько частая.
func aggregateUptime(probes []entity.TargetSummary) (float64, bool) {
	var checks, ok int
	for _, t := range probes {
		if t.Probe == nil {
			continue
		}
		checks += t.Probe.Checks24h
		ok += t.Probe.Successes24h
	}
	if checks == 0 {
		return 0, false
	}

	return 100 * float64(ok) / float64(checks), true
}

func uptimeStatus(uptime float64, ok bool) Status {
	if !ok {
		return ""
	}
	if uptime >= 99.9 {
		return StatusUp
	}

	return StatusWarn
}

func downtimeStatus(uptime float64, ok bool) Status {
	if !ok || uptime >= 100 {
		return ""
	}

	return StatusDown
}

// downtimeOrDash пересчитывает долю неудачных проверок в минуты простоя.
// Это простая оценка по истории проверок, поэтому и подпись на плитке
// говорит «оценка».
func downtimeOrDash(uptime float64, ok bool) string {
	if !ok {
		return "—"
	}
	minutes := int(math.Round((100 - uptime) / 100 * 24 * 60))
	if minutes >= 60 {
		return fmt.Sprintf("%dh %02dmin", minutes/60, minutes%60)
	}

	return fmt.Sprintf("%dmin", minutes)
}

func (p Presenter) probeNote(probes []entity.TargetSummary) string {
	if len(probes) == 0 {
		return ""
	}
	var last *time.Time
	for _, t := range probes {
		if t.Probe == nil || t.Probe.LastCheckedAt == nil {
			continue
		}
		if last == nil || t.Probe.LastCheckedAt.After(*last) {
			last = t.Probe.LastCheckedAt
		}
	}
	if last == nil {
		return "no checks yet"
	}

	return "last checked " + p.ago(*last)
}

func (p Presenter) containerNote(containers []entity.TargetSummary) string {
	if len(containers) == 0 {
		return ""
	}
	running := 0
	for _, c := range containers {
		if c.Status == entity.StatusUp {
			running++
		}
	}

	return fmt.Sprintf("%d of %d running", running, len(containers))
}

// ---------------------------------------------------------------------------
// Uptime-таргеты
// ---------------------------------------------------------------------------

func (p Presenter) targets(probes []entity.TargetSummary) []Target {
	out := make([]Target, 0, len(probes))
	for _, t := range probes {
		status := statusOf(t.Status)
		stats := t.Probe
		if stats == nil {
			stats = &entity.ProbeStats{}
		}

		// Флаки - хоть одна неудача за сутки при формально живой цели - тоже
		// повод подсветить строку, хотя бэкенд числит её up.
		if status == StatusUp && stats.Checks24h > 0 && stats.Successes24h < stats.Checks24h {
			status = StatusWarn
		}

		target := Target{
			Name:   t.Label,
			URL:    t.Source,
			Status: status,
			Label:  warnAwareLabel(status, labelOf(t.Status)),
		}

		if t.Probe != nil {
			target.Latency = latencyOrDash(stats.LastLatencyMS)
			uptime, ok := stats.Uptime24h()
			target.Uptime = percentOrDash(uptime, ok)
			target.Strip = strip(stats.Recent)
		} else {
			target.Latency, target.Uptime = "—", "—"
		}
		out = append(out, target)
	}

	return out
}

func warnAwareLabel(status Status, base string) string {
	if status == StatusWarn && base == "up" {
		return "degraded"
	}

	return base
}

// strip переводит исходы проверок в столбики полоски. Длина полоски равна
// числу реальных проверок: недостающие столбики не дорисовываются.
func strip(outcomes []entity.ProbeOutcome) []Status {
	out := make([]Status, 0, len(outcomes))
	for _, o := range outcomes {
		if o.Success {
			out = append(out, StatusUp)
		} else {
			out = append(out, StatusDown)
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Контейнеры
// ---------------------------------------------------------------------------

func (p Presenter) containers(containers []entity.TargetSummary, logs map[string]int) []Container {
	out := make([]Container, 0, len(containers))
	rank := make(map[string]int, len(containers))

	for _, c := range containers {
		stats := c.Container
		if stats == nil {
			stats = &entity.ContainerStats{}
		}

		status := statusOf(c.Status)
		if status == StatusUp && stats.Restarts1h > 0 {
			status = StatusWarn
		}
		label := warnAwareLabel(status, labelOf(c.Status))
		_, logStatus := LogMood(logs[c.ID])

		switch {
		case c.Status == entity.StatusDown:
			rank[c.Label] = 3
		case stats.Restarts1h > 0:
			rank[c.Label] = 2
		case logStatus == StatusWarn || logStatus == StatusDown:
			rank[c.Label] = 1
		}

		out = append(out, Container{
			Name:   c.Label,
			Note:   p.containerCardNote(*stats),
			Href:   "/containers/" + url.PathEscape(c.Label),
			Status: status,
			Label:  label,
			Dark:   status == StatusDown,
			Metrics: []Metric{
				{Label: "restarts, 1h", Value: fmt.Sprint(stats.Restarts1h), Status: nonZero(stats.Restarts1h)},
				{Label: "log matches, 24h", Value: fmt.Sprint(logs[c.ID]), Status: countStatus(logs[c.ID], logStatus)},
				{Label: "last event", Value: firstNonEmpty(stats.LastEvent, "none")},
			},
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Name] > rank[out[j].Name] })

	return out
}

func nonZero(n int) Status {
	if n == 0 {
		return ""
	}

	return StatusWarn
}

func (p Presenter) containerCardNote(s entity.ContainerStats) string {
	if s.LastEvent == "" {
		return "no events yet"
	}
	note := s.LastEvent
	if s.LastEventDetail != "" {
		note += " · " + s.LastEventDetail
	}
	if s.LastEventAt != nil {
		note += " · " + p.ago(*s.LastEventAt)
	}

	return note
}

// ---------------------------------------------------------------------------
// Алерты
// ---------------------------------------------------------------------------

// AlertTypes и AlertSeverities - значения фильтров.
//
// Тип - это вид цели: своей колонки под тип алерта в бэкенде нет. Важности -
// все пять уровней entity.Severity бэкенда, от срочного к тихому.
var (
	AlertTypes      = []string{"all", "uptime", "container"}
	AlertSeverities = []string{
		"all",
		entity.SeverityCritical, entity.SeverityError, entity.SeverityWarning,
		entity.SeverityInfo, entity.SeverityDebug,
	}
)

// alertKind переводит значение фильтра в вид цели.
func alertKind(filter string) entity.Kind {
	switch filter {
	case "uptime":
		return entity.KindProbe
	case "container":
		return entity.KindContainer
	default:
		return ""
	}
}

// alertStatus переводит важность алерта в статус и настроение маскота.
func alertStatus(severity string) (Status, string) {
	switch severity {
	case entity.SeverityCritical, entity.SeverityError:
		return StatusDown, "angry"
	case entity.SeverityWarning:
		return StatusWarn, "scared"
	default: // info, debug
		return StatusIdle, "watching"
	}
}

func urgent(severity string) bool {
	return severity == entity.SeverityCritical || severity == entity.SeverityError
}

func alertType(kind entity.Kind) string {
	switch kind {
	case entity.KindProbe:
		return "uptime"
	case entity.KindContainer:
		return "container"
	default:
		return "—"
	}
}

// telegramState показывает единственный признак жизненного цикла, который
// есть у алерта: дошло ли сообщение до Telegram.
func telegramState(sent bool) string {
	if sent {
		return "sent to Telegram"
	}

	return "not sent"
}

// ---------------------------------------------------------------------------
// Детали контейнера
// ---------------------------------------------------------------------------

// Detail собирает страницу одного контейнера.
func (p Presenter) Detail(d entity.ContainerDetail) Detail {
	name := d.Target.Label
	base := "/partials/container/" + url.PathEscape(name)

	stats := d.Target.Container
	if stats == nil {
		stats = &entity.ContainerStats{}
	}

	return Detail{
		Name:     name,
		Hero:     p.hero(d.Target, *stats),
		Meta:     MetaPanel{URL: base + "/meta", Tiles: p.detailMeta(d.Target, *stats, len(d.LogMatches))},
		Chart:    ActivityChartCard(name),
		Timeline: p.Timeline(base, d.Incidents),
		Events: EventPanel{
			URL:    base + "/events",
			Events: p.events(d.Events),
		},
		Logs: p.ContainerLogFeed(name, d.LogMatches),
	}
}

// hero описывает словами то же, что показывают плитки рядом.
func (p Presenter) hero(t entity.TargetSummary, s entity.ContainerStats) Hero {
	switch {
	case t.Status == entity.StatusDown:
		return Hero{
			Badge:   "CRITICAL · CONTAINER DOWN",
			Title:   "Container stopped",
			Rule:    eventRule(s),
			MoodSrc: Moods["angry"].Src,
		}
	case t.Status == entity.StatusUnknown:
		return Hero{
			Badge:   "UNKNOWN · NO DATA",
			Title:   "Collector can't see the container",
			Rule:    eventRule(s),
			MoodSrc: Moods["sleeping"].Src,
		}
	case s.Restarts1h > 0:
		return Hero{
			Badge:   "WARNING · RESTARTED",
			Title:   fmt.Sprintf("%s in the last hour", plural(s.Restarts1h, "restart", "restarts")),
			Rule:    eventRule(s),
			MoodSrc: Moods["scared"].Src,
		}
	default:
		return Hero{
			Badge:   "RUNNING · no issues",
			Title:   "Container is running normally",
			Rule:    eventRule(s),
			MoodSrc: Moods["happy"].Src,
		}
	}
}

func eventRule(s entity.ContainerStats) string {
	if s.LastEvent == "" {
		return "no events recorded for this container yet"
	}
	if s.LastEventDetail != "" {
		return s.LastEvent + " · " + s.LastEventDetail
	}

	return s.LastEvent
}

func (p Presenter) detailMeta(t entity.TargetSummary, s entity.ContainerStats, logMatches int) []MetaTile {
	_, logStatus := LogMood(logMatches)

	return []MetaTile{
		{Label: "Status", Value: labelOf(t.Status), Status: statusOf(t.Status)},
		{Label: "Restarts, 1h", Value: fmt.Sprint(s.Restarts1h), Status: nonZero(s.Restarts1h)},
		{Label: "Log matches, 24h", Value: fmt.Sprint(logMatches), Status: logStatus},
		{Label: "Updated", Value: p.ago(t.UpdatedAt)},
	}
}

// events - история событий Docker. Смерть красная, возврат зелёный, рестарт
// жёлтый, остальное нейтрально.
func (p Presenter) events(events []entity.ContainerEvent) []EventRow {
	out := make([]EventRow, 0, len(events))
	for _, e := range events {
		status := StatusIdle
		switch e.Event {
		case "die", "oom", "kill":
			status = StatusDown
		case "start":
			status = StatusUp
		case "restart":
			status = StatusWarn
		}
		out = append(out, EventRow{
			Time:   p.timestamp(e.OccurredAt),
			Reason: firstNonEmpty(e.Detail, "no details"),
			Kind:   e.Event,
			Status: status,
		})
	}

	return out
}

// ---------------------------------------------------------------------------
// Графики
// ---------------------------------------------------------------------------

// LatencyChartCard - карточка графика на сводке.
func LatencyChartCard() Chart {
	return Chart{
		Title:   "Latency and uptime · 24h",
		DataURL: "/api/latency",
		Legend: []LegendItem{
			{Label: "p95, ms", Color: "accent"},
			{Label: "uptime, %", Color: "up"},
		},
	}
}

// ActivityChartCard - карточка графика на странице контейнера.
//
// Контейнер никто не пингует, latency у него нет и быть не может - поэтому
// здесь события Docker и совпадения в логах.
func ActivityChartCard(name string) Chart {
	return Chart{
		Title:   "Container activity · 24h",
		DataURL: "/api/container/" + url.PathEscape(name) + "/activity",
		Legend: []LegendItem{
			{Label: "Docker events", Color: "idle"},
			{Label: "log matches", Color: "warn"},
		},
		Hint: "this container has no latency — it isn't polled by a prober",
	}
}

// LatencyChart переводит почасовой ряд в данные для Chart.js.
func (p Presenter) LatencyChart(points []entity.LatencyPoint) ChartData {
	data := ChartData{
		Labels: make([]string, 0, len(points)),
		Series: []ChartSeries{
			{Label: "p95, ms", Color: "accent", Axis: "left", Style: "area", Unit: " ms"},
			{Label: "uptime, %", Color: "up", Axis: "right", Style: "line", Unit: "%"},
		},
	}

	empty := true
	for _, pt := range points {
		data.Labels = append(data.Labels, pt.Hour.Format("15:00"))
		data.Series[0].Values = append(data.Series[0].Values, intPtrToFloat(pt.P95MS))
		data.Series[1].Values = append(data.Series[1].Values, pt.UptimePct)
		if pt.P95MS != nil || pt.UptimePct != nil {
			empty = false
		}
	}
	data.Empty = empty

	return data
}

// ActivityChart переводит почасовую активность контейнера в данные графика.
func (p Presenter) ActivityChart(points []entity.ActivityPoint) ChartData {
	data := ChartData{
		Labels: make([]string, 0, len(points)),
		Series: []ChartSeries{
			{Label: "Docker events", Color: "idle", Axis: "left", Style: "bar"},
			{Label: "log matches", Color: "warn", Axis: "left", Style: "bar"},
		},
	}

	empty := true
	for _, pt := range points {
		data.Labels = append(data.Labels, pt.Hour.Format("15:00"))
		events, matches := float64(pt.Events), float64(pt.LogMatches)
		data.Series[0].Values = append(data.Series[0].Values, &events)
		data.Series[1].Values = append(data.Series[1].Values, &matches)
		if pt.Events+pt.LogMatches > 0 {
			empty = false
		}
	}
	data.Empty = empty

	return data
}

// ---------------------------------------------------------------------------
// Обвязка страницы
// ---------------------------------------------------------------------------

// Nav строит верхнее меню сайдбара. Контейнеры сюда не попадают: их список
// живёт отдельным сворачиваемым блоком (см. containerNav).
func (p Presenter) Nav(targets []entity.TargetSummary, counts entity.AlertCounts) []NavItem {
	return []NavItem{
		{Key: "overview", Label: "Overview", Href: "/", Badge: badge(len(targets))},
		{Key: "history", Label: "History", Href: "/history/alerts", Badge: badge(counts.Day)},
	}
}

// containerNav - список наблюдаемых контейнеров для сайдбара.
//
// Требующие внимания идут первыми: в сайдбаре важен не алфавит, а то, на что
// смотреть в первую очередь. Внутри каждой группы - алфавитный порядок
// TargetStatus.
func (p Presenter) containerNav(targets []entity.TargetSummary) []NavItem {
	attention := make([]NavItem, 0, len(targets))
	calm := make([]NavItem, 0, len(targets))

	for _, t := range targets {
		if t.Kind != entity.KindContainer {
			continue
		}
		item := NavItem{
			Key:   "container:" + t.Label,
			Label: t.Label,
			Href:  "/containers/" + url.PathEscape(t.Label),
		}
		if t.Status == entity.StatusDown || (t.Container != nil && t.Container.Restarts1h > 0) {
			item.Badge = "!"
			attention = append(attention, item)

			continue
		}
		calm = append(calm, item)
	}

	return append(attention, calm...)
}

func badge(n int) string {
	if n == 0 {
		return ""
	}

	return fmt.Sprint(n)
}

// Sidebar собирает постоянную часть сайдбара.
func (p Presenter) Sidebar(targets []entity.TargetSummary, counts entity.AlertCounts, logs int) Sidebar {
	mood, _ := p.mood(targets, counts, logs)

	return Sidebar{
		Mood:       mood,
		Legend:     legend(mood),
		Build:      p.build(true),
		Containers: p.containerNav(targets),
		Host: HostPanel{
			// Метрики хоста в первую версию не входят: у бэкенда нет ни
			// таблицы, ни колонки под них. Блок остаётся местом под будущий
			// host-коллектор, но полосок с числами не рисует.
			Available: false,
			Note:      "CPU, RAM, and disk aren't collected in this version",
		},
	}
}

func (p Presenter) build(backendOK bool) string {
	backend := "backend: no connection"
	if backendOK {
		backend = "backend: ok"
	}

	return firstNonEmpty(p.Build, "argus-web") + " · " + backend
}

type moodSignal struct {
	key    string
	reason string
}

func moodRank(key string) int {
	switch key {
	case "angry":
		return 3
	case "scared":
		return 2
	case "watching":
		return 1
	default:
		return 0
	}
}

func (p Presenter) mood(targets []entity.TargetSummary, counts entity.AlertCounts, logs int) (Mood, string) {
	if len(targets) == 0 {
		return Moods["sleeping"], Moods["sleeping"].Note
	}

	var signals []moodSignal

	var down []string

	for _, t := range targets {
		if t.Status == entity.StatusDown {
			down = append(down, t.Label)
		}
	}

	switch len(down) {
	case 0:
	case 1:
		signals = append(signals, moodSignal{"angry", down[0] + " is down"})
	default:
		signals = append(signals, moodSignal{"angry", fmt.Sprintf("%s and %d more are down", down[0], len(down)-1)})
	}

	if counts.Critical > 0 {
		signals = append(signals, moodSignal{"angry", plural(counts.Critical, "critical alert", "critical alerts") + " in 24h"})
	}

	if logs > 0 {
		mood, _ := LogMood(logs)
		signals = append(signals, moodSignal{mood.Key, plural(logs, "log match", "log matches") + " in 24h"})
	}

	for _, t := range targets {
		if t.Container != nil && t.Container.Restarts1h > 0 {
			signals = append(signals, moodSignal{"scared", t.Label + " restarted in the last hour"})
		}

		if t.Probe != nil && t.Probe.Checks24h > 0 && t.Probe.Successes24h < t.Probe.Checks24h {
			signals = append(signals, moodSignal{"scared", t.Label + " failed a check in 24h"})
		}
	}

	if counts.Warning > 0 {
		signals = append(signals, moodSignal{"scared", plural(counts.Warning, "warning alert", "warning alerts") + " in 24h"})
	}

	best := moodSignal{"happy", "all targets are up, no alerts, logs are clean"}
	for _, s := range signals {
		if moodRank(s.key) > moodRank(best.key) {
			best = s
		}
	}

	return Moods[best.key], best.reason
}

func moodStatus(key string) Status {
	switch key {
	case "angry":
		return StatusDown
	case "scared":
		return StatusWarn
	case "happy":
		return StatusUp
	default:
		return StatusIdle
	}
}

func legend(current Mood) []LegendMood {
	out := make([]LegendMood, 0, len(MoodOrder))
	for _, key := range MoodOrder {
		out = append(out, LegendMood{Mood: Moods[key], Active: key == current.Key})
	}

	return out
}

// ---------------------------------------------------------------------------
// Форматирование
// ---------------------------------------------------------------------------

var months = [...]string{
	"jan", "feb", "mar", "apr", "may", "jun",
	"jul", "aug", "sep", "oct", "nov", "dec",
}

// timestamp - «05 sep 14:22». Год не пишется: дальше ретеншна история не живёт.
func (p Presenter) timestamp(t time.Time) string {
	local := t.Local()

	return fmt.Sprintf("%02d %s %02d:%02d",
		local.Day(), months[local.Month()-1], local.Hour(), local.Minute())
}

// ago - «3 minutes ago». Для свежих событий это читается быстрее абсолютного времени.
//
// Нулевое время - не «очень давно», а «неизвестно»: бэкенд может вернуть
// time.Time{} для события, которое watcher записал не проставив метку.
// Считать отсюда разницу и печатать «106751 days ago» было бы куда менее
// честно, чем прочерк.
func (p Presenter) ago(t time.Time) string {
	if t.IsZero() {
		return "—"
	}

	d := p.now().Sub(t)
	switch {
	case d < -time.Minute:
		return "in the future"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute", "minutes") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour", "hours") + " ago"
	default:
		return plural(int(d.Hours()/24), "day", "days") + " ago"
	}
}

func humanMinutes(minutes int) string {
	if minutes >= 60 {
		return fmt.Sprintf("%d h %02d min", minutes/60, minutes%60)
	}

	return fmt.Sprintf("%d min", minutes)
}

func latencyOrDash(ms *int) string {
	if ms == nil {
		return "—"
	}

	return fmt.Sprintf("%d ms", *ms)
}

func percentOrDash(value float64, ok bool) string {
	if !ok {
		return "—"
	}

	return fmt.Sprintf("%.2f%%", value)
}

func pct(part, total int) int {
	if total == 0 {
		return 0
	}

	return int(math.Round(100 * float64(part) / float64(total)))
}

func intPtrToFloat(v *int) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)

	return &f
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// plural склоняет существительное при числительном.
func plural(n int, one, many string) string {
	word := many
	if n == 1 || n == -1 {
		word = one
	}

	return fmt.Sprintf("%d %s", n, word)
}

// ---------------------------------------------------------------------------
// Обвязка страниц
// ---------------------------------------------------------------------------

// OverviewSub - подпись под заголовком сводки: сколько чего отслеживается.
func (p Presenter) OverviewSub(targets []entity.TargetSummary) string {
	if len(targets) == 0 {
		return "backend hasn't recorded a single target yet"
	}

	probes, containers := split(targets)

	return plural(len(probes), "uptime target", "uptime targets") +
		" and " + plural(len(containers), "container", "containers") + " on one host"
}

// AlertsSub - подпись под заголовком истории алертов.
func (p Presenter) AlertsSub() string {
	return "retention " + plural(int(p.Retention.Hours()/24), "day", "days") + " · newest first"
}

// ContainerNotFound - пустое состояние страницы контейнера, которого нет
// среди целей: имя могли опечатать, а могли забыть навесить метку.
func (p Presenter) ContainerNotFound(name string) Empty {
	return Empty{
		Mood:  Moods["watching"],
		Title: "Container not being watched",
		Note: "Argus isn't watching \"" + name + "\". Tag the container with the " +
			"argus.monitor=true label and the watcher will pick it up on its own.",
	}
}

// WaitingLayout - вся страница ожидания целиком.
//
// Считать счётчики не из чего, поэтому меню идёт без них - но уйти со
// страницы пользователь должен мочь.
func (p Presenter) WaitingLayout() Layout {
	return Layout{
		Title:       "Waiting for data",
		Sub:         "the dashboard is waiting for argus-backend to come up",
		PartialPath: "—",
		Nav: []NavItem{
			{Key: "overview", Label: "Overview", Href: "/"},
			{Key: "history", Label: "History", Href: "/history/alerts"},
		},
		Sidebar: Sidebar{
			Mood:   Moods["sleeping"],
			Legend: legend(Moods["sleeping"]),
			Build:  p.build(false),
			Host:   HostPanel{Note: "CPU, RAM, and disk aren't collected in this version"},
		},
		Content: WaitingForData(
			"argus-backend isn't responding right now. The page will refresh itself " +
				"once it comes up."),
	}
}
