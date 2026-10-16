package prober

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	probeWorkers    = 20
	probeTargetType = "probe"

	statusUp   = "up"
	statusDown = "down"
)

type UseCase interface {
	SaveProbes(ctx context.Context, probes []*entity.Probe) ([]*entity.Probe, error)
	GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error)
	UpdateTargetStatus(ctx context.Context, id uuid.UUID, status string) (*entity.Target, error)
	CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error)
}

type Prober struct {
	cfg      *config.Config
	log      *slog.Logger
	uc       UseCase
	failures map[uuid.UUID]int
}

type probeResult struct {
	target *entity.Target
	probe  *entity.Probe
	reason string
}

func NewProber(cfg *config.Config, log *slog.Logger, uc UseCase) *Prober {
	return &Prober{cfg: cfg, log: log, uc: uc, failures: make(map[uuid.UUID]int)}
}

func (p *Prober) checkProbe(ctx context.Context, target *entity.Target) (*entity.Probe, string, error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.Source, nil)
	if err != nil {
		return nil, "", err
	}

	client := http.Client{Timeout: p.cfg.Prober.ProbeTimeout}

	probe := &entity.Probe{TargetID: target.ID}

	resp, err := client.Do(req)
	if err != nil {
		probe.Latency = time.Since(start).Microseconds()

		return probe, probeFailureReason(err) + selfAddressHint(target.Source), nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	probe.Success = resp.StatusCode < 300
	probe.Latency = time.Since(start).Microseconds()

	if !probe.Success {
		return probe, fmt.Sprintf("Health check returned HTTP %d", resp.StatusCode), nil
	}

	return probe, "", nil
}

func probeFailureReason(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "Health check timed out"
	}

	dnsErr, ok := errors.AsType[*net.DNSError](err)
	if ok {
		return fmt.Sprintf("Host %s could not be resolved", dnsErr.Name)
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return "Service is unreachable: connection refused"
	}

	_, ok = errors.AsType[*tls.CertificateVerificationError](err)
	if ok {
		return "TLS certificate verification failed"
	}

	return fmt.Sprintf("Health check request failed: %v", err)
}

func selfAddressHint(source string) string {
	u, err := url.Parse(source)
	if err != nil {
		return ""
	}

	host := u.Hostname()
	ip := net.ParseIP(host)

	self := host == "localhost" || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified()))
	if !self {
		return ""
	}

	return fmt.Sprintf(". Inside Argus, %s is the Argus container itself: use host.docker.internal to reach a port published on the host", host)
}

func (p *Prober) runChecks(ctx context.Context, targets []*entity.Target) []probeResult {
	log := p.log.With("op", "Prober.runChecks")

	var (
		mu      sync.Mutex
		results = make([]probeResult, 0, len(targets))
	)

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(probeWorkers)

	for _, target := range targets {
		group.Go(func() error {
			probe, reason, err := p.checkProbe(groupCtx, target)
			if err != nil {
				log.Error("failed to check probe", "source", target.Source, "error", err)

				return nil
			}

			if groupCtx.Err() != nil {
				return nil
			}

			mu.Lock()
			results = append(results, probeResult{target: target, probe: probe, reason: reason})
			mu.Unlock()

			return nil
		})
	}

	_ = group.Wait()

	return results
}

func (p *Prober) transition(result probeResult) (string, *entity.Alert) {
	id := result.target.ID
	current := result.target.Status

	if result.probe.Success {
		delete(p.failures, id)

		switch current {
		case statusUp:
			return "", nil
		case statusDown:
			return statusUp, &entity.Alert{TargetID: &id, Severity: entity.SeverityInfo, Details: "Service is back up"}
		default:
			return statusUp, nil
		}
	}

	p.failures[id]++

	if current == statusDown || p.failures[id] < p.cfg.Prober.FailureThreshold {
		return "", nil
	}

	return statusDown, &entity.Alert{TargetID: &id, Severity: entity.SeverityError, Details: result.reason}
}

func (p *Prober) handleTransitions(ctx context.Context, results []probeResult) {
	log := p.log.With("op", "Prober.handleTransitions")

	for _, result := range results {
		status, alert := p.transition(result)
		if status == "" {
			continue
		}

		if alert != nil && !p.raiseAlert(ctx, result.target.Source, alert) {
			continue
		}

		_, err := p.uc.UpdateTargetStatus(ctx, result.target.ID, status)
		if err != nil {
			log.Error("failed to update target status", "source", result.target.Source, "error", err)
		}
	}
}

func (p *Prober) raiseAlert(ctx context.Context, source string, alert *entity.Alert) bool {
	alert.Status = entity.AlertStatusNotSent
	_, err := p.uc.CreateAlert(ctx, alert)
	if err != nil {
		p.log.With("op", "Prober.raiseAlert").
			Error("failed to create alert", "source", source, "error", err)

		return false
	}

	return true
}

func (p *Prober) MakeProbes(ctx context.Context) error {
	const op = "Prober.MakeProbes"

	log := p.log.With("op", op)

	targets, err := p.uc.GetTargetsByType(ctx, probeTargetType)
	if err != nil {
		log.Error("failed to get targets", "error", err)

		return err
	}

	if len(targets) == 0 {
		return nil
	}

	results := p.runChecks(ctx, targets)
	if len(results) == 0 {
		return nil
	}

	probes := make([]*entity.Probe, 0, len(results))
	for _, result := range results {
		probes = append(probes, result.probe)
	}

	_, err = p.uc.SaveProbes(ctx, probes)
	if err != nil {
		log.Error("failed to save probes", "error", err)

		return err
	}

	p.handleTransitions(ctx, results)

	return nil
}

func (p *Prober) RunProber(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.Prober.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := p.MakeProbes(ctx)
			if err != nil {
				p.log.Error("failed to make probes", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
