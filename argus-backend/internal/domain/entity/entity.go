package entity

import (
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
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityCritical, SeverityError, SeverityWarning, SeverityInfo, SeverityDebug:
		return true
	default:
		return false
	}
}

func (e EventType) Valid() bool {
	switch e {
	case ContainerDie, ContainerOOM, ContainerKill, ContainerRestart, ContainerDestroy:
		return true
	default:
		return false
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
