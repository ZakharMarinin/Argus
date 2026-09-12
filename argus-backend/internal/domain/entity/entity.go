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
type EventType int

const (
	SeverityCritical Severity = iota
	SeverityError
	SeverityWarning
	SeverityInfo
	SeverityDebug
)

const (
	ContainerStart EventType = iota
	ContainerStop
	ContainerRestart
	ContainerDie
	ContainerKill
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
	case ContainerStart, ContainerStop, ContainerRestart, ContainerDie, ContainerKill:
		return true
	default:
		return false
	}
}

type Target struct {
	ID         uuid.UUID
	Source     string
	Name       string
	Status     string
	TargetType string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ContainerEvent struct {
	ID        int
	TargetID  uuid.UUID
	Event     EventType
	Details   string
	CreatedAt time.Time
}

type LogMatch struct {
	ID        int
	TargetID  uuid.UUID
	Severity  Severity
	Message   string
	CreatedAt time.Time
}

type Probe struct {
	ID        int
	TargetID  uuid.UUID
	Success   bool
	Latency   int32
	CreatedAt time.Time
}

type Alert struct {
	ID        int
	TargetID  uuid.UUID
	Severity  Severity
	Details   string
	Notified  bool
	CreatedAt time.Time
}
