package view

import (
	"fmt"
	"net/url"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// timelineWindow - период полосы простоев. Тот же, что у инцидентов в
// usecase: полоса и список под ней должны говорить об одном и том же.
const timelineWindow = 24 * time.Hour

// Incidents раскладывает интервалы простоя по блоку сводки.
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

// incidentRows раскладывает интервалы списком, новыми сверху.
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

// incidentStatus - цвет строки.
//
// Незакрытый интервал идёт idle, а не down: это не «лежит сейчас», это «мы не
// знаем». Тот же смысл, что у StatusIdle на остальных экранах (ТЗ §9).
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

// durationOrDash - длительность простоя или прочерк.
//
// Прочерк стоит там, где конца интервала в данных нет: argus-backend не
// записывает возвращение контейнера, и «0 min» или растущий счётчик были бы
// выдумкой на месте пробела.
func durationOrDash(i entity.Incident) string {
	d, ok := i.Duration()
	if !ok {
		return "—"
	}

	return humanDuration(d)
}

// humanDuration - длительность в том же виде, что и остальные интервалы на
// дашборде, но с секундами: простой в 40 секунд не должен читаться как «0 min».
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

// incidentStats - плитки над списком.
//
// Суммарный простой считается только по измеримым интервалам, поэтому рядом
// стоит плитка незакрытых: без неё сумма читалась бы как полная картина.
func (p Presenter) incidentStats(stats entity.IncidentStats) []MetaTile {
	if stats.Count == 0 {
		return nil
	}

	measured := stats.Count - stats.Unclosed

	// Плиток три, под существующую сетку meta-grid. Незакрытые интервалы
	// своей плитки не получают - про них говорит подпись блока, иначе
	// четвёртая плитка ломала бы ряд ради одного числа.
	return []MetaTile{
		{Label: "OUTAGES", Value: fmt.Sprintf("%d", stats.Count), Status: nonZero(stats.Count)},
		{Label: "MEASURED DOWNTIME", Value: measuredOrDash(stats.Total, measured),
			Status: nonZero(int(stats.Total))},
		{Label: "LONGEST", Value: measuredOrDash(stats.Longest, measured),
			Status: nonZero(int(stats.Longest))},
	}
}

// measuredOrDash прячет сумму, когда измерить не удалось ни один интервал:
// «0 s» в этом случае означал бы «простоя не было», а это другое утверждение.
func measuredOrDash(d time.Duration, measured int) string {
	if measured <= 0 {
		return "—"
	}

	return humanDuration(d)
}

// ---------------------------------------------------------------------------
// Полоса на странице контейнера
// ---------------------------------------------------------------------------

// Timeline раскладывает интервалы простоя контейнера в полосу за сутки.
//
// Фон полосы - idle, а не up: события Docker говорят только о падениях, и
// промежуток без них не доказывает, что контейнер работал. Hint это называет
// прямо, чтобы полоса не читалась как график аптайма.
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

// timelineLegend перечисляет только то, что на полосе действительно есть.
//
// Легенда, обещающая отрезок, которого нет, читается как статус: «return not
// recorded» под полосой без единой такой засечки выглядит утверждением про
// контейнер, хотя это всего лишь условное обозначение.
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

// band переводит интервал в отрезок полосы.
//
// Незакрытый интервал не имеет конца, и рисовать его до правого края значило
// бы утверждать, что контейнер лежит до сих пор. Поэтому он рисуется меткой
// минимальной ширины в точке падения - «здесь упал, дальше неизвестно».
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
