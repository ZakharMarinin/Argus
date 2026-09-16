package alerter

import (
	"context"
	"testing"
	"time"

	"argus-collector/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeUseCase struct {
	targets []*entity.Target
	gotIDs  []uuid.UUID
}

func (f *fakeUseCase) GetAlertsByNotification(_ context.Context, _ bool) ([]*entity.Alert, error) {
	return nil, nil
}

func (f *fakeUseCase) GetTargetsByIDs(_ context.Context, ids []uuid.UUID) ([]*entity.Target, error) {
	f.gotIDs = ids

	return f.targets, nil
}

func (f *fakeUseCase) MarkAlertNotified(_ context.Context, _ int) error {
	return nil
}

func TestTargetsByIDDeduplicates(t *testing.T) {
	shared := uuid.New()
	other := uuid.New()

	fake := &fakeUseCase{targets: []*entity.Target{
		{ID: shared, Name: "api"},
		{ID: other, Name: "db"},
	}}

	a := &Alerter{uc: fake}

	got, err := a.targetsByID(context.Background(), []*entity.Alert{
		{ID: 1, TargetID: shared},
		{ID: 2, TargetID: shared},
		{ID: 3, TargetID: other},
	})

	require.NoError(t, err)
	require.Len(t, fake.gotIDs, 2, "three alerts over two targets must ask for two ids")
	require.Len(t, got, 2)
	require.Equal(t, "api", got[shared].Name)
	require.Equal(t, "db", got[other].Name)
}

func TestFormatMessage(t *testing.T) {
	targetID := uuid.New()
	alert := &entity.Alert{
		ID:        1,
		TargetID:  targetID,
		Severity:  entity.SeverityError,
		Details:   "unexpected status code 503",
		CreatedAt: time.Date(2026, 9, 17, 10, 30, 0, 0, time.UTC),
	}

	t.Run("with target", func(t *testing.T) {
		msg := formatMessage(alert, &entity.Target{
			ID:         targetID,
			Name:       "api-gateway",
			Source:     "http://api:8080/health",
			TargetType: "probe",
		})

		require.Contains(t, msg, "ERROR")
		require.Contains(t, msg, "api-gateway")
		require.Contains(t, msg, "http://api:8080/health")
		require.Contains(t, msg, "probe")
		require.Contains(t, msg, "unexpected status code 503")
		require.Contains(t, msg, "2026-09-17T10:30:00Z")
	})

	t.Run("without target falls back to id", func(t *testing.T) {
		msg := formatMessage(alert, nil)

		require.Contains(t, msg, "unknown target")
		require.Contains(t, msg, targetID.String())
		require.Contains(t, msg, "unexpected status code 503")
	})
}
