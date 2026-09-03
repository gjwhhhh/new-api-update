package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIStatusTracksChatGPTAndCodex(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	payload := openAITrackedGroupsTestPayload()
	payload.Summary.AffectedComponents = []openaiWidgetComponent{
		{ID: "chatgpt-login", Status: "partial_outage"},
		{ID: "codex-api", Status: "degraded_performance"},
		{ID: "responses", Status: "full_outage"},
	}
	payload.Summary.OngoingIncidents = []openaiWidgetIncident{
		{
			ID:        "chatgpt-codex",
			Name:      "Elevated errors across ChatGPT and Codex",
			Status:    "investigating",
			Impact:    "major",
			UpdatedAt: "2026-09-02T08:00:00Z",
			AffectedComponents: []openaiWidgetComponent{
				{ID: "chatgpt-login", Status: "partial_outage"},
				{ID: "codex-api", Status: "degraded_performance"},
			},
		},
		{
			ID:     "api-only",
			Name:   "Responses API disruption",
			Impact: "critical",
			AffectedComponents: []openaiWidgetComponent{
				{ID: "responses", Status: "full_outage"},
			},
		},
	}

	result := normalizeOpenAIStatus(payload, nil, now)

	require.True(t, result.Available)
	assert.Equal(t, "major", result.Indicator)
	require.Len(t, result.Groups, 2)
	assert.Equal(t, []string{"ChatGPT", "Codex"}, []string{result.Groups[0].Name, result.Groups[1].Name})
	assert.Equal(t, "partial_outage", result.Groups[0].Components[0].Status)
	assert.Equal(t, "degraded_performance", result.Groups[1].Components[0].Status)

	require.Len(t, result.Incidents, 1)
	assert.Equal(t, "chatgpt-codex", result.Incidents[0].ID)
	assert.Equal(t, []string{"Login", "Codex API"}, result.Incidents[0].AffectedComponents)
	assert.Equal(t, []string{"ChatGPT", "Codex"}, result.Incidents[0].AffectedGroups)
}

func TestNormalizeOpenAIStatusHistoryUsesTrackedGroups(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	result := normalizeOpenAIStatus(openAITrackedGroupsTestPayload(), &openaiImpactsPayload{
		ComponentImpacts: []openaiImpactWindow{
			{
				ID:                   "chatgpt-impact",
				ComponentID:          "chatgpt-login",
				Status:               "partial_outage",
				StartAt:              "2026-08-20T10:00:00Z",
				EndAt:                "2026-08-20T12:00:00Z",
				StatusPageIncidentID: "chatgpt-incident",
			},
			{
				ID:                   "codex-impact",
				ComponentID:          "codex-api",
				Status:               "degraded_performance",
				StartAt:              "2026-08-15T10:00:00Z",
				EndAt:                "2026-08-15T12:00:00Z",
				StatusPageIncidentID: "codex-incident",
			},
			{
				ID:                   "api-impact",
				ComponentID:          "responses",
				Status:               "full_outage",
				StartAt:              "2026-08-25T10:00:00Z",
				EndAt:                "2026-08-25T12:00:00Z",
				StatusPageIncidentID: "api-incident",
			},
		},
		IncidentLinks: []openaiIncidentLink{
			{
				ID:          "chatgpt-incident",
				Name:        "ChatGPT login issues",
				Status:      "resolved",
				Permalink:   "https://status.openai.com/incidents/chatgpt-incident",
				PublishedAt: "2026-08-20T10:05:00Z",
			},
			{
				ID:          "codex-incident",
				Name:        "Codex API latency",
				Status:      "resolved",
				Permalink:   "https://status.openai.com/incidents/codex-incident",
				PublishedAt: "2026-08-15T10:05:00Z",
			},
		},
	}, now)

	require.Len(t, result.History, 2)
	assert.Equal(t, "chatgpt-incident", result.History[0].ID)
	assert.Equal(t, []string{"ChatGPT"}, result.History[0].AffectedGroups)
	assert.Equal(t, "codex-incident", result.History[1].ID)
	assert.Equal(t, []string{"Codex"}, result.History[1].AffectedGroups)
}

func TestNormalizeOpenAIStatusBuildsSeparateGroupUptimeBars(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	result := normalizeOpenAIStatus(openAITrackedGroupsTestPayload(), &openaiImpactsPayload{
		ComponentUptimes: []openaiComponentUptime{
			{GroupID: "chatgpt-group", Uptime: "99.62"},
			{GroupID: "codex-group", Uptime: "99.81"},
			{ComponentID: "chatgpt-login", Uptime: "99.62"},
			{ComponentID: "codex-api", Uptime: "99.81"},
		},
		ComponentImpacts: []openaiImpactWindow{
			{
				ComponentID: "chatgpt-login",
				Status:      "full_outage",
				StartAt:     "2026-08-20T10:00:00Z",
				EndAt:       "2026-08-20T12:00:00Z",
			},
			{
				ComponentID: "codex-api",
				Status:      "partial_outage",
				StartAt:     "2026-08-15T10:00:00Z",
				EndAt:       "2026-08-15T12:00:00Z",
			},
		},
	}, now)

	require.Len(t, result.Groups, 2)
	chatGPT := result.Groups[0]
	codex := result.Groups[1]
	require.NotNil(t, chatGPT.UptimePercent)
	require.NotNil(t, codex.UptimePercent)
	assert.InDelta(t, 99.62, *chatGPT.UptimePercent, 0.0001)
	assert.InDelta(t, 99.81, *codex.UptimePercent, 0.0001)
	require.Len(t, chatGPT.Series, 90)
	require.Len(t, codex.Series, 90)
	assert.Equal(t, "full_outage", uptimeStatusOn(t, chatGPT.Series, "2026-08-20"))
	assert.Equal(t, "operational", uptimeStatusOn(t, chatGPT.Series, "2026-08-15"))
	assert.Equal(t, "partial_outage", uptimeStatusOn(t, codex.Series, "2026-08-15"))
	assert.Equal(t, "operational", uptimeStatusOn(t, codex.Series, "2026-08-20"))
}

func TestNormalizeOpenAIStatusEmptyPayload(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	result := normalizeOpenAIStatus(openaiWidgetPayload{}, nil, now)
	require.True(t, result.Available)
	assert.Equal(t, "none", result.Indicator)
	assert.Empty(t, result.Groups)
	assert.Empty(t, result.Incidents)
	assert.Empty(t, result.History)
	assert.Equal(t, openaiStatusPageURL, result.SourceURL)
}

func TestBuildOpenAIUptimeSeriesOngoingImpact(t *testing.T) {
	now := time.Date(2026, 9, 2, 18, 0, 0, 0, time.UTC)
	series := buildOpenAIUptimeSeries(
		map[string]struct{}{"chatgpt-login": {}},
		map[string]string{"chatgpt-login": "Login"},
		[]openaiImpactWindow{{
			ComponentID: "chatgpt-login",
			Status:      "degraded_performance",
			StartAt:     "2026-09-02T10:00:00Z",
		}},
		nil,
		now,
	)
	assert.Equal(t, "degraded_performance", uptimeStatusOn(t, series, "2026-09-02"))
	assert.Equal(t, "operational", uptimeStatusOn(t, series, "2026-09-01"))
}

func TestBuildOpenAIHourlySeriesWithIncidentDetails(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 30, 0, 0, time.UTC)
	series := buildOpenAIHourlySeries(
		map[string]struct{}{"codex-api": {}},
		map[string]string{"codex-api": "Codex API"},
		[]openaiImpactWindow{{
			ID:                   "impact-1",
			ComponentID:          "codex-api",
			Status:               "degraded_performance",
			StartAt:              "2026-09-02T10:00:00Z",
			EndAt:                "2026-09-02T11:30:00Z",
			StatusPageIncidentID: "inc-1",
		}},
		[]openaiIncidentLink{{
			ID:        "inc-1",
			Name:      "Elevated errors in Codex",
			Status:    "monitoring",
			Permalink: "https://status.openai.com/incidents/inc-1",
		}},
		now,
	)

	hour := series[21]
	require.Equal(t, "2026-09-02T10:00:00Z", hour.Ts)
	require.Equal(t, "degraded_performance", hour.Status)
	require.Len(t, hour.Events, 1)
	assert.Equal(t, "Elevated errors in Codex", hour.Events[0].Name)
	assert.Equal(t, []string{"Codex API"}, hour.Events[0].ComponentNames)
	assert.Equal(t, "monitoring", hour.Events[0].IncidentStatus)
}

func openAITrackedGroupsTestPayload() openaiWidgetPayload {
	return openaiWidgetPayload{
		Summary: openaiWidgetSummary{
			Structure: openaiWidgetStructure{
				Items: []openaiWidgetStructureItem{
					{Group: &openaiWidgetGroup{
						ID:   "apis-group",
						Name: "APIs",
						Components: []openaiWidgetGroupComponent{
							{ComponentID: "responses", Name: "Responses"},
						},
					}},
					{Group: &openaiWidgetGroup{
						ID:   "chatgpt-group",
						Name: "ChatGPT",
						Components: []openaiWidgetGroupComponent{
							{ComponentID: "chatgpt-login", Name: "Login"},
						},
					}},
					{Group: &openaiWidgetGroup{
						ID:   "codex-group",
						Name: "Codex",
						Components: []openaiWidgetGroupComponent{
							{ComponentID: "codex-api", Name: "Codex API"},
						},
					}},
				},
			},
		},
	}
}

func uptimeStatusOn(t *testing.T, series []openaiUptimeDay, date string) string {
	t.Helper()
	for _, day := range series {
		if day.Date == date {
			return day.Status
		}
	}
	t.Fatalf("missing uptime day %s", date)
	return ""
}
