package watcher

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

const stopGrace = 30 * time.Second

const (
	watcherTargetType = "container"
	probeTargetType   = "probe"

	labelMonitor  = "argus.monitor"
	labelProbeURL = "argus.probe.url"

	statusUp   = "up"
	statusDown = "down"
)

type UseCase interface {
	GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error)
	CreateEvent(ctx context.Context, event *entity.ContainerEvent) (*entity.ContainerEvent, error)
	CreateTarget(ctx context.Context, target *entity.Target) (*entity.Target, error)
	UpdateTargetStatus(ctx context.Context, id uuid.UUID, status string) (*entity.Target, error)

	CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error)
}

type DockerClient interface {
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	Events(ctx context.Context, options client.EventsListOptions) client.EventsResult
}

type Watcher struct {
	cfg    *config.Config
	log    *slog.Logger
	uc     UseCase
	client DockerClient
	starts chan<- string
	dies   map[uuid.UUID][]time.Time
	kills  map[uuid.UUID]time.Time
	cursor time.Time
}

func NewWatcher(cfg *config.Config, log *slog.Logger, uc UseCase, client DockerClient, starts chan<- string) *Watcher {
	return &Watcher{
		cfg:    cfg,
		log:    log,
		uc:     uc,
		client: client,
		starts: starts,
		dies:   make(map[uuid.UUID][]time.Time),
		kills:  make(map[uuid.UUID]time.Time),
		cursor: time.Now(),
	}
}

func eventTime(message events.Message) time.Time {
	if message.TimeNano == 0 {
		return time.Now()
	}

	return time.Unix(0, message.TimeNano)
}

func buildDetails(message events.Message) string {
	attrs := message.Actor.Attributes

	parts := []string{
		"name=" + attrs["name"],
		"image=" + attrs["image"],
	}

	if message.Action == events.ActionDie {
		parts = append(parts, "exitCode="+attrs["exitCode"])
	}

	if message.Action == events.ActionKill {
		parts = append(parts, "signal="+attrs["signal"])
	}

	return strings.Join(parts, " ")
}

func (w *Watcher) createEvent(ctx context.Context, message events.Message) error {
	const op = "Watcher.createEvent"

	log := w.log.With("op", op)

	targets, err := w.uc.GetTargetsByType(ctx, watcherTargetType)
	if err != nil {
		return err
	}

	var target entity.Target

	for _, t := range targets {
		if t.Source == message.Actor.Attributes["name"] {
			target = *t

			break
		}
	}

	if target.ID == uuid.Nil {
		return nil
	}

	eventType := entity.EventType(message.Action)
	if !eventType.Valid() {
		log.Error("invalid event type", "action", message.Action)

		return nil
	}

	if status, ok := containerStatus(eventType); ok {
		w.setStatus(ctx, &target, status)
	}

	at := eventTime(message)

	event := &entity.ContainerEvent{
		TargetID:  target.ID,
		Event:     eventType,
		Details:   buildDetails(message),
		CreatedAt: at,
	}

	_, err = w.uc.CreateEvent(ctx, event)
	if err != nil {
		return err
	}

	if w.plannedStop(target.ID, eventType, at) {
		return nil
	}

	w.createAlert(ctx, event, message.Actor.Attributes)

	if eventType == entity.ContainerDie && w.recordDie(target.ID, at) {
		w.raiseCrashLoop(ctx, target.ID)
	}

	return nil
}

func (w *Watcher) plannedStop(id uuid.UUID, event entity.EventType, now time.Time) bool {
	if event == entity.ContainerKill {
		w.kills[id] = now

		return false
	}

	if event != entity.ContainerDie {
		return false
	}

	killedAt, ok := w.kills[id]
	delete(w.kills, id)

	return ok && now.Sub(killedAt) <= stopGrace
}

func (w *Watcher) recordDie(id uuid.UUID, now time.Time) bool {
	cutoff := now.Add(-w.cfg.Watcher.CrashLoopWindow)
	recent := slices.DeleteFunc(w.dies[id], func(t time.Time) bool { return !t.After(cutoff) })
	w.dies[id] = append(recent, now)

	return len(w.dies[id]) >= w.cfg.Watcher.CrashLoopRestarts
}

func (w *Watcher) raiseCrashLoop(ctx context.Context, targetID uuid.UUID) {
	_, err := w.uc.CreateAlert(ctx, &entity.Alert{
		TargetID: &targetID,
		Severity: entity.SeverityCritical,
		Details: fmt.Sprintf("Container is crash-looping: %d restarts within %s",
			w.cfg.Watcher.CrashLoopRestarts, w.cfg.Watcher.CrashLoopWindow),
	})
	if err != nil {
		w.log.Error("failed to create alert", "op", "Watcher.raiseCrashLoop", "error", err)
	}
}

func containerStatus(event entity.EventType) (string, bool) {
	switch event {
	case entity.ContainerStart, entity.ContainerRestart:
		return statusUp, true
	case entity.ContainerDie, entity.ContainerDestroy:
		return statusDown, true
	case entity.ContainerKill, entity.ContainerOOM:
		return "", false
	default:
		return "", false
	}
}

func (w *Watcher) setStatus(ctx context.Context, target *entity.Target, status string) {
	if target.Status == status {
		return
	}

	_, err := w.uc.UpdateTargetStatus(ctx, target.ID, status)
	if err != nil {
		w.log.Error("failed to update target status", "op", "Watcher.setStatus", "target", target.Source, "error", err)
	}
}

func (w *Watcher) syncStatuses(ctx context.Context, running map[string]bool) error {
	targets, err := w.uc.GetTargetsByType(ctx, watcherTargetType)
	if err != nil {
		return err
	}

	for _, t := range targets {
		status := statusDown
		if running[t.Source] {
			status = statusUp
		}

		w.setStatus(ctx, t, status)
	}

	return nil
}

func alertDetails(event entity.EventType, attrs map[string]string) string {
	image := attrs["image"]

	if event == entity.ContainerOOM {
		return fmt.Sprintf("Container was killed after running out of memory (image: %s)", image)
	}

	if event == entity.ContainerDie {
		return fmt.Sprintf("Container exited unexpectedly with code %s (image: %s)", attrs["exitCode"], image)
	}

	return fmt.Sprintf("Container reported a %s event (image: %s)", event, image)
}

func (w *Watcher) createAlert(ctx context.Context, event *entity.ContainerEvent, attrs map[string]string) {
	const op = "Watcher.createAlert"

	severity, ok := event.Event.AlertSeverity(attrs["exitCode"])
	if !ok {
		return
	}

	targetID := event.TargetID
	alert := &entity.Alert{
		TargetID: &targetID,
		Severity: severity,
		Details:  alertDetails(event.Event, attrs),
	}

	_, err := w.uc.CreateAlert(ctx, alert)
	if err != nil {
		w.log.With("op", op).Error("failed to create alert", "error", err)
	}
}

func (w *Watcher) registerFromLabels(ctx context.Context, name string, labels map[string]string) error {
	if labels[labelMonitor] != "true" {
		return nil
	}

	_, err := w.uc.CreateTarget(ctx, &entity.Target{
		Source:     name,
		Name:       name,
		TargetType: watcherTargetType,
		Status:     "unknown",
	})
	if err != nil && !errors.Is(err, entity.ErrTargetAlreadyExists) {
		return err
	}

	probeURL := labels[labelProbeURL]
	if probeURL == "" {
		return nil
	}

	_, err = w.uc.CreateTarget(ctx, &entity.Target{
		Source:     probeURL,
		Name:       name,
		TargetType: probeTargetType,
		Status:     "unknown",
	})
	if err != nil && !errors.Is(err, entity.ErrTargetAlreadyExists) {
		return err
	}

	return nil
}

func (w *Watcher) RegisterContainers(ctx context.Context, notify bool) error {
	result, err := w.client.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return err
	}

	running := make(map[string]bool, len(result.Items))

	for _, c := range result.Items {
		if len(c.Names) == 0 {
			continue
		}

		name := strings.TrimPrefix(c.Names[0], "/")
		running[name] = true

		regErr := w.registerFromLabels(ctx, name, c.Labels)
		if regErr != nil {
			w.log.Error("failed to register container", "name", name, "error", regErr)

			continue
		}

		if notify && c.Labels[labelMonitor] == "true" {
			w.notifyStart(name)
		}
	}

	return w.syncStatuses(ctx, running)
}

const (
	minBackoff        = 1 * time.Second
	maxBackoff        = 60 * time.Second
	minStableDuration = 10 * time.Second
)

func (w *Watcher) RunWatcher(ctx context.Context) {
	const op = "Watcher.RunWatcher"

	log := w.log.With("op", op)

	backoff := minBackoff
	resync := false

	for {
		timer := time.NewTimer(backoff)

		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-timer.C:
			start := time.Now()

			err := w.startListening(ctx, resync)
			if err == nil {
				return
			}

			resync = true

			log.Error("failed to start stream", "error", err)

			if time.Since(start) >= minStableDuration {
				backoff = minBackoff
			} else {
				backoff = min(2*backoff, maxBackoff)
			}
		}
	}
}

func (w *Watcher) handleStart(ctx context.Context, attrs map[string]string) {
	name := attrs["name"]

	err := w.registerFromLabels(ctx, name, attrs)
	if err != nil {
		w.log.Error("failed to process event", "error", err)

		return
	}

	if attrs[labelMonitor] != "true" {
		return
	}

	w.notifyStart(name)
}

func (w *Watcher) notifyStart(name string) {
	select {
	case w.starts <- name:
	default:
		w.log.Warn("matcher is not keeping up, start event dropped", "container", name)
	}
}

func (w *Watcher) startListening(ctx context.Context, resync bool) error {
	stream := w.client.Events(ctx, client.EventsListOptions{
		Filters: map[string]map[string]bool{
			"type":  {"container": true},
			"event": {"die": true, "oom": true, "kill": true, "restart": true, "destroy": true, "start": true},
		},
		Since: w.cursor.Add(time.Nanosecond).UTC().Format(time.RFC3339Nano),
	})

	if resync {
		err := w.RegisterContainers(ctx, true)
		if err != nil {
			w.log.Error("failed to resync containers after reconnect", "error", err)
		}
	}

	for {
		select {
		case event := <-stream.Messages:
			if event.Action == events.ActionStart {
				w.handleStart(ctx, event.Actor.Attributes)
			}

			err := w.createEvent(ctx, event)
			if err != nil {
				w.log.Error("failed to process event", "error", err)
			}

			if at := eventTime(event); at.After(w.cursor) {
				w.cursor = at
			}
		case err := <-stream.Err:
			w.log.Error("event stream closed", "error", err)

			return err
		case <-ctx.Done():
			return nil
		}
	}
}
