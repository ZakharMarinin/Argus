package view

import (
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"argus-frontend/internal/domain/entity"
)

const containerFeedLimit = 20

type LogPulse struct {
	Count  int
	Status Status
	Tiles  []Tile
	Bars   []LogBar
	Note   string
	Skew   string
}

type LogBar struct {
	Title    string
	Segments []LogSegment
}

type LogSegment struct {
	Status Status
	Style  template.CSS
}

type LogFeed struct {
	URL        string
	PageURL    string
	Query      template.URL
	Chips      []Chip
	Rows       []LogRow
	ShowTarget bool
	Pager      *Pager
	AllHref    string
}

type LogRow struct {
	ID       int64
	Time     string
	Ago      string
	Target   string
	Href     string
	Severity string
	Message  string
	Status   Status
}

func LogMood(n int) (Mood, Status) {
	switch {
	case n == 0:
		return Moods["happy"], StatusUp
	case n <= 2:
		return Moods["watching"], StatusIdle
	case n < 5:
		return Moods["scared"], StatusWarn
	default:
		return Moods["angry"], StatusDown
	}
}

func (p Presenter) LogPulse(report entity.LogMatchReport) LogPulse {
	var urgent, warning, quiet int

	for _, m := range report.Matches {
		switch m.Severity {
		case entity.SeverityCritical, entity.SeverityError:
			urgent++
		case entity.SeverityWarning:
			warning++
		default:
			quiet++
		}
	}

	_, status := LogMood(len(report.Matches))

	pulse := LogPulse{
		Count:  len(report.Matches),
		Status: status,
		Tiles: []Tile{
			{Value: fmt.Sprint(urgent), Label: "critical / error", Status: countStatus(urgent, StatusDown)},
			{Value: fmt.Sprint(warning), Label: "warning", Status: countStatus(warning, StatusWarn)},
			{Value: fmt.Sprint(quiet), Label: "info / debug"},
		},
		Bars: logBars(report.Hourly),
		Note: "no matches recorded",
	}

	if len(report.Matches) > 0 {
		pulse.Note = "last match " + p.ago(report.Matches[0].CreatedAt)
	}

	if report.Future > 0 {
		pulse.Skew = plural(report.Future, "match is", "matches are") +
			" dated in the future and left out — check the clock of whoever wrote them"
	}

	return pulse
}

func logBars(points []entity.LogMatchPoint) []LogBar {
	peak := 0
	for _, pt := range points {
		peak = max(peak, pt.Urgent+pt.Warning+pt.Quiet)
	}

	bars := make([]LogBar, 0, len(points))

	for _, pt := range points {
		total := pt.Urgent + pt.Warning + pt.Quiet
		bar := LogBar{Title: fmt.Sprintf("%s · %s", pt.Hour.Format("15:00"), plural(total, "match", "matches"))}

		for _, seg := range []struct {
			n int
			s Status
		}{{pt.Quiet, StatusIdle}, {pt.Warning, StatusWarn}, {pt.Urgent, StatusDown}} {
			if seg.n > 0 {
				bar.Segments = append(bar.Segments, LogSegment{Status: seg.s, Style: styleHeight(100 * float64(seg.n) / float64(peak))})
			}
		}

		bars = append(bars, bar)
	}

	return bars
}

func countStatus(n int, s Status) Status {
	if n == 0 {
		return ""
	}

	return s
}

func (p Presenter) ContainerLogFeed(name string, matches []entity.LogMatch) LogFeed {
	if len(matches) > containerFeedLimit {
		matches = matches[:containerFeedLimit]
	}

	return LogFeed{
		URL:     "/partials/container/" + url.PathEscape(name) + "/logs",
		Rows:    p.logRows(matches, false),
		AllHref: "/history/logs?target=" + url.QueryEscape(name),
	}
}

func (p Presenter) logRows(matches []entity.LogMatch, link bool) []LogRow {
	rows := make([]LogRow, 0, len(matches))

	for _, m := range matches {
		status, _ := alertStatus(m.Severity)
		row := LogRow{
			ID:       m.ID,
			Time:     p.timestamp(m.CreatedAt),
			Ago:      p.ago(m.CreatedAt),
			Target:   m.TargetLabel,
			Severity: strings.ToUpper(m.Severity),
			Message:  m.Message,
			Status:   status,
		}

		if link && m.TargetKind == entity.KindContainer {
			row.Href = "/containers/" + url.PathEscape(m.TargetLabel)
		}

		rows = append(rows, row)
	}

	return rows
}

func LogMatchChartCard(target string) Chart {
	dataURL := "/api/log-matches"
	if target != "" {
		dataURL += "?target=" + url.QueryEscape(target)
	}

	return Chart{
		Title:   "Log matches · 24h",
		DataURL: dataURL,
		Legend: []LegendItem{
			{Label: "critical / error", Color: "down"},
			{Label: "warning", Color: "warn"},
			{Label: "info / debug", Color: "idle"},
		},
		EmptyNote: "No log matches in the last 24 hours",
	}
}

func (p Presenter) LogMatchChart(points []entity.LogMatchPoint) ChartData {
	data := ChartData{
		Labels:  make([]string, 0, len(points)),
		Stacked: true,
		Empty:   true,
		Series: []ChartSeries{
			{Label: "critical / error", Color: "down", Axis: "left", Style: "bar"},
			{Label: "warning", Color: "warn", Axis: "left", Style: "bar"},
			{Label: "info / debug", Color: "idle", Axis: "left", Style: "bar"},
		},
	}

	for _, pt := range points {
		data.Labels = append(data.Labels, pt.Hour.Format("15:00"))

		for i, n := range []int{pt.Urgent, pt.Warning, pt.Quiet} {
			v := float64(n)
			data.Series[i].Values = append(data.Series[i].Values, &v)

			if n > 0 {
				data.Empty = false
			}
		}
	}

	return data
}
