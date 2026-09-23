package entity

import (
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	EnvLocal = "local"
	EnvDev   = "dev"
	EnvProd  = "prod"
)

type Severity int
type EventType string

const (
	SeverityCritical Severity = iota
	SeverityError
	SeverityWarning
	SeverityInfo
	SeverityDebug
)

const (
	ContainerDie     EventType = "die"
	ContainerOOM     EventType = "oom"
	ContainerKill    EventType = "kill"
	ContainerRestart EventType = "restart"
	ContainerDestroy EventType = "destroy"
	ContainerStart   EventType = "start"
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityCritical, SeverityError, SeverityWarning, SeverityInfo, SeverityDebug:
		return true
	default:
		return false
	}
}

func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "CRITICAL"
	case SeverityError:
		return "ERROR"
	case SeverityWarning:
		return "WARNING"
	case SeverityInfo:
		return "INFO"
	case SeverityDebug:
		return "DEBUG"
	default:
		return "UNKNOWN"
	}
}

func (e EventType) Valid() bool {
	switch e {
	case ContainerDie, ContainerOOM, ContainerKill, ContainerRestart, ContainerDestroy, ContainerStart:
		return true
	default:
		return false
	}
}

func (e EventType) AlertSeverity(exitCode string) (Severity, bool) {
	switch e {
	case ContainerOOM:
		return SeverityCritical, true
	case ContainerDie:
		if exitCode == "0" {
			return SeverityInfo, false
		}

		return SeverityError, true
	case ContainerKill, ContainerRestart, ContainerDestroy, ContainerStart:
		return SeverityInfo, false
	default:
		return SeverityInfo, false
	}
}

type Target struct {
	ID         uuid.UUID `json:"id"`
	Source     string    `json:"source"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	TargetType string    `json:"target_type"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const (
	TargetTypeProbe     = "probe"
	TargetTypeContainer = "container"
)

func (t *Target) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return &ValidationError{Reason: "name is required"}
	}

	switch t.TargetType {
	case TargetTypeContainer:
		if strings.TrimSpace(t.Source) == "" {
			return &ValidationError{Reason: "container name is required"}
		}
	case TargetTypeProbe:
		u, err := url.Parse(t.Source)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return &ValidationError{Reason: "URL must be an absolute http:// or https:// address"}
		}
	default:
		return &ValidationError{Reason: `target_type must be "probe" or "container"`}
	}

	return nil
}

type ContainerEvent struct {
	ID        int       `json:"id"`
	TargetID  uuid.UUID `json:"target_id"`
	Event     EventType `json:"event"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

type LogMatch struct {
	ID        int       `json:"id"`
	TargetID  uuid.UUID `json:"target_id"`
	Severity  Severity  `json:"severity"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Probe struct {
	ID        int       `json:"id"`
	TargetID  uuid.UUID `json:"target_id"`
	Success   bool      `json:"success"`
	Latency   int64     `json:"latency"`
	CreatedAt time.Time `json:"created_at"`
}

type Alert struct {
	ID        int       `json:"id"`
	TargetID  uuid.UUID `json:"target_id"`
	Severity  Severity  `json:"severity"`
	Details   string    `json:"details"`
	Notified  bool      `json:"notified"`
	CreatedAt time.Time `json:"created_at"`
}

type ListFilter struct {
	TargetIDs []uuid.UUID
	Severity  *Severity
	Notified  *bool
	Since     *time.Time
	After     int
	Skip      uint64
	Limit     uint64
	Desc      bool
}
