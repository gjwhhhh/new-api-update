package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIStatusOperational(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	result := normalizeOpenAIStatus(openaiWidgetPayload{
		Summary: openaiWidgetSummary{
			PublicURL: openaiStatusPageURL,
			Structure: openaiWidgetStructure{
				Items: []openaiWidgetStructureItem{
					{Group: &openaiWidgetGroup{
						ID:   "apis-group",
						Name: "APIs",
						Components: []openaiWidgetGroupComponent{
							{ComponentID: "chat", Name: "Chat Completions"},
							{ComponentID: "responses", Name: "Responses"},
						},
					}},
					{Group: &openaiWidgetGroup{
						Name: "ChatGPT",
						Components: []openaiWidgetGroupComponent{
							{ComponentID: "login", Name: "Login"},
						},
					}},
				},
			},
			OngoingIncidents: []openaiWidgetIncident{
				{
					ID:     "chatgpt-only",
					Name:   "ChatGPT login issues",
					Impact: "major",
					AffectedComponents: []openaiWidgetComponent{
						{ID: "login", Status: "partial_outage"},
					},
				},
			},
		},
	}, nil, now)

	require.True(t, result.Available)
	assert.Equal(t, "none", result.Indicator)
	require.Len(t, result.Groups, 1)
	assert.Equal(t, "APIs", result.Groups[0].Name)
	require.Len(t, result.Groups[0].Components, 2)
	assert.Equal(t, "operational", result.Groups[0].Components[0].Status)
	assert.Empty(t, result.Incidents)
	assert.Empty(t, result.History)
	assert.Empty(t, result.Groups[0].Series)
}

func TestNormalizeOpenAIStatusDegradedResponses(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	result := normalizeOpenAIStatus(openaiWidgetPayload{
		Summary: openaiWidgetSummary{
			AffectedComponents: []openaiWidgetComponent{
				{ID: "responses", Status: "degraded_performance"},
			},
			Structure: openaiWidgetStructure{
				Items: []openaiWidgetStructureItem{
					{Group: &openaiWidgetGroup{
						Name: "APIs",
						Components: []openaiWidgetGroupComponent{
							{ComponentID: "chat", Name: "Chat Completions"},
							{ComponentID: "responses", Name: "Responses"},
						},
					}},
				},
			},
			OngoingIncidents: []openaiWidgetIncident{
				{
					ID:        "inc-1",
					Name:      "Increased error rates for Responses API",
					Status:    "investigating",
					Impact:    "minor",
					UpdatedAt: "2026-09-02T08:00:00Z",
					URL:       "https://status.openai.com/incidents/inc-1",
					AffectedComponents: []openaiWidgetComponent{
						{ID: "responses", Status: "degraded_performance"},
					},
				},
			},
		},
	}, nil, now)

	require.True(t, result.Available)
	assert.Equal(t, "minor", result.Indicator)
	require.Len(t, result.Incidents, 1)
	assert.Equal(t, "Increased error rates for Responses API", result.Incidents[0].Name)
	assert.Equal(t, []string{"Responses"}, result.Incidents[0].AffectedComponents)
	require.Len(t, result.Groups[0].Components, 2)
	assert.Equal(t, "operational", result.Groups[0].Components[0].Status)
	assert.Equal(t, "degraded_performance", result.Groups[0].Components[1].Status)
}

func TestNormalizeOpenAIStatusOngoingAlsoInHistory(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	payload := openaiAPIsTestPayload()
	payload.Summary.OngoingIncidents = []openaiWidgetIncident{{
		ID:     "inc-1",
		Name:   "Increased error rates for Responses API",
		Status: "investigating",
		Impact: "minor",
		URL:    "https://status.openai.com/incidents/inc-1",
		AffectedComponents: []openaiWidgetComponent{
			{ID: "chat", Status: "degraded_performance"},
		},
	}}
	result := normalizeOpenAIStatus(payload, &openaiImpactsPayload{
		ComponentImpacts: []openaiImpactWindow{{
			ID:                   "impact-1",
			ComponentID:          "chat",
			Status:               "degraded_performance",
			StartAt:              "2026-09-02T08:00:00Z",
			StatusPageIncidentID: "inc-1",
		}},
		IncidentLinks: []openaiIncidentLink{{
			ID:          "inc-1",
			Name:        "Increased error rates for Responses API",
			Status:      "investigating",
			Permalink:   "https://status.openai.com/incidents/inc-1",
			PublishedAt: "2026-09-02T08:00:00Z",
		}},
	}, now)

	require.Len(t, result.Incidents, 1)
	require.Len(t, result.History, 1)
	assert.Equal(t, "inc-1", result.Incidents[0].ID)
	assert.Equal(t, "inc-1", result.History[0].ID)
	assert.Equal(t, "investigating", result.History[0].Status)
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

func TestNormalizeOpenAIStatusUptimeBars(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	apis := openaiAPIsTestPayload()
	result := normalizeOpenAIStatus(apis, &openaiImpactsPayload{
		ComponentUptimes: []openaiComponentUptime{
			{GroupID: "apis-group", Uptime: "99.94"},
			{ComponentID: "chat", Uptime: "100.00"},
			{ComponentID: "images", Uptime: "99.96"},
			{GroupID: "chatgpt-group", Uptime: "99.62"},
		},
		ComponentImpacts: []openaiImpactWindow{
			{
				ID:          "impact-images-degraded",
				ComponentID: "images",
				Status:      "degraded_performance",
				StartAt:     "2026-08-20T10:00:00.000Z",
				EndAt:       "2026-08-20T12:00:00.000Z",
			},
			{
				ID:          "impact-images-outage",
				ComponentID: "images",
				Status:      "full_outage",
				StartAt:     "2026-08-20T11:00:00.000Z",
				EndAt:       "2026-08-20T11:30:00.000Z",
			},
			{
				ID:          "impact-chat-partial",
				ComponentID: "chat",
				Status:      "partial_outage",
				StartAt:     "2026-08-01T08:00:00Z",
				EndAt:       "2026-08-01T09:00:00Z",
			},
			{
				ID:          "impact-chatgpt",
				ComponentID: "chatgpt-login",
				Status:      "full_outage",
				StartAt:     "2026-08-15T00:00:00Z",
				EndAt:       "2026-08-15T23:00:00Z",
			},
		},
	}, now)

	require.True(t, result.Available)
	require.Len(t, result.Groups, 1)
	group := result.Groups[0]
	require.NotNil(t, group.UptimePercent)
	assert.InDelta(t, 99.94, *group.UptimePercent, 0.0001)
	assert.Equal(t, 90, group.UptimeDays)
	assert.Equal(t, 24, group.HourlyHours)
	require.Len(t, group.Series, 90)
	require.Len(t, group.HourlySeries, 24)
	assert.Equal(t, "2026-09-01T13:00:00Z", group.HourlySeries[0].Ts)
	assert.Equal(t, "2026-09-02T12:00:00Z", group.HourlySeries[23].Ts)
	for _, hour := range group.HourlySeries {
		assert.Equal(t, "operational", hour.Status)
	}
	assert.Equal(t, "2026-06-05", group.Series[0].Date)
	assert.Equal(t, "2026-09-02", group.Series[89].Date)
	assert.Equal(t, "operational", group.Series[0].Status)
	assert.Equal(t, "full_outage", uptimeStatusOn(t, group.Series, "2026-08-20"))
	assert.Equal(t, "partial_outage", uptimeStatusOn(t, group.Series, "2026-08-01"))
	assert.Equal(t, "operational", uptimeStatusOn(t, group.Series, "2026-08-15"))

	require.Len(t, group.Components, 2)
	assert.Equal(t, "Chat Completions", group.Components[0].Name)
	require.NotNil(t, group.Components[0].UptimePercent)
	assert.InDelta(t, 100, *group.Components[0].UptimePercent, 0.0001)
	assert.Equal(t, "partial_outage", uptimeStatusOn(t, group.Components[0].Series, "2026-08-01"))
	assert.Equal(t, "operational", uptimeStatusOn(t, group.Components[0].Series, "2026-08-20"))
	require.NotNil(t, group.Components[1].UptimePercent)
	assert.InDelta(t, 99.96, *group.Components[1].UptimePercent, 0.0001)
	assert.Equal(t, "full_outage", uptimeStatusOn(t, group.Components[1].Series, "2026-08-20"))
}

func TestNormalizeOpenAIStatusHistory(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	result := normalizeOpenAIStatus(openaiAPIsTestPayload(), &openaiImpactsPayload{
		ComponentImpacts: []openaiImpactWindow{
			{
				ID:                   "impact-chat",
				ComponentID:          "chat",
				Status:               "partial_outage",
				StartAt:              "2026-08-01T08:00:00Z",
				EndAt:                "2026-08-01T09:00:00Z",
				StatusPageIncidentID: "inc-chat",
			},
			{
				ID:                   "impact-images",
				ComponentID:          "images",
				Status:               "degraded_performance",
				StartAt:              "2026-08-20T10:00:00Z",
				EndAt:                "2026-08-20T12:00:00Z",
				StatusPageIncidentID: "inc-images",
			},
			{
				ID:                   "impact-images-outage",
				ComponentID:          "images",
				Status:               "full_outage",
				StartAt:              "2026-08-20T11:00:00Z",
				EndAt:                "2026-08-20T11:30:00Z",
				StatusPageIncidentID: "inc-images",
			},
			{
				ID:          "impact-unlinked",
				ComponentID: "chat",
				Status:      "degraded_performance",
				StartAt:     "2026-07-10T04:00:00Z",
				EndAt:       "2026-07-10T05:00:00Z",
			},
			{
				ID:                   "impact-chatgpt",
				ComponentID:          "chatgpt-login",
				Status:               "full_outage",
				StartAt:              "2026-08-15T00:00:00Z",
				EndAt:                "2026-08-15T23:00:00Z",
				StatusPageIncidentID: "inc-chatgpt",
			},
		},
		IncidentLinks: []openaiIncidentLink{
			{
				ID:          "inc-chat",
				Name:        "Partial outage in Chat Completions",
				Status:      "resolved",
				Permalink:   "https://status.openai.com/incidents/inc-chat",
				PublishedAt: "2026-08-01T08:10:00Z",
			},
			{
				ID:          "inc-images",
				Name:        "Images API disruption",
				Status:      "resolved",
				Permalink:   "https://status.openai.com/incidents/inc-images",
				PublishedAt: "2026-08-20T10:05:00Z",
			},
			{
				ID:          "inc-chatgpt",
				Name:        "ChatGPT login issues",
				Status:      "resolved",
				Permalink:   "https://status.openai.com/incidents/inc-chatgpt",
				PublishedAt: "2026-08-15T00:10:00Z",
			},
		},
	}, now)

	require.Len(t, result.History, 3)
	assert.Equal(t, "inc-images", result.History[0].ID)
	assert.Equal(t, "Images API disruption", result.History[0].Name)
	assert.Equal(t, "resolved", result.History[0].Status)
	assert.Equal(t, "full_outage", result.History[0].Impact)
	assert.Equal(t, []string{"Images"}, result.History[0].AffectedComponents)
	assert.Equal(t, "2026-08-20T10:05:00Z", result.History[0].UpdatedAt)
	assert.Equal(t, "https://status.openai.com/incidents/inc-images", result.History[0].URL)

	assert.Equal(t, "inc-chat", result.History[1].ID)
	assert.Equal(t, "impact-unlinked", result.History[2].ID)
	assert.Equal(t, "Chat Completions", result.History[2].Name)
	assert.Equal(t, "degraded_performance", result.History[2].Impact)
	assert.Empty(t, result.History[2].URL)
	for _, item := range result.History {
		assert.NotEqual(t, "inc-chatgpt", item.ID)
		assert.NotContains(t, item.AffectedComponents, "Login")
	}
}

func TestBuildOpenAIUptimeSeriesOngoingImpact(t *testing.T) {
	now := time.Date(2026, 9, 2, 18, 0, 0, 0, time.UTC)
	series := buildOpenAIUptimeSeries(
		map[string]struct{}{"chat": {}},
		map[string]string{"chat": "Chat Completions"},
		[]openaiImpactWindow{{
			ComponentID: "chat",
			Status:      "degraded_performance",
			StartAt:     "2026-09-02T10:00:00Z",
		}},
		nil,
		now,
	)
	assert.Equal(t, "degraded_performance", uptimeStatusOn(t, series, "2026-09-02"))
	assert.Equal(t, "operational", uptimeStatusOn(t, series, "2026-09-01"))
}

func TestBuildOpenAIHourlySeries(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 30, 0, 0, time.UTC)
	series := buildOpenAIHourlySeries(
		map[string]struct{}{"images": {}, "chat": {}},
		map[string]string{
			"images": "Images",
			"chat":   "Chat Completions",
		},
		[]openaiImpactWindow{
			{
				ComponentID: "images",
				Status:      "degraded_performance",
				StartAt:     "2026-09-02T10:00:00Z",
				EndAt:       "2026-09-02T11:30:00Z",
			},
			{
				ComponentID: "images",
				Status:      "full_outage",
				StartAt:     "2026-09-02T11:00:00Z",
				EndAt:       "2026-09-02T11:20:00Z",
			},
			{
				ComponentID: "chatgpt-login",
				Status:      "full_outage",
				StartAt:     "2026-09-02T09:00:00Z",
				EndAt:       "2026-09-02T12:00:00Z",
			},
		},
		nil,
		now,
	)
	require.Len(t, series, 24)
	assert.Equal(t, "2026-09-01T13:00:00Z", series[0].Ts)
	assert.Equal(t, "2026-09-02T12:00:00Z", series[23].Ts)
	assert.Equal(t, "operational", hourlyStatusOn(t, series, "2026-09-02T09:00:00Z"))
	assert.Equal(t, "degraded_performance", hourlyStatusOn(t, series, "2026-09-02T10:00:00Z"))
	assert.Equal(t, "full_outage", hourlyStatusOn(t, series, "2026-09-02T11:00:00Z"))
	assert.Equal(t, "operational", hourlyStatusOn(t, series, "2026-09-02T12:00:00Z"))
}

func TestBuildOpenAIHourlySeriesWithIncidentDetails(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 30, 0, 0, time.UTC)
	series := buildOpenAIHourlySeries(
		map[string]struct{}{"responses": {}},
		map[string]string{"responses": "Responses"},
		[]openaiImpactWindow{{
			ID:                   "impact-1",
			ComponentID:          "responses",
			Status:               "degraded_performance",
			StartAt:              "2026-09-02T10:00:00Z",
			EndAt:                "2026-09-02T11:30:00Z",
			StatusPageIncidentID: "inc-1",
		}},
		[]openaiIncidentLink{{
			ID:        "inc-1",
			Name:      "Elevated latency in the Responses API",
			Status:    "resolved",
			Permalink: "https://status.openai.com/incidents/inc-1",
		}},
		now,
	)
	hour := series[21]
	require.Equal(t, "2026-09-02T10:00:00Z", hour.Ts)
	require.Equal(t, "degraded_performance", hour.Status)
	require.Len(t, hour.Events, 1)
	assert.Equal(t, "Elevated latency in the Responses API", hour.Events[0].Name)
	assert.Equal(t, []string{"Responses"}, hour.Events[0].ComponentNames)
	assert.Equal(t, "resolved", hour.Events[0].IncidentStatus)
}

func openaiAPIsTestPayload() openaiWidgetPayload {
	return openaiWidgetPayload{
		Summary: openaiWidgetSummary{
			Structure: openaiWidgetStructure{
				Items: []openaiWidgetStructureItem{
					{Group: &openaiWidgetGroup{
						ID:   "apis-group",
						Name: "APIs",
						Components: []openaiWidgetGroupComponent{
							{ComponentID: "chat", Name: "Chat Completions"},
							{ComponentID: "images", Name: "Images"},
						},
					}},
					{Group: &openaiWidgetGroup{
						ID:   "chatgpt-group",
						Name: "ChatGPT",
						Components: []openaiWidgetGroupComponent{
							{ComponentID: "chatgpt-login", Name: "Login"},
						},
					}},
				},
			},
		},
	}
}

func hourlyStatusOn(t *testing.T, series []openaiUptimeHour, ts string) string {
	t.Helper()
	for _, hour := range series {
		if hour.Ts == ts {
			return hour.Status
		}
	}
	t.Fatalf("missing uptime hour %s", ts)
	return ""
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
