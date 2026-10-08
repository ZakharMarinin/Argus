package view

import (
	"fmt"
	"html/template"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"argus-frontend/internal/domain/entity"
)

const outagePageSize = 50

type Tab struct {
	Label  string
	Href   string
	Active bool
}

type Option struct {
	Label    string
	Href     string
	Selected bool
}

type Pager struct {
	PageURL    string
	PartialURL string
	Target     string
	Label      string
	HasNewer   bool
	Newer      template.URL
	HasOlder   bool
	Older      template.URL
	Live       bool
}

type OutagePage struct {
	Panel IncidentPanel
	Pager Pager
	Query template.URL
}

type HistoryPage struct {
	Tab        string
	Tabs       []Tab
	Chart      Chart
	Container  string
	Containers []Option
	Alerts     AlertPage
	Logs       LogFeed
	Outages    OutagePage
}

func HistoryTab(tab string) bool {
	return tab == "alerts" || tab == "logs" || tab == "outages"
}

func PageNumber(q url.Values) int {
	n, err := strconv.Atoi(q.Get("page"))
	if err != nil || n < 1 {
		return 1
	}

	return n
}

func HistoryQueryFor(q url.Values) entity.HistoryQuery {
	hq := entity.HistoryQuery{Kind: alertKind(q.Get("type")), Target: q.Get("target"), Page: PageNumber(q)}

	severity := q.Get("sev")
	if severity != "all" && slices.Contains(AlertSeverities, severity) {
		hq.Severity = severity
	}

	return hq
}

func (p Presenter) History(tab string, containers []string, q url.Values) HistoryPage {
	page := HistoryPage{Tab: tab}

	for _, t := range []struct{ key, label string }{
		{"alerts", "Alerts"}, {"logs", "Log matches"}, {"outages", "Outages"},
	} {
		page.Tabs = append(page.Tabs, Tab{Label: t.label, Href: "/history/" + t.key, Active: t.key == tab})
	}

	if tab == "logs" {
		target := q.Get("target")
		page.Chart = LogMatchChartCard(target)
		page.Container = firstNonEmpty(target, "all containers")
		page.Containers = append(page.Containers, containerOption("", "all containers", target, q))

		for _, name := range containers {
			page.Containers = append(page.Containers, containerOption(name, name, target, q))
		}
	}

	return page
}

func containerOption(value, label, current string, q url.Values) Option {
	next := withPage(q, 1)
	if value == "" {
		next.Del("target")
	} else {
		next.Set("target", value)
	}

	return Option{Label: label, Href: "/history/logs" + string(encode(next)), Selected: value == current}
}

func (p Presenter) HistoryAlerts(page entity.Page[entity.Alert], q url.Values) AlertPage {
	rows := make([]Alert, 0, len(page.Items))
	now := p.now()

	for _, a := range page.Items {
		status, mood := alertStatus(a.Severity)

		rows = append(rows, Alert{
			Time:     p.timestamp(a.CreatedAt),
			Message:  a.Message,
			Target:   a.TargetLabel,
			Type:     alertType(a.TargetKind),
			Severity: strings.ToUpper(a.Severity),
			State:    telegramState(a.SentToTelegram),
			Status:   status,
			MoodSrc:  Moods[mood].Src,
			Alarm:    urgent(a.Severity) && now.Sub(a.CreatedAt) < time.Hour,
		})
	}

	out := AlertPage{
		Types:      chipsFor(AlertTypes, "type", q),
		Severities: chipsFor(AlertSeverities, "sev", q),
		Alerts:     rows,
		Query:      encode(q),
		Pager:      pagerFor("alerts", "#history-alerts", page.Total, page.Skip, len(rows), q),
	}

	if page.Total == 0 {
		out.Empty = fmt.Sprintf("kept for %d days", int(p.Retention.Hours()/24))
	}

	return out
}

func (p Presenter) HistoryLogs(page entity.Page[entity.LogMatch], q url.Values) LogFeed {
	return LogFeed{
		URL:        "/partials/history/logs",
		PageURL:    "/history/logs",
		Query:      encode(q),
		Chips:      chipsFor(AlertSeverities, "sev", q),
		Rows:       p.logRows(page.Items, true),
		ShowTarget: q.Get("target") == "",
		Pager:      new(pagerFor("logs", "#log-feed", page.Total, page.Skip, len(page.Items), q)),
	}
}

func (p Presenter) HistoryOutages(incidents []entity.Incident, stats entity.IncidentStats, q url.Values) OutagePage {
	skip := (PageNumber(q) - 1) * outagePageSize
	end := min(skip+outagePageSize, len(incidents))

	var shown []entity.Incident
	if skip < len(incidents) {
		shown = incidents[skip:end]
	}

	return OutagePage{
		Panel: p.Incidents(shown, stats),
		Pager: pagerFor("outages", "#history-outages", len(incidents), skip, len(shown), q),
		Query: encode(q),
	}
}

func pagerFor(tab, target string, total, skip, shown int, q url.Values) Pager {
	page := PageNumber(q)
	pg := Pager{
		PageURL:    "/history/" + tab,
		PartialURL: "/partials/history/" + tab,
		Target:     target,
		Live:       page == 1,
	}

	if shown > 0 {
		pg.Label = fmt.Sprintf("%d–%d of %d", skip+1, skip+shown, total)
	}

	if page > 1 {
		pg.HasNewer = true
		pg.Newer = encode(withPage(q, page-1))
	}

	if skip+shown < total {
		pg.HasOlder = true
		pg.Older = encode(withPage(q, page+1))
	}

	return pg
}

func withPage(q url.Values, page int) url.Values {
	out := maps.Clone(q)
	if out == nil {
		out = url.Values{}
	}

	if page <= 1 {
		out.Del("page")
	} else {
		out.Set("page", strconv.Itoa(page))
	}

	return out
}

func chipsFor(values []string, key string, q url.Values) []Chip {
	active := q.Get(key)
	if active == "" {
		active = "all"
	}

	out := make([]Chip, 0, len(values))

	for _, v := range values {
		next := withPage(q, 1)
		if v == "all" {
			next.Del(key)
		} else {
			next.Set(key, v)
		}

		out = append(out, Chip{Label: v, Href: encode(next), Active: v == active})
	}

	return out
}

func encode(q url.Values) template.URL {
	if len(q) == 0 {
		return ""
	}

	return template.URL("?" + q.Encode()) //nolint:gosec // G203: url.Values.Encode escapes everything itself
}
