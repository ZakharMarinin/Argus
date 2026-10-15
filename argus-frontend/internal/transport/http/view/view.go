// Package view holds the Argus dashboard's view models and their rendering.
//
// The package knows nothing about the backend's HTTP API or about routes: it
// takes entity domain structures, lays them out into its own types with the
// Presenter and renders the templates from web/. It is the only place where
// data becomes text.
package view

import (
	"fmt"
	"html/template"
)

// Status is a target's state in dashboard terms. The value goes into a CSS class
// that sets the --s / --s-tint pair; the marker, the pill and the bar then
// read them. No template hardcodes a colour.
type Status string

const (
	StatusUp   Status = "up"   // everything responds
	StatusWarn Status = "warn" // degraded: restarts, flaky probes
	StatusDown Status = "down" // not responding
	StatusIdle Status = "idle" // state unknown (spec §9: not the same as down)
)

// Class returns the CSS class of the status.
func (s Status) Class() string {
	switch s {
	case StatusUp, StatusWarn, StatusDown, StatusIdle:
		return "is-" + string(s)
	default:
		return "is-idle"
	}
}

// Mood is the mascot's mood. A single source of truth for the picture and the
// caption: both the overview card and the preview in the switcher take it from here.
type Mood struct {
	Key   string
	Title string
	Note  string
	Src   string
	When  string
}

// Moods is the full set of moods; the key matches the GIF name.
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
		Note:  "There are warnings: a flaky check, or a container just crashed and came back.",
		When:  "A flaky check, a container crash in the last 15 minutes, a warning alert, or 3–4 log matches.",
	},
	"angry": {
		Key: "angry", Src: "/static/img/argus-angry.gif",
		Title: "Critical failure",
		Note:  "There are critical alerts or targets that are down. Worth checking the history.",
		When:  "A target is down, a critical alert fired, or 5+ log matches in 24h.",
	},
	// A sleeping Argus means "nothing to watch yet": the backend returned no
	// targets at all. Pausing auto-refresh is shown by the chip in the header, not the mascot.
	"sleeping": {
		Key: "sleeping", Src: "/static/img/argus-sleeping.gif",
		Title: "Nothing to watch yet",
		Note:  "There isn't a single target yet. Argus will wake up once collector records the first check or sees a container.",
		When:  "Nothing is being watched yet.",
	},
}

// MoodOrder sets the order of buttons in the switcher.
var MoodOrder = []string{"happy", "watching", "scared", "angry", "sleeping"}

// ---------------------------------------------------------------------------
// Page scaffolding
// ---------------------------------------------------------------------------

// Layout is everything layout.html needs: the sidebar and the model of the
// specific page in Content.
type Layout struct {
	Title  string
	Sub    string
	Active string // key of the current menu item

	// PartialPath is shown in the `hx-get ...` chip in the header. It isn't
	// decoration: it shows which partial is being polled right now.
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

// NavItem is a menu item in the sidebar.
type NavItem struct {
	Key   string
	Label string
	Href  string
	Badge string
}

// Sidebar is the persistent part of the sidebar: the mascot, the version, host
// resources and the collapsible list of containers.
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

type HostPanel struct {
	Note   string
	Meters []Meter
}

// Meter is a single resource usage bar.
type Meter struct {
	Label   string
	Value   string
	Percent int
	Status  Status
}

// ---------------------------------------------------------------------------
// Overview
// ---------------------------------------------------------------------------

// Overview is the model of the main page.
type Overview struct {
	Health     Health
	Targets    TargetList
	Containers ContainerList
	Chart      Chart
	Logs       LogPulse
	Recent     RecentPanel

	// Blocks is the default arrangement of the overview board. The person can
	// reorder, resize and hide blocks; that choice lives in their browser
	// (localStorage, like the theme), and the server only renders the default.
	Blocks []Block
}

// Block widths in columns of the 12-column overview board. Four steps are
// enough to build any row and few enough to pick from at a glance.
const (
	BlockThird     = 4
	BlockHalf      = 6
	BlockTwoThirds = 8
	BlockFull      = 12
)

// BlockSizes is the size switcher of a block, narrowest first.
func BlockSizes() []BlockSize {
	return []BlockSize{
		{Cols: BlockThird, Label: "1/3", Name: "One third"},
		{Cols: BlockHalf, Label: "1/2", Name: "Half"},
		{Cols: BlockTwoThirds, Label: "2/3", Name: "Two thirds"},
		{Cols: BlockFull, Label: "Full", Name: "Full width"},
	}
}

// BlockSize is one option of the size switcher.
type BlockSize struct {
	Cols  int
	Label string
	Name  string // accessible name: "2/3" alone is read out as "two slash three"
}

// Block is one movable block of the overview board.
type Block struct {
	ID    string // stable key: the saved layout refers to blocks by it
	Title string
	Size  int // default width in board columns
	// Pinned blocks can be moved and resized but not hidden: without the
	// overall state the overview no longer answers "is something broken?".
	Pinned bool
	Sizes  []BlockSize
}

// OverviewBlocks is the default board, triage first: the state and what just
// happened, then what exactly is broken, and the 24-hour trends last.
func OverviewBlocks() []Block {
	sizes := BlockSizes()

	return []Block{
		{ID: "summary", Title: "Overall status", Size: BlockThird, Pinned: true, Sizes: sizes},
		{ID: "recent", Title: "Recent activity", Size: BlockTwoThirds, Sizes: sizes},
		{ID: "targets", Title: "Uptime targets", Size: BlockFull, Sizes: sizes},
		{ID: "containers", Title: "Containers", Size: BlockFull, Sizes: sizes},
		{ID: "latency", Title: "Latency and uptime", Size: BlockTwoThirds, Sizes: sizes},
		{ID: "logs", Title: "Log matches", Size: BlockThird, Sizes: sizes},
	}
}

// Health is the overall state line.
//
// The schema has no alert lifecycle: there is nowhere to get "active/closed" from.
// So alerts over the last 24 hours are counted, and the caption says exactly that.
type Health struct {
	State        string // OK | DEGRADED | WATCH | NO DATA
	TargetsUp    int
	TargetsTotal int
	AlertsDay    int
	Bar          []HealthSeg
	Tiles        []Tile
	// Empty means the backend returned no targets at all.
	Empty      bool
	Mood       Mood
	MoodStatus Status
	Reason     string
}

// Tone turns State into a data-health value for the page's background tint.
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

// HealthSeg is a segment of the health bar.
type HealthSeg struct {
	Status  Status
	Percent int
}

// Tile is one of the dense figures in the state line.
type Tile struct {
	Value  string
	Label  string
	Status Status
}

// Chart holds the parameters of a chart card. The data is fetched with a
// separate JSON request, so that htmx polling of partials doesn't recreate the
// canvas every 10 seconds.
type Chart struct {
	DataURL string
	Title   string
	Legend  []LegendItem
	// Hint explains what exactly is drawn, if it isn't obvious.
	Hint      string
	EmptyNote string
}

// LegendItem is a series label with a colour mark.
type LegendItem struct {
	Label string
	// Color is the name of a palette token: accent, up, warn, down, idle.
	Color string
}

// TargetList is the uptime targets block.
type TargetList struct {
	Note    string // "checks every 30 s · timeout 5 s"
	Targets []Target
}

// Target is a single uptime target.
type Target struct {
	ID      string
	Name    string
	URL     string
	Status  Status
	Label   string // up | degraded | down | no data
	Latency string
	Uptime  string
	// Strip holds the outcomes of the latest checks, left to right (old → new).
	Strip []Status
}

// ContainerList is the containers block.
type ContainerList struct {
	Note       string // "5 of 6 running"
	Containers []Container
}

// Container is a single container card.
type Container struct {
	Name   string
	Note   string
	Href   string
	Status Status
	Label  string // up | warn | down | no data
	// Dark raises the card onto a dark panel: containers that definitely
	// need attention (not responding), not just a warning.
	Dark    bool
	Metrics []Metric
}

// Metric is a value like restarts/last-event on a container card.
type Metric struct {
	Label string
	Value string
	// Status highlights the value itself. Empty means a neutral number.
	Status Status
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

// AlertPage holds the filters and the list together: the partial returns them in
// one piece, so that after a filter click the chip's active state is redrawn too.
type AlertPage struct {
	Types      []Chip
	Severities []Chip
	Alerts     []Alert
	// Query holds the current filters as a query string. The partial reuses
	// it in its own hx-get, so polling keeps the selected filter.
	Query template.URL
	Pager Pager
	Empty string
}

// Chip is a filter button.
type Chip struct {
	Label  string
	Href   template.URL
	Active bool
}

// Alert is a row of the alert history.
type Alert struct {
	Time     string
	Message  string
	Target   string
	Type     string
	Severity string
	State    string // delivery status as text
	// StateClass is a modifier class for State; empty for the plain look.
	StateClass string
	Status     Status
	MoodSrc    string
	// Alarm raises the row onto a dark panel. Only fresh critical/error alerts get it:
	// if the whole history got it, the device would stop meaning "look here".
	Alarm bool
}

// ---------------------------------------------------------------------------
// Container details
// ---------------------------------------------------------------------------

// Detail is the page of a single container.
type Detail struct {
	Name     string
	Hero     Hero
	Meta     MetaPanel
	Chart    Chart
	Timeline Timeline
	Events   EventPanel
	Logs     LogFeed
}

// Hero is the dark banner with the mascot and the current reason for concern.
type Hero struct {
	Badge   string // «WARNING · RESTARTED»
	Title   string
	Rule    string // the triggered event, in monospace
	MoodSrc string
	Status  Status
}

// MetaPanel holds the container's state tiles.
type MetaPanel struct {
	URL   string
	Tiles []MetaTile
}

// MetaTile is a "STATUS / RESTARTS / ..." tile.
type MetaTile struct {
	Label  string
	Value  string
	Status Status
}

// EventPanel is the history of Docker events for a container.
//
// The events table stores not only restarts but also die/oom/kill/destroy,
// so the panel is named after events rather than just restarts.
type EventPanel struct {
	URL    string
	Events []EventRow
}

// EventRow is a single Docker event.
type EventRow struct {
	Time   string
	Reason string
	Kind   string
	Status Status
}

// ---------------------------------------------------------------------------
// Incidents
// ---------------------------------------------------------------------------

// IncidentPanel is the "how many outages and how long" block.
type IncidentPanel struct {
	Note  string
	Stats []MetaTile
	Rows  []IncidentRow
}

// IncidentRow is a single outage interval.
//
// Duration is a ready string, and for an unclosed interval it is a dash: the end
// isn't in the data, and a drawn number would look like a measurement.
type IncidentRow struct {
	Name     string
	Href     string // link to the container page; empty for probes
	Kind     string
	Status   Status
	Label    string // recovered | still down | return not recorded
	Cause    string
	Started  string
	Duration string
}

// Timeline is the 24-hour outage bar on the container page.
//
// The bar is static between htmx swaps and needs neither Chart.js nor JS: it is
// a set of blocks with precomputed left/width.
type Timeline struct {
	URL    string
	Title  string
	Hint   string
	Bands  []TimelineBand
	Legend []LegendItem
	// Rows holds the same intervals as a list, newest first.
	//
	// The bar shows the picture of the day, but a short outage takes up a
	// fraction of a percent on it: three failures in a row merge into one mark.
	// The list is readable at any scale, so the numbers live in it rather than
	// in hover tooltips.
	Rows []IncidentRow
	// Note replaces the bar when there is nothing to draw.
	Note string
}

// TimelineBand is a single segment of the bar.
type TimelineBand struct {
	Status Status
	// Style carries left and width already computed: html/template would strip
	// them from the style attribute as unsafe.
	Style template.CSS
	// Title is the hover tooltip: cause and duration.
	Title string
}

// ---------------------------------------------------------------------------
// "No data yet" state
// ---------------------------------------------------------------------------

// Empty describes an empty screen.
type Empty struct {
	Mood  Mood
	Title string
	Note  string
}

// WaitingForData is the "no data yet" screen.
func WaitingForData(reason string) Empty {
	return Empty{
		Mood:  Moods["sleeping"],
		Title: "No data yet",
		Note:  reason,
	}
}

// ---------------------------------------------------------------------------
// Template helpers
// ---------------------------------------------------------------------------

// styleSpan returns the position and width of a bar segment as typed CSS.
//
// The percentages are fractional: a one-minute interval on a 24-hour bar would
// vanish entirely in whole percent, and an outage nobody can see is an outage
// nobody finds out about.
func styleSpan(left, width float64) template.CSS {
	left = clampPercent(left)
	width = clampPercent(width)

	if left+width > 100 {
		width = 100 - left
	}

	// G203: both values are numbers already clamped to 0..100.
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

// styleWidth returns the width as typed CSS, otherwise html/template strips
// the computed value from the style attribute.
func styleWidth(percent int) template.CSS {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// G203: the value is an integer already clamped to 0..100.
	return template.CSS(fmt.Sprintf("width:%d%%", percent)) //nolint:gosec
}

func styleHeight(percent float64) template.CSS {
	return template.CSS(fmt.Sprintf("height:%.1f%%", clampPercent(percent))) //nolint:gosec // G203: a number clamped to 0..100
}
