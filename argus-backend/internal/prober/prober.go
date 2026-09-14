package prober

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	probeWorkers    = 20
	probeTargetType = "probe"
)

type UseCase interface {
	SaveProbes(ctx context.Context, probes []*entity.Probe) ([]*entity.Probe, error)
	GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error)
}

type Prober struct {
	cfg *config.Config
	log *slog.Logger
	uc  UseCase
}

func NewProber(cfg *config.Config, log *slog.Logger, uc UseCase) *Prober {
	return &Prober{cfg: cfg, log: log, uc: uc}
}

func (p *Prober) checkProbe(ctx context.Context, target *entity.Target) (*entity.Probe, error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.Source, nil)
	if err != nil {
		return nil, err
	}

	client := http.Client{Timeout: p.cfg.Prober.ProbeTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	probe := &entity.Probe{
		TargetID: target.ID,
		Success:  resp.StatusCode < 300,
		Latency:  time.Since(start).Microseconds(),
	}

	return probe, nil
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

	var (
		mu     sync.Mutex
		probes = make([]*entity.Probe, 0, len(targets))
	)

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(probeWorkers)

	for _, target := range targets {
		group.Go(func() error {
			probe, err := p.checkProbe(groupCtx, target)
			if err != nil {
				log.Error("failed to check probe", "source", target.Source, "error", err)

				return nil
			}

			mu.Lock()
			probes = append(probes, probe)
			mu.Unlock()

			return nil
		})
	}

	_ = group.Wait()

	if len(probes) == 0 {
		return nil
	}

	_, err = p.uc.SaveProbes(ctx, probes)
	if err != nil {
		log.Error("failed to save probes", "error", err)

		return err
	}

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
