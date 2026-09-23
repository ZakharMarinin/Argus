package host

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	procStat    = "/proc/stat"
	procMeminfo = "/proc/meminfo"
)

var errBadFormat = errors.New("unexpected format")

type UseCase interface {
	CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error)
}

type cpuTimes struct {
	idle  uint64
	total uint64
}

type Sampler struct {
	cfg         config.Host
	log         *slog.Logger
	uc          UseCase
	statPath    string
	meminfoPath string
	hostname    string

	mu   sync.RWMutex
	last entity.HostSnapshot

	prevCPU *cpuTimes
	memory  levels
	disk    levels
	failing map[string]bool
}

func NewSampler(cfg *config.Config, log *slog.Logger, uc UseCase, name string) *Sampler {
	return &Sampler{
		cfg:         cfg.Host,
		log:         log,
		uc:          uc,
		statPath:    procStat,
		meminfoPath: procMeminfo,
		hostname:    name,
		last:        entity.HostSnapshot{Hostname: name},
		failing:     make(map[string]bool),
	}
}

func Hostname(root, fromDocker string) string {
	raw, err := os.ReadFile(filepath.Join(root, "etc", "hostname")) //nolint:gosec // root is host.disk_path from config
	if name := strings.TrimSpace(string(raw)); err == nil && name != "" {
		return name
	}

	if fromDocker != "" {
		return fromDocker
	}

	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}

	return name
}

func (s *Sampler) Snapshot() entity.HostSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.last
}

func (s *Sampler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		s.sample(ctx, time.Now())

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Sampler) sample(ctx context.Context, now time.Time) {
	snap := entity.HostSnapshot{Hostname: s.hostname, SampledAt: &now}

	cur, err := readCPU(s.statPath)
	if s.report("cpu", err) {
		if s.prevCPU != nil {
			if pct, ok := cpuPercent(*s.prevCPU, cur); ok {
				snap.CPU = &entity.HostMetric{Percent: pct, Level: entity.HostLevelOK}
			}
		}

		s.prevCPU = &cur
	}

	used, total, err := readMemory(s.meminfoPath)
	if s.report("memory", err) {
		snap.Memory = s.measure(ctx, &s.memory, "Memory", used, total)
	}

	used, total, err = readDisk(s.cfg.DiskPath)
	if s.report("disk", err) {
		snap.Disk = s.measure(ctx, &s.disk, "Disk", used, total)
	}

	s.mu.Lock()
	s.last = snap
	s.mu.Unlock()
}

func (s *Sampler) measure(ctx context.Context, l *levels, name string, used, total uint64) *entity.HostMetric {
	pct := percent(used, total)
	level := levelFor(pct, s.cfg.Warning, s.cfg.Critical)

	if l.observe(level, s.cfg.Consecutive) {
		s.raise(ctx, name, pct, l.level)
	}

	return &entity.HostMetric{Percent: pct, Used: used, Total: total, Level: l.level}
}

func (s *Sampler) raise(ctx context.Context, name string, pct float64, level string) {
	severity, threshold := entity.SeverityWarning, s.cfg.Warning
	if level == entity.HostLevelCritical {
		severity, threshold = entity.SeverityCritical, s.cfg.Critical
	}

	_, err := s.uc.CreateAlert(ctx, &entity.Alert{
		Severity: severity,
		Details:  fmt.Sprintf("%s usage is %.0f%% (%s above %.0f%%)", name, pct, level, threshold),
	})
	if err != nil {
		s.log.Error("failed to create host alert", "op", "Sampler.raise", "error", err)
	}
}

func (s *Sampler) report(source string, err error) bool {
	if err != nil {
		if !s.failing[source] {
			s.log.Warn("host metric unavailable", "source", source, "error", err)
		}

		s.failing[source] = true

		return false
	}

	if s.failing[source] {
		s.log.Info("host metric available again", "source", source)
	}

	delete(s.failing, source)

	return true
}

type levels struct {
	level  string
	recent []string
}

func (l *levels) observe(level string, need int) bool {
	if l.level == "" {
		l.level = entity.HostLevelOK
	}

	l.recent = append(l.recent, level)
	if len(l.recent) > need {
		l.recent = l.recent[1:]
	}

	if len(l.recent) < need {
		return false
	}

	floor, ceil := l.recent[0], l.recent[0]
	for _, r := range l.recent[1:] {
		if rank(r) < rank(floor) {
			floor = r
		}

		if rank(r) > rank(ceil) {
			ceil = r
		}
	}

	switch {
	case rank(floor) > rank(l.level):
		l.level = floor

		return true
	case rank(ceil) < rank(l.level):
		l.level = ceil
	}

	return false
}

func rank(level string) int {
	switch level {
	case entity.HostLevelCritical:
		return 2
	case entity.HostLevelWarning:
		return 1
	default:
		return 0
	}
}

func levelFor(pct, warning, critical float64) string {
	switch {
	case pct >= critical:
		return entity.HostLevelCritical
	case pct >= warning:
		return entity.HostLevelWarning
	default:
		return entity.HostLevelOK
	}
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}

	return float64(used) / float64(total) * 100
}

func readCPU(path string) (cpuTimes, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // fixed /proc path
	if err != nil {
		return cpuTimes{}, err
	}

	return parseCPU(string(raw))
}

func parseCPU(stat string) (cpuTimes, error) {
	line, _, _ := strings.Cut(stat, "\n")

	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, fmt.Errorf("cpu line: %w", errBadFormat)
	}

	var t cpuTimes

	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return cpuTimes{}, fmt.Errorf("cpu field %d: %w", i, err)
		}

		if i < 8 {
			t.total += v
		}

		if i == 3 || i == 4 {
			t.idle += v
		}
	}

	return t, nil
}

func cpuPercent(prev, cur cpuTimes) (float64, bool) {
	if cur.total <= prev.total || cur.idle < prev.idle {
		return 0, false
	}

	total := cur.total - prev.total
	idle := min(cur.idle-prev.idle, total)

	return float64(total-idle) / float64(total) * 100, true
}

func readMemory(path string) (uint64, uint64, error) {
	f, err := os.Open(path) //nolint:gosec // fixed /proc path
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()

	return parseMemory(bufio.NewScanner(f))
}

func parseMemory(sc *bufio.Scanner) (uint64, uint64, error) {
	var total, available uint64

	var haveTotal, haveAvailable bool

	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok || (key != "MemTotal" && key != "MemAvailable") {
			continue
		}

		kb, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("%s: %w", key, err)
		}

		if key == "MemTotal" {
			total, haveTotal = kb*1024, true
		} else {
			available, haveAvailable = kb*1024, true
		}
	}

	err := sc.Err()
	if err != nil {
		return 0, 0, err
	}

	if !haveTotal || !haveAvailable || available > total {
		return 0, 0, fmt.Errorf("MemTotal/MemAvailable: %w", errBadFormat)
	}

	return total - available, total, nil
}

func readDisk(path string) (uint64, uint64, error) {
	var st syscall.Statfs_t

	err := syscall.Statfs(path, &st)
	if err != nil {
		return 0, 0, err
	}

	used, total := diskUsage(st.Blocks, st.Bfree, st.Bavail, uint64(st.Bsize)) //nolint:gosec // block size is always positive

	return used, total, nil
}

func diskUsage(blocks, free, avail, size uint64) (uint64, uint64) {
	used := (blocks - free) * size

	return used, used + avail*size
}
