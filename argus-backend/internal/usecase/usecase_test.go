package usecase_test

import (
	"context"
	"testing"

	"argus-collector/internal/usecase"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

type delegation struct {
	method string
	args   []any
	want   any
	zero   any
	call   func(ctx context.Context, uc *usecase.UseCase) (any, error)
}

func (d delegation) returns(storageErr error) []any {
	if d.want == nil {
		return []any{storageErr}
	}

	if storageErr != nil {
		return []any{d.zero, storageErr}
	}

	return []any{d.want, nil}
}

func (d delegation) run(t *testing.T, storageErr error) (any, error) {
	t.Helper()

	db := mocks.NewMockStorage(t)
	db.On(d.method, append([]any{mock.Anything}, d.args...)...).Return(d.returns(storageErr)...).Once()

	return d.call(t.Context(), usecase.NewUseCase(db))
}

func assertDelegates(t *testing.T, cases []delegation) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			t.Run("returns storage result", func(t *testing.T) {
				t.Parallel()

				got, err := tc.run(t, nil)
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			})

			t.Run("returns storage error as is", func(t *testing.T) {
				t.Parallel()

				_, err := tc.run(t, assert.AnError)
				require.Equal(t, assert.AnError, err)
			})
		})
	}
}
