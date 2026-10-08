package view

import (
	"fmt"
	"net/url"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// timelineWindow is the period of the outage bar. The same as for incidents in
// usecase: the bar and the list under it must talk about the same thing.
const timelineWindow = 24 * time.Hour

// Incidents lays out outage intervals for the overview block.
func (p Presenter) Incidents(incidents []entity.Incident, stats entity.IncidentStats) IncidentPanel {
	panel := IncidentPanel{
		Note:  incidentNote(stats),
		Stats: p.incidentStats(stats),
		Rows:  make([]IncidentRow, 0, len(incidents)),
	}

	for _, i := range incidents {
		panel.Rows = append(panel.Rows, p.incidentRow(i))
	}

	return panel
}

// incidentRows lays out intervals as a list, newest first.
func (p Presenter) incidentRows(incidents []entity.Incident) []IncidentRow {
	sorted := append([]entity.Incident(nil), incidents...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].StartedAt.After(sorted[j].StartedAt)
	})

	rows := make([]IncidentRow, 0, len(sorted))
	for _, i := range sorted {
		rows = append(rows, p.incidentRow(i))
	}

	return rows
}

func (p Presenter) incidentRow(i entity.Incident) IncidentRow {
	row := IncidentRow{
		Name:     i.TargetLabel,
		Kind:     alertType(i.Kind),
		Status:   incidentStatus(i.State),
		Label:    incidentLabel(i.State),
		Cause:    i.Cause,
		Started:  p.timestamp(i.StartedAt),
		Duration: durationOrDash(i),
	}

	if i.Kind == entity.KindContainer {
		row.Href = "/containers/" + url.PathEscape(i.TargetLabel)
	}

	return row
}

// incidentStatus is the colour of a row.
//
// An unclosed interval is idle, not down: it doesn't mean "down right now", it
// means "we don't know". Same meaning as StatusIdle on the other screens (spec §9).
func incidentStatus(state entity.IncidentState) Status {
	switch state {
	case entity.IncidentClosed:
		return StatusWarn
	case entity.IncidentOpen:
		return StatusDown
	case entity.IncidentUnclosed:
		return StatusIdle
	default:
		return StatusIdle
	}
}

func incidentLabel(state entity.IncidentState) string {
	switch state {
	case entity.IncidentClosed:
		return "recovered"
	case entity.IncidentOpen:
		return "still down"
	case entity.IncidentUnclosed:
		return "return not recorded"
	default:
		return "unknown"
	}
}

// durationOrDash returns the outage duration or a dash.
//
// The dash appears where the end of the interval isn't in the data: argus-backend
// doesn't record the container's return, and "0 min" or a growing counter would
// be made up in place of a gap.
func durationOrDash(i entity.Incident) string {
	d, ok := i.Duration()
	if !ok {
		return "—"
	}

	return humanDuration(d)
}

// humanDuration formats a duration the same way as the other intervals on the
// dashboard, but with seconds: a 40-second outage must not read as "0 min".
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	default:
		return humanMinutes(int(d.Minutes()))
	}
}

func incidentNote(stats entity.IncidentStats) string {
	if stats.Count == 0 {
		return ""
	}

	note := plural(stats.Count, "outage", "outages") + " in the last 24h"
	if stats.Unclosed > 0 {
		note += fmt.Sprintf(" · %d without a recorded return", stats.Unclosed)
	}

	return note
}

// incidentStats builds the tiles above the list.
//
// Total downtime is counted only over measurable intervals, so a tile with
// unclosed ones sits next to it: without it the total would read as the full picture.
func (p Presenter) incidentStats(stats entity.IncidentStats) []MetaTile {
	if stats.Count == 0 {
		return nil
	}

	measured := stats.Count - stats.Unclosed

	// There are three tiles, to fit the existing meta-grid. Unclosed intervals
	// don't get a tile of their own: the block caption covers them, otherwise
	// a fourth tile would break the row for the sake of one number.
	return []MetaTile{
		{Label: "OUTAGES", Value: fmt.Sprintf("%d", stats.Count), Status: nonZero(stats.Count)},
		{Label: "MEASURED DOWNTIME", Value: measuredOrDash(stats.Total, measured),
			Status: nonZero(int(stats.Total))},
		{Label: "LONGEST", Value: measuredOrDash(stats.Longest, measured),
			Status: nonZero(int(stats.Longest))},
	}
}

// measuredOrDash hides the total when not a single interval could be measured:
// "0 s" would then mean "there was no downtime", which is a different claim.
func measuredOrDash(d time.Duration, measured int) string {
	if measured <= 0 {
		return "—"
	}

	return humanDuration(d)
}

// ---------------------------------------------------------------------------
// Bar on the container page
// ---------------------------------------------------------------------------

// Timeline lays out a container's outage intervals on a 24-hour bar.
//
// The bar's background is idle, not up: Docker events only report failures,
// and a stretch without them doesn't prove the container was running. Hint says
// so explicitly, so the bar doesn't read as an uptime chart.
func (p Presenter) Timeline(partialBase string, incidents []entity.Incident) Timeline {
	tl := Timeline{
		URL:   partialBase + "/timeline",
		Title: "Downtime over 24h",
		Hint:  "gaps are hours without Docker events — not proof the container was up",
	}

	from := p.now().Add(-timelineWindow)

	for _, i := range incidents {
		band, ok := p.band(i, from)
		if ok {
			tl.Bands = append(tl.Bands, band)
		}
	}

	if len(tl.Bands) == 0 {
		tl.Note = "no outages recorded in the last 24h"
	}

	tl.Legend = timelineLegend(tl.Bands)
	tl.Rows = p.incidentRows(incidents)

	return tl
}

// timelineLegend lists only what is actually on the bar.
//
// A legend promising a segment that isn't there reads as a status: "return not
// recorded" under a bar without a single such mark looks like a claim about the
// container, although it is just a legend entry.
func timelineLegend(bands []TimelineBand) []LegendItem {
	var measured, unclosed bool

	for _, b := range bands {
		if b.Status == StatusIdle {
			unclosed = true

			continue
		}

		measured = true
	}

	legend := make([]LegendItem, 0, 2)

	if measured {
		legend = append(legend, LegendItem{Label: "measured outage", Color: "down"})
	}

	if unclosed {
		legend = append(legend, LegendItem{Label: "return not recorded", Color: "idle"})
	}

	return legend
}

// band turns an interval into a segment of the bar.
//
// An unclosed interval has no end, and drawing it up to the right edge would
// claim the container is still down. So it is drawn as a mark of minimal
// width at the point of failure: "went down here, unknown after that".
func (p Presenter) band(i entity.Incident, from time.Time) (TimelineBand, bool) {
	const (
		window       = float64(timelineWindow)
		minWidthPct  = 0.6
		markWidthPct = 0.9
	)

	now := p.now()

	start := i.StartedAt
	if start.Before(from) {
		start = from
	}

	if start.After(now) {
		return TimelineBand{}, false
	}

	left := 100 * float64(start.Sub(from)) / window

	if i.State == entity.IncidentUnclosed {
		return TimelineBand{
			Status: StatusIdle,
			Style:  styleSpan(left, markWidthPct),
			Title:  i.Cause + " at " + p.timestamp(i.StartedAt) + " · return not recorded",
		}, true
	}

	end := i.LastKnownAt
	if i.EndedAt != nil {
		end = *i.EndedAt
	}

	if end.After(now) {
		end = now
	}

	width := 100 * float64(end.Sub(start)) / window
	if width < minWidthPct {
		width = minWidthPct
	}

	return TimelineBand{
		Status: StatusDown,
		Style:  styleSpan(left, width),
		Title:  i.Cause + " at " + p.timestamp(i.StartedAt) + " · " + durationOrDash(i),
	}, true
}
