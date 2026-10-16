package alerter

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeUseCase struct {
	targets []*entity.Target
	gotIDs  []uuid.UUID
}

func (f *fakeUseCase) GetAlertsByNotification(_ context.Context, _ entity.AlertStatus, _ uint64) ([]*entity.Alert, error) {
	return nil, nil
}

func (f *fakeUseCase) GetTargetsByIDs(_ context.Context, ids []uuid.UUID) ([]*entity.Target, error) {
	f.gotIDs = ids

	return f.targets, nil
}

func (f *fakeUseCase) MarkAlertsNotified(_ context.Context, _ []*entity.Alert, _ entity.AlertStatus) error {
	return nil
}

func TestGroupSplitsTargetsAndHost(t *testing.T) {
	api := uuid.New()
	db := uuid.New()

	fake := &fakeUseCase{targets: []*entity.Target{
		{ID: api, Name: "api", Status: "down"},
		{ID: db, Name: "db", Status: "up"},
	}}

	a := &Alerter{uc: fake, hostName: "vps-01"}

	got, err := a.group(context.Background(), []*entity.Alert{
		{ID: 1, TargetID: &api},
		{ID: 2, TargetID: &api},
		{ID: 3, TargetID: &db},
		{ID: 4},
		{ID: 5},
	})

	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{api, db}, fake.gotIDs, "host alerts don't ask storage for a target")
	require.Len(t, got, 3)

	byName := make(map[string]*notification, len(got))
	for _, n := range got {
		byName[n.Name] = n
	}

	require.Len(t, byName["api"].Alerts, 2)
	require.Equal(t, api, byName["api"].Key)
	require.Equal(t, "down", byName["api"].Status)
	require.Len(t, byName["db"].Alerts, 1)

	host := byName["Host · vps-01"]
	require.NotNil(t, host)
	require.Equal(t, uuid.Nil, host.Key)
	require.Equal(t, "n/a", host.Status)
	require.Equal(t, []int{4, 5}, alertIDs(host.Alerts))
}

func TestGroupWithOnlyHostAlertsSkipsStorage(t *testing.T) {
	fake := &fakeUseCase{}
	a := &Alerter{uc: fake, hostName: "vps-01"}

	got, err := a.group(context.Background(), []*entity.Alert{{ID: 1}})

	require.NoError(t, err)
	require.Nil(t, fake.gotIDs)
	require.Len(t, got, 1)
}

func TestSplitMutesRepeatsWithinCooldown(t *testing.T) {
	target := uuid.New()
	now := time.Now()

	a := &Alerter{
		cfg: &config.Config{Telegram: config.Telegram{Cooldown: time.Minute}},
		lastSent: map[alertKey]time.Time{
			{target, "status 503"}: now.Add(-30 * time.Second),
			{target, "oom"}:        now.Add(-2 * time.Minute),
			{uuid.Nil, "Disk usage is 93% (warning above 90%)"}: now.Add(-30 * time.Second),
		},
	}

	send, muted := a.split(&notification{Alerts: []*entity.Alert{
		{ID: 1, TargetID: &target, Details: "status 503"},
		{ID: 2, TargetID: &target, Details: "oom"},
		{ID: 3, TargetID: &target, Details: "died"},
		{ID: 4, Details: "Disk usage is 93% (warning above 90%)"},
		{ID: 5, Details: "Memory usage is 91% (warning above 90%)"},
	}}, now)

	require.Equal(t, []int{2, 3, 5}, alertIDs(send))
	require.Equal(t, []int{1, 4}, alertIDs(muted))
}

func TestFormatMessage(t *testing.T) {
	target := notification{Name: "api-gateway", Status: "down"}

	t.Run("few alerts are listed", func(t *testing.T) {
		msg := formatMessage(&notification{Name: target.Name, Status: target.Status, Alerts: []*entity.Alert{
			{Severity: entity.SeverityWarning, Details: "slow"},
			{Severity: entity.SeverityCritical, Details: "oom"},
		}}, 0)

		require.True(t, strings.HasPrefix(msg, "api-gateway - down"))
		require.Contains(t, msg, "[WARNING] slow")
		require.Contains(t, msg, "[CRITICAL] oom")
		require.NotContains(t, msg, "suppressed")
	})

	t.Run("many alerts are summarized", func(t *testing.T) {
		alerts := make([]*entity.Alert, 0, maxListedAlerts+2)
		for range maxListedAlerts + 1 {
			alerts = append(alerts, &entity.Alert{Severity: entity.SeverityError, Details: "status 503"})
		}

		alerts = append(alerts, &entity.Alert{Severity: entity.SeverityCritical, Details: "oom"})

		msg := formatMessage(&notification{Name: target.Name, Status: target.Status, Alerts: alerts}, 4)

		require.True(t, strings.HasPrefix(msg, "api-gateway - down"))
		require.Contains(t, msg, "12 alerts: CRITICAL ×1, ERROR ×11")
		require.Contains(t, msg, "latest: oom")
		require.Contains(t, msg, "+4 suppressed")
		require.NotContains(t, msg, "[ERROR] status 503")
	})

	t.Run("host has no status", func(t *testing.T) {
		msg := formatMessage(&notification{Name: "Host · vps-01", Status: "n/a", Alerts: []*entity.Alert{
			{Severity: entity.SeverityWarning, Details: "Disk usage is 93% (warning above 90%)"},
		}}, 0)

		require.True(t, strings.HasPrefix(msg, "Host · vps-01 - n/a"))
	})

	t.Run("long details are cut, other alerts kept", func(t *testing.T) {
		msg := formatMessage(&notification{Name: target.Name, Status: target.Status, Alerts: []*entity.Alert{
			{Severity: entity.SeverityCritical, Details: "panic: " + strings.Repeat("x", 6000)},
			{Severity: entity.SeverityError, Details: "container died (exit code 2)"},
		}}, 5)

		require.Contains(t, msg, "…\n  - [ERROR] container died (exit code 2)")
		require.Contains(t, msg, "+5 suppressed")
		require.Less(t, len(msg), maxDetailsLength+200)
	})
}

func TestSplit(t *testing.T) {
	t.Parallel()

	targetID := uuid.New()
	otherID := uuid.New()
	now := time.Now()

	cases := []struct {
		name          string
		n             *notification
		now           time.Time
		lastSent      map[alertKey]time.Time
		expectedSend  []*entity.Alert
		expectedMuted []*entity.Alert
	}{
		{
			name: "only dups test",
			n: &notification{Alerts: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 2, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 3, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 4, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 5, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			}},
			now: time.Now(),
			expectedSend: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			},
			expectedMuted: []*entity.Alert{
				{ID: 2, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 3, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 4, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 5, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			},
		},
		{
			name: "dups + some unique test",
			n: &notification{Alerts: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 2, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 3, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 4, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 5, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 6, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "unique"},
			}},
			now: time.Now(),
			expectedSend: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 6, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "unique"},
			},
			expectedMuted: []*entity.Alert{
				{ID: 2, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 3, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 4, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 5, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			},
		},
		{
			name: "cooldown not expired mutes the alert",
			n: &notification{Alerts: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			}},
			now:      now,
			lastSent: map[alertKey]time.Time{{targetID, "dup"}: now.Add(-time.Minute)},
			expectedMuted: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			},
		},
		{
			name: "cooldown expired sends the alert",
			n: &notification{Alerts: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			}},
			now:      now,
			lastSent: map[alertKey]time.Time{{targetID, "dup"}: now.Add(-10 * time.Minute)},
			expectedSend: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			},
		},
		{
			name: "dups in one tick under active cooldown are all muted",
			n: &notification{Alerts: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 2, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 3, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "other"},
			}},
			now:      now,
			lastSent: map[alertKey]time.Time{{targetID, "dup"}: now.Add(-time.Minute)},
			expectedSend: []*entity.Alert{
				{ID: 3, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "other"},
			},
			expectedMuted: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 2, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
			},
		},
		{
			name: "same details on another target are not dups",
			n: &notification{Alerts: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 2, TargetID: &otherID, Severity: entity.SeverityCritical, Details: "dup"},
			}},
			now: now,
			expectedSend: []*entity.Alert{
				{ID: 1, TargetID: &targetID, Severity: entity.SeverityCritical, Details: "dup"},
				{ID: 2, TargetID: &otherID, Severity: entity.SeverityCritical, Details: "dup"},
			},
		},
		{
			name: "no alerts",
			n:    &notification{},
			now:  now,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := &Alerter{
				cfg:      &config.Config{Telegram: config.Telegram{Cooldown: 5 * time.Minute}},
				lastSent: tc.lastSent,
			}

			send, muted := a.split(tc.n, tc.now)

			require.Equal(t, tc.expectedSend, send)
			require.Equal(t, tc.expectedMuted, muted)
		})
	}
}

func TestTruncateCountsUTF16(t *testing.T) {
	got := truncate(strings.Repeat("😱", telegramMessageLimit), telegramMessageLimit)

	require.LessOrEqual(t, len(utf16.Encode([]rune(got))), telegramMessageLimit)
	require.True(t, strings.HasSuffix(got, "…"))

	exact := strings.Repeat("я", telegramMessageLimit)
	require.Equal(t, exact, truncate(exact, telegramMessageLimit))
}

func alertIDs(alerts []*entity.Alert) []int {
	ids := make([]int, 0, len(alerts))
	for _, alert := range alerts {
		ids = append(ids, alert.ID)
	}

	return ids
}
