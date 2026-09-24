package usecase

import (
	"context"
	"math"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// LatencySeries отдаёт почасовой ряд p95 и доступности по всем
// uptime-таргетам за последние 24 часа.
func (uc *UseCase) LatencySeries(ctx context.Context) ([]entity.LatencyPoint, error) {
	targets, err := uc.backend.Targets(ctx)
	if err != nil {
		return nil, notReady(err)
	}

	now := time.Now().Truncate(time.Hour)
	dayAgo := now.Add(-23 * time.Hour)

	buckets := make([]hourBucket, 24)

	for _, t := range targets {
		if t.Kind != entity.KindProbe {
			continue
		}

		probes, err := uc.backend.Probes(ctx, t.ID)
		if err != nil {
			return nil, notReady(err)
		}

		fillBuckets(buckets, probes, dayAgo)
	}

	out := make([]entity.LatencyPoint, len(buckets))
	for i := range out {
		out[i].Hour = dayAgo.Add(time.Duration(i) * time.Hour)

		b := buckets[i]
		if b.total == 0 {
			continue // разрыв: в этот час проверок не было
		}

		pct := 100 * float64(b.ok) / float64(b.total)
		out[i].UptimePct = &pct

		if len(b.latencies) > 0 {
			p95 := percentileDisc95(b.latencies)
			out[i].P95MS = &p95
		}
	}

	return out, nil
}

// hourBucket - накопитель одного часа графика.
type hourBucket struct {
	latencies []float64
	total     int
	ok        int
}

func fillBuckets(buckets []hourBucket, probes []entity.ProbeOutcome, dayAgo time.Time) {
	for _, p := range probes {
		if p.CheckedAt.Before(dayAgo) {
			continue
		}

		idx := int(p.CheckedAt.Truncate(time.Hour).Sub(dayAgo) / time.Hour)
		if idx < 0 || idx >= len(buckets) {
			continue
		}

		b := &buckets[idx]
		b.total++

		if p.Success {
			b.ok++
		}

		if p.LatencyMS != nil && *p.LatencyMS > 0 {
			b.latencies = append(b.latencies, *p.LatencyMS)
		}
	}
}

// percentileDisc95 - тот же percentile_disc(0.95), что считал Postgres:
// сортировка и выбор ceil(0.95*n)-го (1-индексного) элемента.
func percentileDisc95(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)

	idx := min(max(int(math.Ceil(0.95*float64(len(sorted))))-1, 0), len(sorted)-1)

	return sorted[idx]
}
