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
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeUseCase struct {
	err    error
	alerts []*entity.Alert
}

func (f *fakeUseCase) CreateAlert(_ context.Context, a *entity.Alert) (*entity.Alert, error) {
	f.alerts = append(f.alerts, a)

	return a, f.err
}

func TestHostname(t *testing.T) {
	t.Parallel()

	fallback, err := os.Hostname()
	require.NoError(t, err)

	cases := []struct {
		name       string
		file       *string
		fromDocker string
		want       string
	}{
		{name: "host's /etc/hostname wins", file: new("vps-01\n"), fromDocker: "fedora", want: "vps-01"},
		{name: "an empty /etc/hostname (transient hostname) falls through to Docker", file: new("\n"), fromDocker: "fedora", want: "fedora"},
		{name: "no file uses Docker's name", fromDocker: "fedora", want: "fedora"},
		{name: "neither falls back to the container's own name", want: fallback},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tc.file != nil {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "etc"), 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "etc", "hostname"), []byte(*tc.file), 0o600))
			}

			require.Equal(t, tc.want, Hostname(dir, tc.fromDocker))
		})
	}
}

func writeHostFiles(t *testing.T, dir, meminfo string) (stat, mem string) {
	t.Helper()

	stat, mem = filepath.Join(dir, "stat"), filepath.Join(dir, "meminfo")
	require.NoError(t, os.WriteFile(stat, []byte("cpu  100 0 0 800 0 0 0 0\n"), 0o600))
	require.NoError(t, os.WriteFile(mem, []byte(meminfo), 0o600))

	return stat, mem
}

func TestRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const interval = 15 * time.Second

		dir := t.TempDir()
		stat, mem := writeHostFiles(t, dir, "MemTotal: 1000 kB\nMemAvailable: 500 kB\n")

		cfg := &config.Config{Host: config.Host{Interval: interval, DiskPath: dir, Warning: 90, Critical: 95, Consecutive: 3}}
		s := NewSampler(cfg, slog.New(slog.DiscardHandler), &fakeUseCase{}, "vps-01")
		s.statPath, s.meminfoPath = stat, mem

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		start := time.Now()

		go func() {
			s.Run(ctx)
			close(done)
		}()

		synctest.Wait()
		require.NotNil(t, s.Snapshot().SampledAt, "the first sample is taken at start, not after one interval")
		require.True(t, s.Snapshot().SampledAt.Equal(start))

		synctest.Sleep(interval)
		require.True(t, s.Snapshot().SampledAt.Equal(start.Add(interval)))

		cancel()
		<-done
	})
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

func TestSampleAlerts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		available int
		alertErr  error
		wantLevel string
		wantAlert string
		wantSev   entity.Severity
	}{
		{
			name: "memory past the warning line raises a warning", available: 80,
			wantLevel: entity.HostLevelWarning, wantAlert: "Memory usage is 92% (warning above 90%)", wantSev: entity.SeverityWarning,
		},
		{name: "healthy memory stays quiet", available: 500, wantLevel: entity.HostLevelOK},
		{
			name: "a failed alert still leaves the snapshot", available: 40, alertErr: assert.AnError,
			wantLevel: entity.HostLevelCritical, wantAlert: "Memory usage is 96% (critical above 95%)", wantSev: entity.SeverityCritical,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			stat, mem := writeHostFiles(t, dir, "MemTotal: 1000 kB\nMemAvailable: "+strconv.Itoa(tc.available)+" kB\n")

			uc := &fakeUseCase{err: tc.alertErr}
			cfg := &config.Config{Host: config.Host{DiskPath: dir, Warning: 90, Critical: 95, Consecutive: 1}}
			s := NewSampler(cfg, slog.New(slog.DiscardHandler), uc, "vps-01")
			s.statPath, s.meminfoPath = stat, mem

			s.sample(t.Context(), time.Now())

			require.Equal(t, tc.wantLevel, s.Snapshot().Memory.Level)

			if tc.wantAlert == "" {
				require.Empty(t, uc.alerts)

				return
			}

			require.Len(t, uc.alerts, 1)
			require.Equal(t, tc.wantSev, uc.alerts[0].Severity)
			require.Equal(t, tc.wantAlert, uc.alerts[0].Details)
		})
	}
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

func TestLevelFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pct  float64
		want string
	}{
		{pct: 89.9, want: entity.HostLevelOK},
		{pct: 90, want: entity.HostLevelWarning},
		{pct: 94.9, want: entity.HostLevelWarning},
		{pct: 95, want: entity.HostLevelCritical},
		{pct: 100, want: entity.HostLevelCritical},
	}

	for _, tc := range cases {
		t.Run(strconv.FormatFloat(tc.pct, 'f', -1, 64), func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, levelFor(tc.pct, 90, 95))
		})
	}
}

func TestPercent(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 25.0, percent(1, 4), 0.001)
	require.Zero(t, percent(0, 0), "an unreadable total is 0%, not a division by zero")
}

func TestParseCPU(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		stat    string
		want    cpuTimes
		wantErr bool
	}{
		{
			name: "only the first 8 fields add up, guest time is already inside user",
			stat: "cpu  100 5 50 800 40 3 2 0 7 0\ncpu0 1 2 3 4 5 6 7 8\n",
			want: cpuTimes{idle: 840, total: 1000},
		},
		{name: "not the aggregate cpu line", stat: "intr 1 2 3", wantErr: true},
		{name: "too few fields", stat: "cpu 1 2 3", wantErr: true},
		{name: "non-numeric field", stat: "cpu 1 2 x 4 5", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseCPU(tc.stat)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestCPUPercent(t *testing.T) {
	pct, ok := cpuPercent(cpuTimes{idle: 800, total: 1000}, cpuTimes{idle: 875, total: 1100})
	require.True(t, ok)
	require.InDelta(t, 25.0, pct, 0.001)

	_, ok = cpuPercent(cpuTimes{idle: 800, total: 1000}, cpuTimes{idle: 800, total: 1000})
	require.False(t, ok, "no time passed between samples")
}

func TestParseMemory(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		meminfo   string
		wantUsed  uint64
		wantTotal uint64
		wantErr   bool
	}{
		{
			name:     "page cache counts as available, not used",
			meminfo:  "MemTotal:       16000 kB\nMemFree:         1000 kB\nMemAvailable:    4000 kB\nBuffers: 10 kB\n",
			wantUsed: 12000 * 1024, wantTotal: 16000 * 1024,
		},
		{name: "kernels without MemAvailable are not guessed at", meminfo: "MemTotal: 16000 kB\n", wantErr: true},
		{name: "more available than total is rejected", meminfo: "MemTotal: 100 kB\nMemAvailable: 200 kB\n", wantErr: true},
		{name: "non-numeric value", meminfo: "MemTotal: lots kB\nMemAvailable: 1 kB\n", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			used, total, err := parseMemory(bufio.NewScanner(strings.NewReader(tc.meminfo)))
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantUsed, used)
			require.Equal(t, tc.wantTotal, total)
		})
	}
}

func TestReadDisk(t *testing.T) {
	t.Parallel()

	used, total, err := readDisk(t.TempDir())
	require.NoError(t, err)
	require.Positive(t, total)
	require.LessOrEqual(t, used, total)

	_, _, err = readDisk(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}

func TestDiskUsageMatchesDf(t *testing.T) {
	used, total := diskUsage(1000, 300, 250, 4096)
	require.Equal(t, uint64(700*4096), used)
	require.Equal(t, uint64(950*4096), total, "blocks reserved for root are left out, as df does")
}
