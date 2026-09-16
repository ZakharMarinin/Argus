package usecase_test

import (
	"context"
	"testing"

	"argus-collector/internal/domain/entity"
	"argus-collector/internal/usecase"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestUseCase_CreateTarget(t *testing.T) {
	type args struct{ target *entity.Target }
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		in      args
		wantErr bool
	}{
		{name: "success", setup: func(m *mocks.MockStorage) {
			m.On("CreateTarget", mock.Anything, mock.Anything).Return(&entity.Target{ID: uuid.New(), Source: "src", Name: "t1", Status: "ok", TargetType: "svc"}, nil).Once()
		}, in: args{target: &entity.Target{Source: "src", Name: "t1", Status: "ok", TargetType: "svc"}}, wantErr: false},
		{name: "error", setup: func(m *mocks.MockStorage) {
			m.On("CreateTarget", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
		}, in: args{target: &entity.Target{}}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			res, err := uc.CreateTarget(context.Background(), tc.in.target)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "CreateTarget")
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)
				require.NotEqual(t, uuid.Nil, res.ID)
			}
		})
	}
}

func TestUseCase_UpdateTargetStatus(t *testing.T) {
	type args struct {
		id     uuid.UUID
		status string
	}
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		in      args
		wantErr bool
	}{
		{name: "success", setup: func(m *mocks.MockStorage) {
			m.On("UpdateTargetStatus", mock.Anything, mock.Anything, "newstatus").Return(&entity.Target{ID: uuid.New(), Status: "newstatus"}, nil).Once()
		}, in: args{id: uuid.New(), status: "newstatus"}, wantErr: false},
		{name: "error", setup: func(m *mocks.MockStorage) {
			m.On("UpdateTargetStatus", mock.Anything, mock.Anything, "errstatus").Return(nil, assert.AnError).Once()
		}, in: args{id: uuid.New(), status: "errstatus"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			res, err := uc.UpdateTargetStatus(context.Background(), tc.in.id, tc.in.status)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "UpdateTargetStatus")
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)
				require.Equal(t, tc.in.status, res.Status)
			}
		})
	}
}

func TestUseCase_DeleteTarget(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		id      uuid.UUID
		wantErr bool
	}{
		{name: "success", setup: func(m *mocks.MockStorage) {
			m.On("DeleteTarget", mock.Anything, mock.Anything).Return(nil).Once()
		}, id: uuid.New(), wantErr: false},
		{name: "error", setup: func(m *mocks.MockStorage) {
			m.On("DeleteTarget", mock.Anything, mock.Anything).Return(assert.AnError).Once()
		}, id: uuid.New(), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			err := uc.DeleteTarget(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "DeleteTarget")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUseCase_GetTarget(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		id      uuid.UUID
		wantErr bool
	}{
		{name: "found", setup: func(m *mocks.MockStorage) {
			m.On("GetTarget", mock.Anything, mock.Anything).Return(&entity.Target{ID: uuid.New()}, nil).Once()
		}, id: uuid.New(), wantErr: false},
		{name: "notfound", setup: func(m *mocks.MockStorage) {
			m.On("GetTarget", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
		}, id: uuid.New(), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			res, err := uc.GetTarget(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "GetTarget")
				require.Nil(t, res)
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)
			}
		})
	}
}

func TestUseCase_GetTargetsByType(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		typ     string
		wantErr bool
	}{
		{name: "found", setup: func(m *mocks.MockStorage) {
			m.On("GetTargetsByType", mock.Anything, "web").Return([]*entity.Target{{ID: uuid.New()}}, nil).Once()
		}, typ: "web", wantErr: false},
		{name: "error", setup: func(m *mocks.MockStorage) {
			m.On("GetTargetsByType", mock.Anything, "db").Return(nil, assert.AnError).Once()
		}, typ: "db", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			res, err := uc.GetTargetsByType(context.Background(), tc.typ)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "GetTargetsByType")
				require.Nil(t, res)
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)
			}
		})
	}
}

func TestUseCase_GetTargetsByIDs(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}

	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		wantErr bool
	}{
		{name: "ok", setup: func(m *mocks.MockStorage) {
			m.On("GetTargetsByIDs", mock.Anything, ids).
				Return([]*entity.Target{{ID: ids[0]}, {ID: ids[1]}}, nil).Once()
		}, wantErr: false},
		{name: "err", setup: func(m *mocks.MockStorage) {
			m.On("GetTargetsByIDs", mock.Anything, ids).Return(nil, assert.AnError).Once()
		}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			res, err := uc.GetTargetsByIDs(context.Background(), ids)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "GetTargetsByIDs")
				require.Nil(t, res)
			} else {
				require.NoError(t, err)
				require.Len(t, res, 2)
			}
		})
	}
}

func TestUseCase_GetTargetsByStatus(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		status  string
		wantErr bool
	}{
		{name: "ok", setup: func(m *mocks.MockStorage) {
			m.On("GetTargetsByStatus", mock.Anything, "up").Return([]*entity.Target{{ID: uuid.New()}}, nil).Once()
		}, status: "up", wantErr: false},
		{name: "err", setup: func(m *mocks.MockStorage) {
			m.On("GetTargetsByStatus", mock.Anything, "down").Return(nil, assert.AnError).Once()
		}, status: "down", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			res, err := uc.GetTargetsByStatus(context.Background(), tc.status)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "GetTargetsByStatus")
				require.Nil(t, res)
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)
			}
		})
	}
}

func TestUseCase_GetAllTargets(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		limit   uint64
		offset  uint64
		wantErr bool
	}{
		{name: "ok", setup: func(m *mocks.MockStorage) {
			m.On("GetAllTargets", mock.Anything, uint64(10), uint64(0)).Return([]*entity.Target{{ID: uuid.New()}}, nil).Once()
		}, limit: 10, offset: 0, wantErr: false},
		{name: "err", setup: func(m *mocks.MockStorage) {
			m.On("GetAllTargets", mock.Anything, uint64(5), uint64(1)).Return(nil, assert.AnError).Once()
		}, limit: 5, offset: 1, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			res, err := uc.GetAllTargets(context.Background(), tc.limit, tc.offset)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "GetAllTargets")
				require.Nil(t, res)
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)
			}
		})
	}
}
