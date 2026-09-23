package handlers

import (
	"argus-collector/internal/domain/entity"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseListFilterCombinesParams(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	since := "2026-09-22T10:00:00Z"

	r := httptest.NewRequest("GET", "/log-matches?target_id="+a.String()+"&target_id="+b.String()+
		"&severity=2&since="+since+"&order=desc&limit=50&skip=100&offset=7", nil)

	f, err := parseListFilter(r)
	require.NoError(t, err)

	wantSince, _ := time.Parse(time.RFC3339, since)
	wantSeverity := entity.SeverityWarning

	assert.Equal(t, entity.ListFilter{
		TargetIDs: []uuid.UUID{a, b},
		Severity:  &wantSeverity,
		Since:     &wantSince,
		After:     7,
		Skip:      100,
		Limit:     50,
		Desc:      true,
	}, f)
}

func TestParseListFilterDefaults(t *testing.T) {
	f, err := parseListFilter(httptest.NewRequest("GET", "/alerts", nil))
	require.NoError(t, err)

	assert.Equal(t, entity.ListFilter{Limit: defaultLimit}, f)
}

func TestParseListFilterRejectsBadValues(t *testing.T) {
	for _, query := range []string{
		"target_id=nope",
		"severity=high",
		"since=yesterday",
		"skip=-1",
		"order=random",
	} {
		_, err := parseListFilter(httptest.NewRequest("GET", "/alerts?"+query, nil))
		assert.Error(t, err, query)
	}
}
