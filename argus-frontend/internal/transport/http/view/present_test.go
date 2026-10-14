package view

import (
	"strings"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
)

// TestDeliveryState: each backend status gets its own wording, cooldown is
// told apart from "sent", and a value the dashboard doesn't know is shown as
// is instead of being passed off as one of the known ones.
func TestDeliveryState(t *testing.T) {
	cases := []struct {
		status    string
		wantLabel string
		wantClass string
	}{
		{entity.AlertStatusSent, "sent to Telegram", "alert-row__state--sent"},
		{entity.AlertStatusCooldown, "suppressed (cooldown)", "alert-row__state--cooldown"},
		{entity.AlertStatusNotSent, "not sent", ""},
		{"queued", "queued", ""},
		{"", "delivery unknown", ""},
	}

	for _, c := range cases {
		label, class := deliveryState(c.status)
		if label != c.wantLabel || class != c.wantClass {
			t.Errorf("deliveryState(%q) = %q, %q; want %q, %q", c.status, label, class, c.wantLabel, c.wantClass)
		}
	}
}

func TestPercentOrDash(t *testing.T) {
	cases := []struct {
		value float64
		ok    bool
		want  string
	}{
		{100, true, "100%"},
		{99.996, true, "99.99%"},
		{99.99, true, "99.99%"},
		{33.333, true, "33.33%"},
		{0, true, "0.00%"},
		{0, false, "—"},
	}

	for _, c := range cases {
		got := percentOrDash(c.value, c.ok)
		if got != c.want {
			t.Errorf("percentOrDash(%v, %v) = %q, want %q", c.value, c.ok, got, c.want)
		}
	}
}

// shortHistory — данные из ARG-28: цель проверяют восемь минут, упали 2 из 5
// проверок, Outages насчитала один измеренный интервал в 2 минуты.
func shortHistory(now time.Time) []entity.TargetSummary {
	return []entity.TargetSummary{{
		ID: "t1", Kind: entity.KindProbe, Label: "qa-web", Status: entity.StatusUp,
		Probe: &entity.ProbeStats{
			Checks24h: 5, Successes24h: 3,
			FirstCheckedAt: new(now.Add(-8 * time.Minute)),
		},
		Outages24h: entity.IncidentStats{Count: 1, Total: 2 * time.Minute, Longest: 2 * time.Minute},
	}}
}

func summaryTiles(p Presenter, targets []entity.TargetSummary) []Tile {
	return p.Overview(targets, entity.AlertCounts{}, entity.LogMatchReport{}).Health.Tiles
}

// TestDowntimeTileShortHistory: при истории короче суток плитка показывает
// измеренный простой, а не долю упавших проверок, растянутую на 24 часа
// («9h 36min»), и совпадает с MEASURED DOWNTIME страницы Outages.
func TestDowntimeTileShortHistory(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	p := Presenter{Now: func() time.Time { return now }}
	targets := shortHistory(now)

	tiles := summaryTiles(p, targets)
	uptime, downtime := tiles[1], tiles[2]

	if downtime.Value != "2 min" {
		t.Errorf("downtime tile = %q, want the measured 2 min", downtime.Value)
	}
	if strings.Contains(downtime.Label, "estimated") {
		t.Errorf("downtime label %q still calls itself an estimate", downtime.Label)
	}

	outages := p.Incidents(nil, targets[0].Outages24h)
	for _, s := range outages.Stats {
		if s.Label == "MEASURED DOWNTIME" && s.Value != downtime.Value {
			t.Errorf("summary says %q, Outages says %q on the same data", downtime.Value, s.Value)
		}
	}

	if uptime.Label != "uptime, last 8 min" {
		t.Errorf("uptime label = %q, want the real observed period, not 24h", uptime.Label)
	}
}

// TestDowntimeTileNoFakeZero: ноль показывается только как измеренный факт.
func TestDowntimeTileNoFakeZero(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	p := Presenter{Now: func() time.Time { return now }}
	full := new(now.Add(-24 * time.Hour).Add(time.Minute))

	cases := []struct {
		name      string
		target    entity.TargetSummary
		value     string
		label     string
		uptimeLbl string
	}{
		{
			name:      "no checks yet",
			target:    entity.TargetSummary{Kind: entity.KindProbe, Probe: &entity.ProbeStats{}},
			value:     "—",
			label:     "measured downtime, 24h",
			uptimeLbl: "uptime, 24h",
		},
		{
			name: "checks without failures",
			target: entity.TargetSummary{Kind: entity.KindProbe, Probe: &entity.ProbeStats{
				Checks24h: 10, Successes24h: 10, FirstCheckedAt: full,
			}},
			value:     "0 min",
			label:     "measured downtime, 24h",
			uptimeLbl: "uptime, 24h",
		},
		{
			name: "only an outage without a recorded return",
			target: entity.TargetSummary{Kind: entity.KindContainer, Container: &entity.ContainerStats{},
				Outages24h: entity.IncidentStats{Count: 1, Unclosed: 1}},
			value:     "—",
			label:     "measured downtime, 24h · 1 unmeasured",
			uptimeLbl: "uptime, 24h",
		},
	}

	for _, c := range cases {
		tiles := summaryTiles(p, []entity.TargetSummary{c.target})
		if tiles[2].Value != c.value || tiles[2].Label != c.label {
			t.Errorf("%s: downtime tile = %q / %q, want %q / %q", c.name, tiles[2].Value, tiles[2].Label, c.value, c.label)
		}
		if tiles[1].Label != c.uptimeLbl {
			t.Errorf("%s: uptime label = %q, want %q", c.name, tiles[1].Label, c.uptimeLbl)
		}
	}
}
