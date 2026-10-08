package entity

import "time"

// IncidentState is what exactly is known about an outage interval.
//
// There are three states, not two, because "came back" and "unknown whether it
// came back" are different facts and must not be confused: with one kind of
// data the outage can be measured, with the other only its start can be recorded.
type IncidentState string

const (
	// IncidentClosed: both the start and the end are known, the outage is measured.
	IncidentClosed IncidentState = "closed"

	// IncidentOpen: the outage is ongoing, and this is confirmed by fresh
	// observations (failed probes continue up to the latest record).
	IncidentOpen IncidentState = "open"

	// IncidentUnclosed: the start is known, there is no recovery record: the
	// container went down, and no start/restart/destroy was recorded after that.
	// The duration of such an interval is unknown and must not be made up.
	IncidentUnclosed IncidentState = "unclosed"
)

// Incident is a single interval during which a target wasn't working.
//
// Incidents aren't stored: they are derived from probes and Docker events. So
// they have neither an id nor a lifecycle: it is a slice of what the collector recorded.
type Incident struct {
	TargetID    string
	TargetLabel string
	Kind        Kind

	// StartedAt is the first observation of the outage: a failed probe or a
	// die/oom/kill event. The real moment of failure lies somewhere between this
	// observation and the previous successful one; the data is no more precise.
	StartedAt time.Time

	// EndedAt is the observation that closed the outage: a successful probe or
	// start/restart/destroy. nil for open and unclosed.
	EndedAt *time.Time

	// LastKnownAt is the latest observation confirming the outage. For open it is
	// the latest failed probe; the duration is computed from it so that the
	// counter doesn't keep growing on its own once the collector stopped checking.
	LastKnownAt time.Time

	// Cause is what started the outage: die, oom, kill, probe failed.
	Cause string
	Crash bool

	State IncidentState
}

// Duration returns the measured outage duration and whether there was anything
// to compute it from. IncidentUnclosed returns false: the end of the interval
// isn't in the data, and any number here would be made up.
func (i Incident) Duration() (time.Duration, bool) {
	if i.State == IncidentUnclosed {
		return 0, false
	}

	end := i.LastKnownAt
	if i.EndedAt != nil {
		end = *i.EndedAt
	}

	if end.Before(i.StartedAt) {
		return 0, false
	}

	return end.Sub(i.StartedAt), true
}

// IncidentStats is a summary of incidents over a period.
type IncidentStats struct {
	// Count is how many outage intervals fell into the period, including unclosed ones.
	Count int

	// Total is the total measurable downtime. Unclosed intervals aren't
	// included: their duration is unknown, and adding zero to the sum would
	// silently understate it.
	Total time.Duration

	// Longest is the longest measurable outage.
	Longest time.Duration

	// Unclosed is how many intervals have no recovery record. It counts the
	// gap in the data and is shown next to the total.
	Unclosed int
}
