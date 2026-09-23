package watcher

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

const (
	watcherTargetType = "container"
	probeTargetType   = "probe"

	labelMonitor  = "argus.monitor"
	labelProbeURL = "argus.probe.url"
)

type UseCase interface {
	GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error)
	CreateEvent(ctx context.Context, event *entity.ContainerEvent) (*entity.ContainerEvent, error)
	CreateTarget(ctx context.Context, target *entity.Target) (*entity.Target, error)

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
}

func NewWatcher(cfg *config.Config, log *slog.Logger, uc UseCase, client DockerClient) (*Watcher, error) {
	return &Watcher{cfg: cfg, log: log, uc: uc, client: client}, nil
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

	event := &entity.ContainerEvent{
		TargetID: target.ID,
		Event:    eventType,
		Details:  buildDetails(message),
	}

	_, err = w.uc.CreateEvent(ctx, event)
	if err != nil {
		return err
	}

	w.createAlert(ctx, event, message.Actor.Attributes)

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

	alert := &entity.Alert{
		TargetID: event.TargetID,
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

func (w *Watcher) RegisterContainers(ctx context.Context) error {
	result, err := w.client.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return err
	}

	for _, c := range result.Items {
		if len(c.Names) == 0 {
			continue
		}

		name := strings.TrimPrefix(c.Names[0], "/")

		regErr := w.registerFromLabels(ctx, name, c.Labels)
		if regErr != nil {
			w.log.Error("failed to register container", "name", name, "error", regErr)
		}
	}

	return nil
}

const (
	minBackoff        = 1 * time.Second
	maxBackoff        = 60 * time.Second
	minStableDuration = 10 * time.Second
)

func (w *Watcher) RunWatcher(ctx context.Context) {
	const op = "Watcher.RunWatcher"

	log := w.log.With("op", op)

	err := w.RegisterContainers(ctx)
	if err != nil {
		log.Error("failed to register existing containers", "error", err)
	}

	backoff := minBackoff

	for {
		timer := time.NewTimer(backoff)

		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-timer.C:
			start := time.Now()

			err := w.startListening(ctx)
			if err == nil {
				return
			}

			log.Error("failed to start stream", "error", err)

			if time.Since(start) >= minStableDuration {
				backoff = minBackoff
			} else {
				backoff = min(2*backoff, maxBackoff)
			}
		}
	}
}

func (w *Watcher) startListening(ctx context.Context) error {
	stream := w.client.Events(ctx, client.EventsListOptions{
		Filters: map[string]map[string]bool{
			"type":  {"container": true},
			"event": {"die": true, "oom": true, "kill": true, "restart": true, "destroy": true, "start": true},
		},
	})

	for {
		select {
		case event := <-stream.Messages:
			if event.Action == events.ActionStart {
				regErr := w.registerFromLabels(ctx, event.Actor.Attributes["name"], event.Actor.Attributes)
				if regErr != nil {
					w.log.Error("failed to process event", "error", regErr)
				}
			}

			err := w.createEvent(ctx, event)
			if err != nil {
				w.log.Error("failed to process event", "error", err)
			}
		case err := <-stream.Err:
			w.log.Error("event stream closed", "error", err)

			return err
		case <-ctx.Done():
			return nil
		}
	}
}
