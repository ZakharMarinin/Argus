package matcher

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type UseCase interface {
	GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error)

	SaveLogMatch(ctx context.Context, logMatch *entity.LogMatch) (*entity.LogMatch, error)
	CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error)
}

type DockerClient interface {
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
}

type Matcher struct {
	log    *slog.Logger
	client DockerClient
	uc     UseCase
	rules  []rule
	starts <-chan string

	mu      sync.Mutex
	running map[string]struct{}
}

type rule struct {
	re        *regexp.Regexp
	severity  entity.Severity
	container string
}

func NewMatcher(cfg *config.Config, log *slog.Logger, uc UseCase, client DockerClient, starts <-chan string) (*Matcher, error) {
	rules, err := compileRules(cfg.Matcher.Rules)
	if err != nil {
		return nil, err
	}

	return &Matcher{
		log:     log,
		uc:      uc,
		client:  client,
		rules:   rules,
		starts:  starts,
		running: make(map[string]struct{}),
	}, nil
}

func compileRules(cfg []config.Rule) ([]rule, error) {
	rules := make([]rule, 0, len(cfg))
	for _, r := range cfg {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", r.Pattern, err)
		}
		sev, err := severityFromString(r.Severity)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", r.Pattern, err)
		}
		rules = append(rules, rule{re: re, severity: sev, container: r.Container})
	}

	return rules, nil
}

func (m *Matcher) follow(ctx context.Context, source string, id uuid.UUID, since time.Time) (time.Time, error) {
	info, err := m.client.ContainerInspect(ctx, source, client.ContainerInspectOptions{})
	if err != nil {
		return since, err
	}

	if since.IsZero() {
		since = startedAt(info.Container)
	}

	rc, err := m.client.ContainerLogs(ctx, source, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
		Since:      since.Format(time.RFC3339Nano),
	})
	if err != nil {
		return since, err
	}
	defer func() { _ = rc.Close() }()

	pr, pw := io.Pipe()

	go func() {
		var err error

		if info.Container.Config.Tty {
			_, err = io.Copy(pw, rc)
		} else {
			_, err = stdcopy.StdCopy(pw, pw, rc)
		}
		pw.CloseWithError(err)
	}()

	last := since
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		ts, msg, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}

		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil || !t.After(last) {
			continue
		}
		last = t

		for _, rule := range m.rules {
			if rule.container != "*" && rule.container != source {
				continue
			}
			if rule.re.MatchString(msg) {
				m.onMatch(ctx, id, t, msg, rule)
			}
		}
	}

	err = sc.Err()
	if err == nil || errors.Is(err, context.Canceled) {
		return last, nil
	}

	return last, fmt.Errorf("read logs %s: %w", source, err)
}

func (m *Matcher) onMatch(ctx context.Context, id uuid.UUID, t time.Time, msg string, rule rule) {
	logMatch := &entity.LogMatch{
		TargetID:  id,
		Severity:  rule.severity,
		Message:   msg,
		CreatedAt: t,
	}

	const op = "Matcher.onMatch"

	_, err := m.uc.SaveLogMatch(ctx, logMatch)
	if err != nil {
		m.log.Error("failed to save log match", "op", op, "error", err)

		return
	}

	_, err = m.uc.CreateAlert(ctx, &entity.Alert{
		TargetID: &id,
		Severity: rule.severity,
		Details:  fmt.Sprintf("Log line matched rule %q", rule.re.String()),
	})
	if err != nil {
		m.log.Error("failed to create alert", "op", op, "error", err)
	}
}

func severityFromString(name string) (entity.Severity, error) {
	for s := entity.SeverityCritical; s <= entity.SeverityDebug; s++ {
		if strings.EqualFold(name, s.String()) {
			return s, nil
		}
	}

	return 0, fmt.Errorf("invalid severity: %s", name)
}

const (
	minBackoff        = 1 * time.Second
	maxBackoff        = 60 * time.Second
	minStableDuration = 10 * time.Second
)

func (m *Matcher) gone(ctx context.Context, source string) bool {
	info, err := m.client.ContainerInspect(ctx, source, client.ContainerInspectOptions{})
	if err != nil {
		return cerrdefs.IsNotFound(err)
	}

	return info.Container.State == nil || !info.Container.State.Running
}

func startedAt(c container.InspectResponse) time.Time {
	if c.State != nil {
		t, err := time.Parse(time.RFC3339Nano, c.State.StartedAt)
		if err == nil && !t.IsZero() {
			return t
		}
	}

	return time.Now()
}

func (m *Matcher) startMatching(ctx context.Context, source string, id uuid.UUID, since time.Time) {
	const op = "Matcher.startMatching"

	log := m.log.With("op", op, "container", source)

	last := since
	backoff := minBackoff

	for {
		start := time.Now()

		var err error

		last, err = m.follow(ctx, source, id, last)
		if err != nil {
			log.Error("log stream failed", "error", err)
		}

		if m.gone(ctx, source) {
			log.Info("container stopped, log stream closed")

			return
		}

		if time.Since(start) >= minStableDuration {
			backoff = minBackoff
		}

		timer := time.NewTimer(backoff)

		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-timer.C:
			backoff = min(2*backoff, maxBackoff)
		}
	}
}

func (m *Matcher) spawn(ctx context.Context, wg *sync.WaitGroup, source string, id uuid.UUID, since time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.running[source]; ok {
		return
	}

	m.running[source] = struct{}{}

	wg.Go(func() {
		defer func() {
			m.mu.Lock()
			delete(m.running, source)
			m.mu.Unlock()
		}()

		m.startMatching(ctx, source, id, since)
	})
}

func (m *Matcher) targetByName(ctx context.Context, name string) (*entity.Target, error) {
	targets, err := m.uc.GetTargetsByType(ctx, "container")
	if err != nil {
		return nil, err
	}

	for _, t := range targets {
		if t.Source == name {
			return t, nil
		}
	}

	return nil, entity.ErrTargetNotFound
}

func (m *Matcher) RunMatcher(ctx context.Context) {
	const op = "Matcher.RunMatcher"

	log := m.log.With("op", op)

	var wg sync.WaitGroup

	targets, err := m.uc.GetTargetsByType(ctx, "container")
	if err != nil {
		log.Error("failed to get container targets", "error", err)
	}

	for _, t := range targets {
		m.spawn(ctx, &wg, t.Source, t.ID, time.Now())
	}

	for {
		select {
		case name := <-m.starts:
			t, err := m.targetByName(ctx, name)
			if err != nil {
				log.Error("failed to find target for started container", "container", name, "error", err)

				continue
			}

			m.spawn(ctx, &wg, t.Source, t.ID, time.Time{})
		case <-ctx.Done():
			wg.Wait()

			return
		}
	}
}
