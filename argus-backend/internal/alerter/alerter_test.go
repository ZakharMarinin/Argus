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

func (f *fakeUseCase) GetAlertsByNotification(_ context.Context, _ bool, _ uint64) ([]*entity.Alert, error) {
	return nil, nil
}

func (f *fakeUseCase) GetTargetsByIDs(_ context.Context, ids []uuid.UUID) ([]*entity.Target, error) {
	f.gotIDs = ids

	return f.targets, nil
}

func (f *fakeUseCase) MarkAlertsNotified(_ context.Context, _ []*entity.Alert) error {
	return nil
}

func TestTargetsByIDGroupsAlertsPerTarget(t *testing.T) {
	api := uuid.New()
	db := uuid.New()

	fake := &fakeUseCase{targets: []*entity.Target{
		{ID: api, Name: "api"},
		{ID: db, Name: "db"},
	}}

	a := &Alerter{uc: fake}

	got, err := a.targetsByID(context.Background(), []*entity.Alert{
		{ID: 1, TargetID: api},
		{ID: 2, TargetID: api},
		{ID: 3, TargetID: db},
	})

	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{api, db}, fake.gotIDs)
	require.Len(t, got, 2)

	byTarget := make(map[uuid.UUID][]*entity.Alert, len(got))
	for _, n := range got {
		byTarget[n.Target.ID] = n.Alerts
	}

	require.Len(t, byTarget[api], 2)
	require.Len(t, byTarget[db], 1)
}

func TestSplitMutesRepeatsWithinCooldown(t *testing.T) {
	target := uuid.New()
	now := time.Now()

	a := &Alerter{
		cfg: &config.Config{Telegram: config.Telegram{Cooldown: time.Minute}},
		lastSent: map[alertKey]time.Time{
			{target, "status 503"}: now.Add(-30 * time.Second),
			{target, "oom"}:        now.Add(-2 * time.Minute),
		},
	}

	send, muted := a.split(&notification{Alerts: []*entity.Alert{
		{ID: 1, TargetID: target, Details: "status 503"},
		{ID: 2, TargetID: target, Details: "oom"},
		{ID: 3, TargetID: target, Details: "died"},
	}}, now)

	require.Equal(t, []int{2, 3}, alertIDs(send))
	require.Equal(t, []int{1}, alertIDs(muted))
}

func TestFormatMessage(t *testing.T) {
	target := entity.Target{Name: "api-gateway", Status: "down"}

	t.Run("few alerts are listed", func(t *testing.T) {
		msg := formatMessage(&notification{Target: target, Alerts: []*entity.Alert{
			{Severity: entity.SeverityWarning, Details: "slow"},
			{Severity: entity.SeverityCritical, Details: "oom"},
		}}, 0)

		require.True(t, strings.HasPrefix(msg, "[CRITICAL] api-gateway - down"))
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

		msg := formatMessage(&notification{Target: target, Alerts: alerts}, 4)

		require.True(t, strings.HasPrefix(msg, "[CRITICAL] api-gateway - down"))
		require.Contains(t, msg, "12 alerts: CRITICAL ×1, ERROR ×11")
		require.Contains(t, msg, "latest: oom")
		require.Contains(t, msg, "+4 suppressed")
		require.NotContains(t, msg, "[ERROR] status 503")
	})

	t.Run("long details are cut, other alerts kept", func(t *testing.T) {
		msg := formatMessage(&notification{Target: target, Alerts: []*entity.Alert{
			{Severity: entity.SeverityCritical, Details: "panic: " + strings.Repeat("x", 6000)},
			{Severity: entity.SeverityError, Details: "container died (exit code 2)"},
		}}, 5)

		require.Contains(t, msg, "…\n  - [ERROR] container died (exit code 2)")
		require.Contains(t, msg, "+5 suppressed")
		require.Less(t, len(msg), maxDetailsLength+200)
	})
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
