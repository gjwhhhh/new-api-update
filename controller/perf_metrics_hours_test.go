package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/pkg/perf_metrics"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPerfMetricsGroupsRejectsUnsupportedHours(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, rawHours := range []string{"", "0", "24", "abc"} {
		t.Run(rawHours, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(
				http.MethodGet,
				"/api/perf-metrics/groups?hours="+rawHours,
				nil,
			)

			GetPerfMetricsGroups(ctx)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.JSONEq(t, `{"success":false,"message":"hours must be 48 or 168"}`, recorder.Body.String())
		})
	}
}

func TestGetPerfMetricsChannelsRejectsUnsupportedHours(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/channel/status-metrics?hours=24",
		nil,
	)

	GetPerfMetricsChannels(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.JSONEq(t, `{"success":false,"message":"hours must be 48 or 168"}`, recorder.Body.String())
}

func TestClearPerfMetricGroupSamplesRejectsUnsupportedHours(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "missing", body: `{"group":"default"}`},
		{name: "zero", body: `{"group":"default","hours":0}`},
		{name: "legacy-24", body: `{"group":"default","hours":24}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(
				http.MethodPost,
				"/api/perf-metrics/groups/clear",
				bytes.NewBufferString(testCase.body),
			)

			ClearPerfMetricGroupSamples(ctx)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.JSONEq(t, `{"success":false,"message":"`+perfmetrics.ErrUnsupportedGroupHours.Error()+`"}`, recorder.Body.String())
		})
	}
}

func TestClearPerfMetricChannelSamplesRejectsUnsupportedHours(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "missing", body: `{}`},
		{name: "zero", body: `{"hours":0}`},
		{name: "legacy-24", body: `{"hours":24}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = []gin.Param{{Key: "id", Value: "1"}}
			ctx.Request = httptest.NewRequest(
				http.MethodPost,
				"/api/channel/1/status-metrics/clear",
				bytes.NewBufferString(testCase.body),
			)

			ClearPerfMetricChannelSamples(ctx)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.JSONEq(t, `{"success":false,"message":"`+perfmetrics.ErrUnsupportedGroupHours.Error()+`"}`, recorder.Body.String())
		})
	}
}
