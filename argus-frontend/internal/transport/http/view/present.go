package view

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// Presenter lays out store domain structures as view models.
//
// There is one rule: show only what the backend actually has. Where there is
// no value, there is a dash or an empty state. Substituting a plausible number
// for a missing one is not allowed, otherwise the monitoring dashboard starts
// lying.
type Presenter struct {
	// Build is the version string in the sidebar footer.
	Build string
	// Retention is the retention period from the config; it explains an empty history.
	Retention time.Duration
	// Now is replaced in tests.
	Now func() time.Time
}

func (p Presenter) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}

	return time.Now()
}

// ---------------------------------------------------------------------------
// Statuses
// ---------------------------------------------------------------------------

// statusOf turns a target's backend state into a view state.
//
// The backend has only up/down/unknown. The yellow warn isn't made up from
// thresholds: it appears where there is a fact behind it, computed in
// internal/store: a container that restarted within the last hour, or a
// probe that had at least one failure in 24 hours.
func statusOf(s entity.Status) Status {
	switch s {
	case entity.StatusUp:
		return StatusUp
	case entity.StatusDown:
		return StatusDown
	case entity.StatusUnknown:
		return StatusIdle
	default:
		// An unknown value from the backend is also "state unknown",
		// not a silent "all good".

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
// Overview
// ---------------------------------------------------------------------------

// Overview builds the main page from targets, alert counters and
// log matches over 24 hours.
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
		Blocks: OverviewBlocks(),
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
		case entity.KindHost:
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
	outages := totalOutages(probes, containers)
	h.Tiles = []Tile{
		{
			Value: containerCount(running, len(containers)),
			Label: "containers running",
		},
		{
			Value:  percentOrDash(uptime, uptimeOK),
			Label:  p.uptimeLabel(probes),
			Status: uptimeStatus(uptime, uptimeOK),
		},
		{
			// Проверки за сутки были (uptimeOK), значит отсутствие интервалов —
			// измеренный факт, а не пустота в данных.
			Value:  downtimeOrDash(outages, uptimeOK),
			Label:  downtimeLabel(outages),
			Status: downtimeStatus(outages),
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

// aggregateUptime computes availability across all uptime targets at once: as a
// sum of checks, not an average of averages, otherwise a rarely checked target
// would weigh as much as a frequently checked one.
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

// fullDaySlack — допуск, с которым история считается полной сутками: проверки
// идут с интервалом, и самая старая из окна 24 часов всегда чуть моложе его начала.
const fullDaySlack = time.Hour

// uptimeLabel подписывает процент реальным периодом наблюдения.
//
// Процент считается только по сделанным проверкам, но подпись «24h» у цели,
// которую проверяют восемь минут, обещала бы сутки истории, которых нет.
func (p Presenter) uptimeLabel(probes []entity.TargetSummary) string {
	var first *time.Time
	for _, t := range probes {
		if t.Probe == nil || t.Probe.FirstCheckedAt == nil {
			continue
		}
		if first == nil || t.Probe.FirstCheckedAt.Before(*first) {
			first = t.Probe.FirstCheckedAt
		}
	}

	if first == nil {
		return "uptime, 24h"
	}

	observed := p.now().Sub(*first)
	if observed >= 24*time.Hour-fullDaySlack {
		return "uptime, 24h"
	}

	return "uptime, last " + humanDuration(observed)
}

// totalOutages складывает интервалы простоя всех целей за 24 часа — те же,
// что на странице Outages, поэтому итог совпадает с её MEASURED DOWNTIME.
func totalOutages(probes, containers []entity.TargetSummary) entity.IncidentStats {
	var total entity.IncidentStats
	for _, t := range append(append([]entity.TargetSummary{}, probes...), containers...) {
		total.Count += t.Outages24h.Count
		total.Unclosed += t.Outages24h.Unclosed
		total.Total += t.Outages24h.Total
		if t.Outages24h.Longest > total.Longest {
			total.Longest = t.Outages24h.Longest
		}
	}

	return total
}

func downtimeStatus(outages entity.IncidentStats) Status {
	switch {
	case outages.Count > outages.Unclosed:
		return StatusDown
	case outages.Unclosed > 0:
		return StatusIdle
	default:
		return ""
	}
}

// downtimeOrDash показывает измеренный простой за 24 часа.
//
// Это сумма реальных интервалов между наблюдениями, а не доля упавших проверок,
// растянутая на сутки: на истории в восемь минут та давала «9h 36min».
// Прочерк — когда измерять нечем: проверок не было (observed == false) или ни
// один интервал не имеет конца. «0 min» — только когда проверки шли и простоя
// в них не нашлось.
func downtimeOrDash(outages entity.IncidentStats, observed bool) string {
	if outages.Count == 0 {
		if !observed {
			return "—"
		}

		return "0 min"
	}

	return measuredOrDash(outages.Total, outages.Count-outages.Unclosed)
}

// downtimeLabel повторяет подпись страницы Outages и называет интервалы без
// записанного возвращения: без этого сумма читалась бы как полная картина.
func downtimeLabel(outages entity.IncidentStats) string {
	label := "measured downtime, 24h"
	if outages.Unclosed > 0 {
		label += fmt.Sprintf(" · %d unmeasured", outages.Unclosed)
	}

	return label
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
// Uptime targets
// ---------------------------------------------------------------------------

func (p Presenter) targets(probes []entity.TargetSummary) []Target {
	out := make([]Target, 0, len(probes))
	for _, t := range probes {
		status := statusOf(t.Status)
		stats := t.Probe
		if stats == nil {
			stats = &entity.ProbeStats{}
		}

		// Flaky (at least one failure in 24 hours for a formally live target) is also
		// a reason to highlight the row, even though the backend counts it as up.
		if status == StatusUp && stats.Checks24h > 0 && stats.Successes24h < stats.Checks24h {
			status = StatusWarn
		}

		target := Target{
			ID:     t.ID,
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

	// Same rule as the container grid: what needs attention comes first, so
	// that a down target isn't somewhere in the middle of an alphabetical list.
	// The sort is stable: within one status the backend's order stays.
	sort.SliceStable(out, func(i, j int) bool { return attention(out[i].Status) > attention(out[j].Status) })

	return out
}

func attention(s Status) int {
	switch s {
	case StatusDown:
		return 3
	case StatusWarn:
		return 2
	case StatusIdle:
		return 1
	case StatusUp:
		return 0
	default:
		return 0
	}
}

func warnAwareLabel(status Status, base string) string {
	if status == StatusWarn && base == "up" {
		return "degraded"
	}

	return base
}

// strip turns check outcomes into the bars of the strip. The strip's length equals
// the number of real checks: missing bars aren't filled in.
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
// Containers
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
		if status == StatusUp && stats.Crashes > 0 {
			status = StatusWarn
		}
		label := warnAwareLabel(status, labelOf(c.Status))
		_, logStatus := LogMood(logs[c.ID])

		switch {
		case c.Status == entity.StatusDown:
			rank[c.Label] = 3
		case stats.Crashes > 0:
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
				{Label: "crashes, 15m", Value: fmt.Sprint(stats.Crashes), Status: nonZero(stats.Crashes)},
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
	if s.Crashes > 0 && s.CrashesClearAt != nil {
		note += " · clears at " + s.CrashesClearAt.Local().Format("15:04")
	}

	return note
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

// AlertTypes and AlertSeverities are the filter values.
//
// The type is the kind of target: the backend has no column for an alert type.
// The severities are all five levels of the backend's entity.Severity, from urgent to quiet.
var (
	AlertTypes      = []string{"all", "uptime", "container", "host"}
	AlertSeverities = []string{
		"all",
		entity.SeverityCritical, entity.SeverityError, entity.SeverityWarning,
		entity.SeverityInfo, entity.SeverityDebug,
	}
)

// alertKind turns a filter value into a kind of target.
func alertKind(filter string) entity.Kind {
	switch filter {
	case "uptime":
		return entity.KindProbe
	case "container":
		return entity.KindContainer
	case "host":
		return entity.KindHost
	default:
		return ""
	}
}

// alertStatus turns an alert's severity into a status and a mascot mood.
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
	case entity.KindHost:
		return "host"
	default:
		return "—"
	}
}

// deliveryState shows the only lifecycle indicator an alert has: what
// happened to its message. Cooldown is a deliberate suppression, not a
// failure, so it gets its own quiet look instead of an error colour. A value
// the dashboard doesn't know yet is shown as is rather than guessed at.
func deliveryState(status string) (label, class string) {
	switch status {
	case entity.AlertStatusSent:
		return "sent to Telegram", "alert-row__state--sent"
	case entity.AlertStatusCooldown:
		return "suppressed (cooldown)", "alert-row__state--cooldown"
	case entity.AlertStatusNotSent:
		return "not sent", ""
	case "":
		return "delivery unknown", ""
	default:
		return status, ""
	}
}

// ---------------------------------------------------------------------------
// Container details
// ---------------------------------------------------------------------------

// Detail builds the page of a single container.
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

// hero describes in words the same thing the tiles next to it show.
func (p Presenter) hero(t entity.TargetSummary, s entity.ContainerStats) Hero {
	switch {
	case t.Status == entity.StatusDown:
		return Hero{
			Badge:   "CRITICAL · CONTAINER DOWN",
			Title:   "Container stopped",
			Rule:    eventRule(s),
			MoodSrc: Moods["angry"].Src,
			Status:  StatusDown,
		}
	case t.Status == entity.StatusUnknown:
		return Hero{
			Badge:   "UNKNOWN · NO DATA",
			Title:   "Collector can't see the container",
			Rule:    eventRule(s),
			MoodSrc: Moods["sleeping"].Src,
			Status:  StatusIdle,
		}
	case s.Crashes > 0:
		return Hero{
			Badge:   "WARNING · CRASHED",
			Title:   fmt.Sprintf("Came back after %s in the last 15 minutes", plural(s.Crashes, "crash", "crashes")),
			Rule:    eventRule(s),
			MoodSrc: Moods["scared"].Src,
			Status:  StatusWarn,
		}
	default:
		return Hero{
			Badge:   "RUNNING · no issues",
			Title:   "Container is running normally",
			Rule:    eventRule(s),
			MoodSrc: Moods["happy"].Src,
			Status:  StatusUp,
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
		{Label: "Crashes, 15m", Value: fmt.Sprint(s.Crashes), Status: nonZero(s.Crashes)},
		{Label: "Log matches, 24h", Value: fmt.Sprint(logMatches), Status: logStatus},
		{Label: "Updated", Value: p.ago(t.UpdatedAt)},
	}
}

// events is the history of Docker events. A death is red, a return is green, a
// restart is yellow, everything else is neutral.
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
// Charts
// ---------------------------------------------------------------------------

// LatencyChartCard is the chart card on the overview.
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

// ActivityChartCard is the chart card on the container page.
//
// Nothing pings a container, it has no latency and can't have any, so this
// shows Docker events and log matches.
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

// LatencyChart turns the hourly series into data for Chart.js.
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
		data.Series[0].Values = append(data.Series[0].Values, pt.P95MS)
		data.Series[1].Values = append(data.Series[1].Values, pt.UptimePct)
		if pt.P95MS != nil || pt.UptimePct != nil {
			empty = false
		}
	}
	data.Empty = empty

	return data
}

// ActivityChart turns a container's hourly activity into chart data.
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
// Page scaffolding
// ---------------------------------------------------------------------------

// Nav builds the top menu of the sidebar. Containers don't go here: their list
// lives in a separate collapsible block (see containerNav).
func (p Presenter) Nav(targets []entity.TargetSummary, counts entity.AlertCounts) []NavItem {
	return []NavItem{
		{Key: "overview", Label: "Overview", Href: "/", Badge: badge(len(targets))},
		{Key: "history", Label: "History", Href: "/history/alerts", Badge: badge(counts.Day)},
	}
}

// containerNav is the list of watched containers for the sidebar.
//
// Those that need attention come first: in the sidebar what matters is not the
// alphabet but what to look at first. Within each group, the alphabetical order of
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
		if t.Status == entity.StatusDown || (t.Container != nil && t.Container.Crashes > 0) {
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

// Sidebar builds the persistent part of the sidebar.
func (p Presenter) Sidebar(targets []entity.TargetSummary, counts entity.AlertCounts, logs int, host HostPanel) Sidebar {
	mood, _ := p.mood(targets, counts, logs)

	return Sidebar{
		Mood:       mood,
		Legend:     legend(mood),
		Build:      p.build(true),
		Containers: p.containerNav(targets),
		Host:       host,
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
		if t.Container != nil && t.Container.Crashes > 0 {
			signals = append(signals, moodSignal{"scared", t.Label + " crashed in the last 15 minutes"})
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
// Formatting
// ---------------------------------------------------------------------------

var months = [...]string{
	"jan", "feb", "mar", "apr", "may", "jun",
	"jul", "aug", "sep", "oct", "nov", "dec",
}

// timestamp formats "05 sep 14:22". No year: history doesn't live longer than retention.
func (p Presenter) timestamp(t time.Time) string {
	local := t.Local()

	return fmt.Sprintf("%02d %s %02d:%02d",
		local.Day(), months[local.Month()-1], local.Hour(), local.Minute())
}

// ago formats "3 minutes ago". For recent events this reads faster than absolute time.
//
// A zero time isn't "very long ago" but "unknown": the backend may return
// time.Time{} for an event the watcher recorded without setting a timestamp.
// Computing a difference from it and printing "106751 days ago" would be far
// less honest than a dash.
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

func latencyOrDash(ms *float64) string {
	if ms == nil {
		return "—"
	}

	if *ms < 10 {
		return fmt.Sprintf("%.2g ms", *ms)
	}

	return fmt.Sprintf("%.0f ms", *ms)
}

func percentOrDash(value float64, ok bool) string {
	if !ok {
		return "—"
	}

	if value >= 100 {
		return "100%"
	}

	return fmt.Sprintf("%.2f%%", math.Floor(value*100+1e-9)/100)
}

func pct(part, total int) int {
	if total == 0 {
		return 0
	}

	return int(math.Round(100 * float64(part) / float64(total)))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// plural picks the singular or plural form of a noun for a number.
func plural(n int, one, many string) string {
	word := many
	if n == 1 || n == -1 {
		word = one
	}

	return fmt.Sprintf("%d %s", n, word)
}

// ---------------------------------------------------------------------------
// Page scaffolding
// ---------------------------------------------------------------------------

// OverviewSub is the caption under the overview heading: how many of what is watched.
func (p Presenter) OverviewSub(targets []entity.TargetSummary) string {
	if len(targets) == 0 {
		return "backend hasn't recorded a single target yet"
	}

	probes, containers := split(targets)

	return plural(len(probes), "uptime target", "uptime targets") +
		" and " + plural(len(containers), "container", "containers") + " on one host"
}

// AlertsSub is the caption under the alert history heading.
func (p Presenter) AlertsSub() string {
	return "retention " + plural(int(p.Retention.Hours()/24), "day", "days") + " · newest first"
}

// ContainerNotFound is the empty state of the page for a container that isn't
// among the targets: the name may be mistyped, or the label may be missing.
func (p Presenter) ContainerNotFound(name string) Empty {
	return Empty{
		Mood:  Moods["watching"],
		Title: "Container not being watched",
		Note: "Argus isn't watching \"" + name + "\". Tag the container with the " +
			"argus.monitor=true label and the watcher will pick it up on its own.",
	}
}

// WaitingLayout is the whole waiting page.
//
// There is nothing to compute counters from, so the menu goes without them, but
// the user must still be able to leave the page.
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
			Host:   HostPanel{Note: hostUnreachable},
		},
		Content: WaitingForData(
			"argus-backend isn't responding right now. The page will refresh itself " +
				"once it comes up."),
	}
}

const (
	hostUnreachable = "no answer from the collector"
	hostUnsupported = "collector doesn't report host metrics, update argus-backend"
)

func (p Presenter) HostPanel(h entity.Host, err error) HostPanel {
	switch {
	case errors.Is(err, entity.ErrNotFound):
		return HostPanel{Note: hostUnsupported}
	case err != nil:
		return HostPanel{Note: hostUnreachable}
	}

	return HostPanel{
		Note: h.Hostname,
		Meters: []Meter{
			hostMeter("CPU", h.CPU, func(m entity.HostMetric) string { return fmt.Sprintf("%.0f%%", m.Percent) }),
			hostMeter("RAM", h.Memory, bytesOfTotal),
			hostMeter("Disk", h.Disk, bytesOfTotal),
		},
	}
}

func hostMeter(label string, m *entity.HostMetric, value func(entity.HostMetric) string) Meter {
	if m == nil {
		return Meter{Label: label, Value: "—", Status: StatusIdle}
	}

	status := StatusUp

	switch m.Level {
	case entity.HostLevelWarning:
		status = StatusWarn
	case entity.HostLevelCritical:
		status = StatusDown
	}

	return Meter{Label: label, Value: value(*m), Percent: int(math.Round(m.Percent)), Status: status}
}

func bytesOfTotal(m entity.HostMetric) string {
	return gib(m.Used) + " / " + gib(m.Total) + " GiB"
}

func gib(b uint64) string {
	v := float64(b) / (1 << 30)
	if v < 10 {
		return fmt.Sprintf("%.1f", v)
	}

	return fmt.Sprintf("%.0f", v)
}
