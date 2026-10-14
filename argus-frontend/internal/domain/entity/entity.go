// Package entity holds the domain types of the Argus dashboard.
//
// The layer knows nothing about the backend's HTTP API or about HTML: usecase
// computes aggregates from these types, transport lays them out for the view.
// The package has no outward dependencies, only the standard library.
package entity

import "time"

// Kind is the kind of target. The values match the backend's target_type (spec §7).
type Kind string

const (
	KindProbe     Kind = "probe"
	KindContainer Kind = "container"
	KindHost      Kind = "host"
)

// Status is the state of a target as the backend keeps it.
//
// StatusUnknown is not the same as down: it marks targets the collector
// currently knows nothing about, for example when Docker is unavailable (spec §9).
type Status string

const (
	StatusUp      Status = "up"
	StatusDown    Status = "down"
	StatusUnknown Status = "unknown"
)

// Target is a target as the backend returns it, without aggregates.
type Target struct {
	ID        string
	Kind      Kind
	Label     string
	Status    Status
	Source    string
	UpdatedAt time.Time
}

// TargetSummary is a target together with its computed
// aggregates. Probe is filled only for uptime targets, Container only for
// containers; for the other kind the field is always nil.
type TargetSummary struct {
	ID     string
	Kind   Kind
	Label  string
	Status Status
	// Source is the address the prober checks (for Kind == KindProbe).
	// For containers it is the same as Label (the container name).
	Source    string
	UpdatedAt time.Time

	Probe     *ProbeStats
	Container *ContainerStats

	// Outages24h — сводка тех же интервалов простоя за 24 часа, что показывает
	// страница Outages (usecase.Incidents), посчитанная по уже загруженной
	// истории цели. Плитка простоя в сводке берёт её, а не пересчитывает
	// процент uptime в минуты.
	Outages24h IncidentStats
}

// ProbeStats holds aggregates over the check results of an uptime target.
type ProbeStats struct {
	// LastLatencyMS and LastCheckedAt describe the latest check. nil if there
	// were no checks yet: the target is in the config, but the collector hasn't reached it.
	LastLatencyMS *float64
	LastCheckedAt *time.Time

	// Checks24h and Successes24h are counted over the last 24 hours. Zero checks
	// is zero, not 100% uptime.
	Checks24h    int
	Successes24h int

	// FirstCheckedAt — самая старая проверка в окне 24 часов. По ней видно,
	// что истории меньше суток и процент нельзя подписывать как «24h».
	// nil, если проверок за сутки не было.
	FirstCheckedAt *time.Time

	// Recent holds the latest checks from oldest to newest, for the bar on the card.
	Recent []ProbeOutcome
}

// Uptime24h returns the share of successful checks over 24 hours and whether
// there was anything to compute it from.
func (p ProbeStats) Uptime24h() (float64, bool) {
	if p.Checks24h == 0 {
		return 0, false
	}

	return 100 * float64(p.Successes24h) / float64(p.Checks24h), true
}

// ProbeOutcome is the outcome of a single check.
type ProbeOutcome struct {
	Success   bool
	LatencyMS *float64
	CheckedAt time.Time
}

// ContainerStats holds aggregates over a container's events.
type ContainerStats struct {
	Crashes        int
	CrashesClearAt *time.Time

	// The latest event: what exactly the container is doing right now.
	LastEvent       string
	LastEventDetail string
	LastEventAt     *time.Time
}

// Severity is the backend's five entity.Severity levels as strings, from the
// most urgent to the quietest. AlertSeverities in web/present.go sets the
// filter order with the same values.
const (
	SeverityCritical = "critical"
	SeverityError    = "error"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
	SeverityDebug    = "debug"
)

// Alert is a backend alert, enriched with data about its target.
//
// An alert has neither a type nor a lifecycle: the type is derived from the
// kind of target, and instead of an "active/closed" status there is only the
// delivery status (Status). None of this is made up: exactly what was
// recorded is shown.
type Alert struct {
	ID          int64
	TargetID    string
	TargetLabel string
	TargetKind  Kind
	Severity    string // one of the Severity* values
	Message     string
	// Status is the backend value as is: one of the AlertStatus* values, or
	// one the dashboard doesn't know yet if the backend adds it.
	Status    string
	CreatedAt time.Time
}

// Alert delivery statuses, as the backend reports them in alert.status.
const (
	AlertStatusSent     = "sent"     // delivered to Telegram
	AlertStatusCooldown = "cooldown" // suppressed: an alert for the target went out recently
	AlertStatusNotSent  = "not_sent" // not delivered
)

// AlertCounts holds alert counters for the last 24 hours.
// Critical combines SeverityCritical and SeverityError, both "urgent, look
// now" from the dashboard's point of view; Warning is only SeverityWarning.
type AlertCounts struct {
	Day      int
	Critical int
	Warning  int
}

// ContainerDetail is the page of a single container.
type ContainerDetail struct {
	Target     TargetSummary
	Events     []ContainerEvent
	Activity   []ActivityPoint
	LogMatches []LogMatch

	// Incidents are outage intervals over 24 hours, derived from the same events
	// that are in Events. Unclosed ones (without a recovery record) are here too:
	// a gap in the data must be visible, not hidden.
	Incidents []Incident
}

// ContainerEvent is a Docker event for a container.
type ContainerEvent struct {
	Event      string
	Detail     string
	OccurredAt time.Time
}

// ActivityPoint is a container's hourly activity.
//
// A container has neither latency nor uptime percentage: nothing pings it.
type ActivityPoint struct {
	Hour       time.Time
	Events     int
	LogMatches int
}

type LogMatch struct {
	ID          int64
	TargetID    string
	TargetLabel string
	TargetKind  Kind
	Severity    string
	Message     string
	CreatedAt   time.Time
}

type LogMatchPoint struct {
	Hour    time.Time
	Urgent  int
	Warning int
	Quiet   int
}

type LogMatchReport struct {
	Matches []LogMatch
	Hourly  []LogMatchPoint
	Future  int
}

// LatencyPoint is an hourly point of the overview chart.
// Both fields are nil if there were no checks in that hour: a gap in the chart
// is more honest than a drawn zero.
type LatencyPoint struct {
	Hour      time.Time
	P95MS     *float64
	UptimePct *float64
}

type ListQuery struct {
	TargetIDs []string
	Host      bool
	Severity  string
	Since     time.Time
	Skip      int
	Limit     int
}

type Page[T any] struct {
	Items []T
	Total int
	Skip  int
}

type HistoryQuery struct {
	Kind     Kind
	Severity string
	Target   string
	Page     int
}

type Recent struct {
	Alerts     []Alert
	LogMatches []LogMatch
	Incidents  []Incident
}

const (
	HostLevelOK       = "ok"
	HostLevelWarning  = "warning"
	HostLevelCritical = "critical"
)

type HostMetric struct {
	Percent float64
	Used    uint64
	Total   uint64
	Level   string
}

type Host struct {
	Hostname  string
	SampledAt *time.Time
	CPU       *HostMetric
	Memory    *HostMetric
	Disk      *HostMetric
}
