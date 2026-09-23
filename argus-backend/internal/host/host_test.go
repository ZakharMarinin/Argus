package host

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseCPU(t *testing.T) {
	got, err := parseCPU("cpu  100 5 50 800 40 3 2 0 7 0\ncpu0 1 2 3 4 5 6 7 8\n")
	require.NoError(t, err)
	require.Equal(t, cpuTimes{idle: 840, total: 1000}, got, "guest time is already inside user, so only the first 8 fields add up")

	_, err = parseCPU("intr 1 2 3")
	require.Error(t, err)
}

func TestCPUPercent(t *testing.T) {
	pct, ok := cpuPercent(cpuTimes{idle: 800, total: 1000}, cpuTimes{idle: 875, total: 1100})
	require.True(t, ok)
	require.InDelta(t, 25.0, pct, 0.001)

	_, ok = cpuPercent(cpuTimes{idle: 800, total: 1000}, cpuTimes{idle: 800, total: 1000})
	require.False(t, ok, "no time passed between samples")
}

func TestParseMemory(t *testing.T) {
	meminfo := "MemTotal:       16000 kB\nMemFree:         1000 kB\nMemAvailable:    4000 kB\nBuffers: 10 kB\n"

	used, total, err := parseMemory(bufio.NewScanner(strings.NewReader(meminfo)))
	require.NoError(t, err)
	require.Equal(t, uint64(12000*1024), used, "page cache counts as available, not used")
	require.Equal(t, uint64(16000*1024), total)

	_, _, err = parseMemory(bufio.NewScanner(strings.NewReader("MemTotal: 16000 kB\n")))
	require.Error(t, err, "kernels without MemAvailable are not guessed at")
}

func TestDiskUsageMatchesDf(t *testing.T) {
	used, total := diskUsage(1000, 300, 250, 4096)

	require.Equal(t, uint64(700*4096), used)
	require.Equal(t, uint64(950*4096), total, "blocks reserved for root are left out, as df does")
}

func TestLevelsRaiseOnlyOnSustainedRise(t *testing.T) {
	var l levels

	steps := []struct {
		level  string
		raised bool
		want   string
	}{
		{entity.HostLevelWarning, false, entity.HostLevelOK},
		{entity.HostLevelOK, false, entity.HostLevelOK},
		{entity.HostLevelWarning, false, entity.HostLevelOK},
		{entity.HostLevelWarning, false, entity.HostLevelOK},
		{entity.HostLevelCritical, true, entity.HostLevelWarning},
		{entity.HostLevelCritical, false, entity.HostLevelWarning},
		{entity.HostLevelCritical, true, entity.HostLevelCritical},
		{entity.HostLevelWarning, false, entity.HostLevelCritical},
		{entity.HostLevelWarning, false, entity.HostLevelCritical},
		{entity.HostLevelWarning, false, entity.HostLevelWarning},
		{entity.HostLevelOK, false, entity.HostLevelWarning},
		{entity.HostLevelOK, false, entity.HostLevelWarning},
		{entity.HostLevelOK, false, entity.HostLevelOK},
	}

	for i, s := range steps {
		raised := l.observe(s.level, 3)
		require.Equal(t, s.raised, raised, "step %d: raised", i)
		require.Equal(t, s.want, l.level, "step %d: level", i)
	}
}

type fakeUseCase struct {
	alerts []*entity.Alert
}

func (f *fakeUseCase) CreateAlert(_ context.Context, a *entity.Alert) (*entity.Alert, error) {
	f.alerts = append(f.alerts, a)

	return a, nil
}

func TestSampleReportsHostAndAlertsOnce(t *testing.T) {
	dir := t.TempDir()
	stat := filepath.Join(dir, "stat")
	meminfo := filepath.Join(dir, "meminfo")

	require.NoError(t, os.WriteFile(meminfo, []byte("MemTotal: 1000 kB\nMemAvailable: 40 kB\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "etc"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "etc", "hostname"), []byte("vps-01\n"), 0o600))

	uc := &fakeUseCase{}
	cfg := &config.Config{Host: config.Host{Interval: time.Second, DiskPath: dir, Warning: 90, Critical: 95, Consecutive: 2}}
	s := NewSampler(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), uc, Hostname(dir, "from-docker"))
	s.statPath, s.meminfoPath = stat, meminfo

	require.Nil(t, s.Snapshot().SampledAt, "no sample yet")
	require.Equal(t, "vps-01", s.Snapshot().Hostname)

	for i, idle := range []int{800, 850, 900} {
		require.NoError(t, os.WriteFile(stat, []byte("cpu  "+strconv.Itoa(100*i)+" 0 0 "+strconv.Itoa(idle)+" 0 0 0 0\n"), 0o600))
		s.sample(context.Background(), time.Now())
	}

	snap := s.Snapshot()
	require.NotNil(t, snap.SampledAt)
	require.NotNil(t, snap.CPU)
	require.InDelta(t, 66.67, snap.CPU.Percent, 0.01)
	require.Equal(t, entity.HostLevelCritical, snap.Memory.Level)
	require.InDelta(t, 96.0, snap.Memory.Percent, 0.001)
	require.NotNil(t, snap.Disk)

	require.Len(t, uc.alerts, 1, "one alert for the rise, none while it stays critical")
	require.Nil(t, uc.alerts[0].TargetID)
	require.Equal(t, entity.SeverityCritical, uc.alerts[0].Severity)
	require.Equal(t, "Memory usage is 96% (critical above 95%)", uc.alerts[0].Details)

	require.NoError(t, os.Remove(meminfo))
	s.sample(context.Background(), time.Now())
	require.Nil(t, s.Snapshot().Memory, "a source that can't be read drops only its own metric")
	require.NotNil(t, s.Snapshot().Disk)
}


func TestHostnamePrefersFileThenDocker(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "etc"), 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "etc", "hostname"), []byte("\n"), 0o600))
	require.Equal(t, "fedora", Hostname(dir, "fedora"), "an empty /etc/hostname (transient hostname) falls through to Docker")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "etc", "hostname"), []byte("vps-01\n"), 0o600))
	require.Equal(t, "vps-01", Hostname(dir, "fedora"))
}
