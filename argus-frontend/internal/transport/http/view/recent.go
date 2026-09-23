package view

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"argus-frontend/internal/domain/entity"
)

const recentLimit = 8

type RecentPanel struct {
	Rows []RecentRow
}

type RecentRow struct {
	Time    string
	Ago     string
	Kind    string
	Target  string
	Href    string
	Text    string
	TabHref string
	Status  Status
	at      time.Time
}

func (p Presenter) Recent(r entity.Recent) RecentPanel {
	var rows []RecentRow

	for _, i := range r.Incidents {
		row := p.recentRow(i.StartedAt, "outage", i.TargetLabel, i.Kind, "/history/outages", incidentStatus(i.State))
		row.Text = i.Cause + " · " + incidentLabel(i.State)

		if d, ok := i.Duration(); ok {
			row.Text += " · " + humanDuration(d)
		}

		rows = append(rows, row)
	}

	for _, a := range r.Alerts {
		if !loud(a.Severity) {
			continue
		}

		status, _ := alertStatus(a.Severity)
		row := p.recentRow(a.CreatedAt, "alert", a.TargetLabel, a.TargetKind, "/history/alerts", status)
		row.Text = strings.ToUpper(a.Severity) + " · " + a.Message
		rows = append(rows, row)
	}

	for _, m := range r.LogMatches {
		if !loud(m.Severity) {
			continue
		}

		status, _ := alertStatus(m.Severity)
		row := p.recentRow(m.CreatedAt, "log", m.TargetLabel, m.TargetKind, "/history/logs", status)
		row.Text = strings.ToUpper(m.Severity) + " · " + m.Message
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.After(rows[j].at) })

	if len(rows) > recentLimit {
		rows = rows[:recentLimit]
	}

	return RecentPanel{Rows: rows}
}

func (p Presenter) recentRow(at time.Time, kind, target string, targetKind entity.Kind, tab string, status Status) RecentRow {
	row := RecentRow{
		Time:    p.timestamp(at),
		Ago:     p.ago(at),
		Kind:    kind,
		Target:  target,
		TabHref: tab,
		Status:  status,
		at:      at,
	}

	if targetKind == entity.KindContainer {
		row.Href = "/containers/" + url.PathEscape(target)
	}

	return row
}

func loud(severity string) bool {
	return severity == entity.SeverityCritical || severity == entity.SeverityError || severity == entity.SeverityWarning
}
