package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

const (
	openaiStatusURL     = "https://status.openai.com/proxy/status.openai.com"
	openaiStatusPageURL = "https://status.openai.com/"
	openaiStatusTimeout = 8 * time.Second
	openaiStatusTTL     = 60 * time.Second
	openaiStatusFailTTL = 15 * time.Second
	openaiAPIsGroupName = "APIs"
	openaiUptimeDays    = 90
	openaiUptimeHours   = 24
)

type openaiUptimeDay struct {
	Date   string              `json:"date"`
	Status string              `json:"status"`
	Events []openaiUptimeEvent `json:"events,omitempty"`
}

type openaiUptimeHour struct {
	Ts     string              `json:"ts"`
	Status string              `json:"status"`
	Events []openaiUptimeEvent `json:"events,omitempty"`
}

type openaiUptimeEvent struct {
	Name           string   `json:"name"`
	ImpactStatus   string   `json:"impact_status"`
	ComponentNames []string `json:"component_names,omitempty"`
	IncidentStatus string   `json:"incident_status,omitempty"`
	URL            string   `json:"url,omitempty"`
}

type openaiStatusComponent struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Status        string             `json:"status"`
	UptimePercent *float64           `json:"uptime_percent,omitempty"`
	Series        []openaiUptimeDay  `json:"series,omitempty"`
	HourlySeries  []openaiUptimeHour `json:"hourly_series,omitempty"`
}

type openaiStatusGroup struct {
	Name          string                  `json:"name"`
	UptimePercent *float64                `json:"uptime_percent,omitempty"`
	UptimeDays    int                     `json:"uptime_days,omitempty"`
	Series        []openaiUptimeDay       `json:"series,omitempty"`
	HourlyHours   int                     `json:"hourly_hours,omitempty"`
	HourlySeries  []openaiUptimeHour      `json:"hourly_series,omitempty"`
	Components    []openaiStatusComponent `json:"components"`
}

type openaiStatusIncident struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Status             string   `json:"status"`
	Impact             string   `json:"impact"`
	AffectedComponents []string `json:"affected_components"`
	UpdatedAt          string   `json:"updated_at"`
	URL                string   `json:"url"`
}

type openaiStatusResponse struct {
	Available   bool                   `json:"available"`
	Indicator   string                 `json:"indicator"`
	Description string                 `json:"description"`
	UpdatedAt   string                 `json:"updated_at"`
	SourceURL   string                 `json:"source_url"`
	Groups      []openaiStatusGroup    `json:"groups"`
	Incidents   []openaiStatusIncident `json:"incidents"`
	History     []openaiStatusIncident `json:"history"`
}

type openaiWidgetPayload struct {
	Summary openaiWidgetSummary `json:"summary"`
}

type openaiWidgetSummary struct {
	Name               string                  `json:"name"`
	PublicURL          string                  `json:"public_url"`
	AffectedComponents []openaiWidgetComponent `json:"affected_components"`
	OngoingIncidents   []openaiWidgetIncident  `json:"ongoing_incidents"`
	Structure          openaiWidgetStructure   `json:"structure"`
}

type openaiWidgetStructure struct {
	Items []openaiWidgetStructureItem `json:"items"`
}

type openaiWidgetStructureItem struct {
	Group *openaiWidgetGroup `json:"group"`
}

type openaiWidgetGroup struct {
	ID         string                       `json:"id"`
	Name       string                       `json:"name"`
	Components []openaiWidgetGroupComponent `json:"components"`
}

type openaiImpactsPayload struct {
	ComponentImpacts []openaiImpactWindow    `json:"component_impacts"`
	ComponentUptimes []openaiComponentUptime `json:"component_uptimes"`
	IncidentLinks    []openaiIncidentLink    `json:"incident_links"`
}

type openaiImpactWindow struct {
	StartAt              string `json:"start_at"`
	EndAt                string `json:"end_at"`
	ID                   string `json:"id"`
	ComponentID          string `json:"component_id"`
	Status               string `json:"status"`
	StatusPageIncidentID string `json:"status_page_incident_id"`
}

type openaiIncidentLink struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Permalink   string `json:"permalink"`
	PublishedAt string `json:"published_at"`
}

type openaiComponentUptime struct {
	ComponentID string `json:"component_id"`
	GroupID     string `json:"status_page_component_group_id"`
	Uptime      string `json:"uptime"`
}

type openaiWidgetGroupComponent struct {
	ComponentID string `json:"component_id"`
	Name        string `json:"name"`
	Hidden      bool   `json:"hidden"`
}

type openaiWidgetComponent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	ComponentID string `json:"component_id"`
}

type openaiWidgetIncident struct {
	ID                 string                  `json:"id"`
	Name               string                  `json:"name"`
	Status             string                  `json:"status"`
	Impact             string                  `json:"impact"`
	URL                string                  `json:"url"`
	UpdatedAt          string                  `json:"updated_at"`
	AffectedComponents []openaiWidgetComponent `json:"affected_components"`
	ComponentImpacts   []openaiWidgetComponent `json:"component_impacts"`
}

var (
	openaiStatusMu        sync.Mutex
	openaiStatusCached    *openaiStatusResponse
	openaiStatusCachedAt  time.Time
	openaiStatusCachedTTL time.Duration
)

var openaiImpactRank = map[string]int{
	"none":     0,
	"minor":    1,
	"major":    2,
	"critical": 3,
}

var openaiComponentImpactRank = map[string]int{
	"operational":          0,
	"under_maintenance":    1,
	"degraded_performance": 1,
	"partial_outage":       2,
	"major_outage":         3,
	"full_outage":          3,
}

func GetOpenAIStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    loadOpenAIStatus(),
	})
}

func loadOpenAIStatus() openaiStatusResponse {
	now := time.Now()
	openaiStatusMu.Lock()
	if openaiStatusCached != nil && now.Sub(openaiStatusCachedAt) < openaiStatusCachedTTL {
		cached := *openaiStatusCached
		openaiStatusMu.Unlock()
		return cached
	}
	openaiStatusMu.Unlock()

	result := fetchOpenAIStatus()
	ttl := openaiStatusTTL
	if !result.Available {
		ttl = openaiStatusFailTTL
	}

	openaiStatusMu.Lock()
	openaiStatusCached = &result
	openaiStatusCachedAt = time.Now()
	openaiStatusCachedTTL = ttl
	openaiStatusMu.Unlock()
	return result
}

func fetchOpenAIStatus() openaiStatusResponse {
	unavailable := openaiStatusResponse{
		Available:   false,
		Indicator:   "unknown",
		Description: "Unable to load OpenAI status",
		SourceURL:   openaiStatusPageURL,
		Groups:      []openaiStatusGroup{},
		Incidents:   []openaiStatusIncident{},
		History:     []openaiStatusIncident{},
	}

	now := time.Now().UTC()
	client := &http.Client{Timeout: openaiStatusTimeout}
	var widgetBody, impactsBody []byte
	var widgetErr, impactsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		widgetBody, widgetErr = getOpenAIURL(client, openaiStatusURL)
	}()
	go func() {
		defer wg.Done()
		impactsBody, impactsErr = getOpenAIURL(client, openaiImpactsURL(now))
	}()
	wg.Wait()
	if widgetErr != nil {
		return unavailable
	}
	var payload openaiWidgetPayload
	if err := common.Unmarshal(widgetBody, &payload); err != nil {
		return unavailable
	}
	var impacts *openaiImpactsPayload
	if impactsErr == nil {
		var parsed openaiImpactsPayload
		if err := common.Unmarshal(impactsBody, &parsed); err == nil {
			impacts = &parsed
		}
	}
	return normalizeOpenAIStatus(payload, impacts, now)
}

func getOpenAIURL(client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "new-api-openai-status")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai status http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func openaiImpactsURL(now time.Time) string {
	start, _ := openaiUptimeWindow(now)
	query := url.Values{}
	query.Set("start_at", start.Format(time.RFC3339))
	query.Set("end_at", now.UTC().Format(time.RFC3339))
	return openaiStatusURL + "/component_impacts?" + query.Encode()
}

func normalizeOpenAIStatus(payload openaiWidgetPayload, impacts *openaiImpactsPayload, now time.Time) openaiStatusResponse {
	summary := payload.Summary
	sourceURL := summary.PublicURL
	if sourceURL == "" {
		sourceURL = openaiStatusPageURL
	}

	apiComponentIDs := map[string]string{}
	var apiGroupID string
	var apiComponents []openaiWidgetGroupComponent
	for _, item := range summary.Structure.Items {
		if item.Group == nil || item.Group.Name != openaiAPIsGroupName {
			continue
		}
		apiGroupID = item.Group.ID
		for _, component := range item.Group.Components {
			if component.Hidden || component.ComponentID == "" {
				continue
			}
			apiComponentIDs[component.ComponentID] = component.Name
			apiComponents = append(apiComponents, component)
		}
	}

	statusByID := map[string]string{}
	for _, affected := range summary.AffectedComponents {
		id := firstNonEmpty(affected.ID, affected.ComponentID)
		if id == "" || affected.Status == "" {
			continue
		}
		statusByID[id] = affected.Status
	}

	incidents := make([]openaiStatusIncident, 0, len(summary.OngoingIncidents))
	for _, incident := range summary.OngoingIncidents {
		affectedNames := make([]string, 0)
		touchesAPIs := false
		for _, affected := range append(incident.AffectedComponents, incident.ComponentImpacts...) {
			id := firstNonEmpty(affected.ID, affected.ComponentID)
			if id == "" {
				continue
			}
			if affected.Status != "" {
				if current, ok := statusByID[id]; !ok || openaiComponentImpactRank[affected.Status] > openaiComponentImpactRank[current] {
					statusByID[id] = affected.Status
				}
			}
			name, ok := apiComponentIDs[id]
			if !ok {
				continue
			}
			touchesAPIs = true
			affectedNames = append(affectedNames, name)
		}
		if !touchesAPIs {
			continue
		}
		incidents = append(incidents, openaiStatusIncident{
			ID:                 incident.ID,
			Name:               incident.Name,
			Status:             incident.Status,
			Impact:             incident.Impact,
			AffectedComponents: uniqueStrings(affectedNames),
			UpdatedAt:          incident.UpdatedAt,
			URL:                incident.URL,
		})
	}

	components := make([]openaiStatusComponent, 0, len(apiComponents))
	worstComponent := "operational"
	for _, component := range apiComponents {
		status := statusByID[component.ComponentID]
		if status == "" {
			status = "operational"
		}
		if openaiComponentImpactRank[status] > openaiComponentImpactRank[worstComponent] {
			worstComponent = status
		}
		components = append(components, openaiStatusComponent{
			ID:     component.ComponentID,
			Name:   component.Name,
			Status: status,
		})
	}

	indicator := "none"
	for _, incident := range incidents {
		if openaiImpactRank[incident.Impact] > openaiImpactRank[indicator] {
			indicator = incident.Impact
		}
	}
	if indicator == "none" {
		switch worstComponent {
		case "degraded_performance":
			indicator = "minor"
		case "partial_outage":
			indicator = "major"
		case "major_outage", "full_outage":
			indicator = "critical"
		}
	}

	description := "All Systems Operational"
	switch indicator {
	case "minor":
		description = "Partially Degraded Service"
	case "major":
		description = "Partial System Outage"
	case "critical":
		description = "Major System Outage"
	}

	groups := []openaiStatusGroup{}
	if len(components) > 0 {
		group := openaiStatusGroup{
			Name:       openaiAPIsGroupName,
			Components: components,
		}
		attachOpenAIUptime(&group, apiGroupID, apiComponentIDs, impacts, now)
		groups = append(groups, group)
	}

	if now.IsZero() {
		now = time.Now().UTC()
	}

	history := []openaiStatusIncident{}
	if impacts != nil {
		history = buildOpenAIHistory(apiComponentIDs, impacts.ComponentImpacts, impacts.IncidentLinks, now)
	}

	return openaiStatusResponse{
		Available:   true,
		Indicator:   indicator,
		Description: description,
		UpdatedAt:   now.UTC().Format(time.RFC3339),
		SourceURL:   sourceURL,
		Groups:      groups,
		Incidents:   incidents,
		History:     history,
	}
}

func attachOpenAIUptime(
	group *openaiStatusGroup,
	groupID string,
	apiComponentIDs map[string]string,
	impacts *openaiImpactsPayload,
	now time.Time,
) {
	if group == nil || impacts == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	ids := make(map[string]struct{}, len(apiComponentIDs))
	for id := range apiComponentIDs {
		ids[id] = struct{}{}
	}
	group.UptimeDays = openaiUptimeDays
	group.HourlyHours = openaiUptimeHours
	group.Series = buildOpenAIUptimeSeries(ids, apiComponentIDs, impacts.ComponentImpacts, impacts.IncidentLinks, now)
	group.HourlySeries = buildOpenAIHourlySeries(ids, apiComponentIDs, impacts.ComponentImpacts, impacts.IncidentLinks, now)
	group.UptimePercent = openaiUptimeByGroupID(impacts.ComponentUptimes, groupID)

	uptimeByComponent := map[string]*float64{}
	for _, item := range impacts.ComponentUptimes {
		if item.ComponentID == "" {
			continue
		}
		uptimeByComponent[item.ComponentID] = parseOpenAIUptimePercent(item.Uptime)
	}
	for i := range group.Components {
		componentID := group.Components[i].ID
		componentIDs := map[string]struct{}{componentID: {}}
		componentNames := map[string]string{componentID: group.Components[i].Name}
		group.Components[i].UptimePercent = uptimeByComponent[componentID]
		group.Components[i].Series = buildOpenAIUptimeSeries(componentIDs, componentNames, impacts.ComponentImpacts, impacts.IncidentLinks, now)
		group.Components[i].HourlySeries = buildOpenAIHourlySeries(componentIDs, componentNames, impacts.ComponentImpacts, impacts.IncidentLinks, now)
	}
}

func openaiUptimeByGroupID(uptimes []openaiComponentUptime, groupID string) *float64 {
	if groupID == "" {
		return nil
	}
	for _, item := range uptimes {
		if item.GroupID == groupID {
			return parseOpenAIUptimePercent(item.Uptime)
		}
	}
	return nil
}

func parseOpenAIUptimePercent(value string) *float64 {
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func openaiUptimeWindow(now time.Time) (start, endExclusive time.Time) {
	endDay := utcDay(now)
	start = endDay.AddDate(0, 0, 1-openaiUptimeDays)
	return start, endDay.AddDate(0, 0, 1)
}

func openaiHourlyWindow(now time.Time) (start, endExclusive time.Time) {
	endExclusive = utcHour(now).Add(time.Hour)
	start = endExclusive.Add(-time.Duration(openaiUptimeHours) * time.Hour)
	return start, endExclusive
}

func utcDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func utcHour(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), 0, 0, 0, time.UTC)
}

func parseOpenAITime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func worseOpenAIDayStatus(current, incoming string) string {
	if current == "" {
		current = "operational"
	}
	incomingRank, ok := openaiComponentImpactRank[incoming]
	if !ok {
		incomingRank = 1
	}
	if incomingRank > openaiComponentImpactRank[current] {
		return incoming
	}
	return current
}

func buildOpenAIUptimeBuckets(
	componentIDs map[string]struct{},
	componentNames map[string]string,
	impacts []openaiImpactWindow,
	incidentLinks []openaiIncidentLink,
	now time.Time,
	start time.Time,
	n int,
	step time.Duration,
) []openaiUptimeBucket {
	endExclusive := start.Add(time.Duration(n) * step)
	linkByID := openaiIncidentLinkMap(incidentLinks)
	buckets := make([]openaiUptimeBucket, n)
	for i := range buckets {
		buckets[i] = openaiUptimeBucket{
			status: "operational",
			events: map[string]*openaiUptimeEvent{},
		}
	}
	for _, impact := range impacts {
		if _, ok := componentIDs[impact.ComponentID]; !ok {
			continue
		}
		startAt, ok := parseOpenAITime(impact.StartAt)
		if !ok {
			continue
		}
		endAt := now.UTC()
		if parsedEnd, ok := parseOpenAITime(impact.EndAt); ok {
			endAt = parsedEnd
		}
		if !endAt.After(startAt) {
			endAt = startAt.Add(time.Second)
		}
		if !endAt.After(start) || !startAt.Before(endExclusive) {
			continue
		}
		componentName := componentNames[impact.ComponentID]
		if componentName == "" {
			componentName = impact.ComponentID
		}
		eventKey := impact.StatusPageIncidentID
		if eventKey == "" {
			eventKey = impact.ID
		}
		link, hasLink := linkByID[impact.StatusPageIncidentID]
		eventName := componentName
		incidentStatus := ""
		url := ""
		if hasLink {
			eventName = link.Name
			incidentStatus = link.Status
			url = link.Permalink
		}
		for i := 0; i < n; i++ {
			bucketStart := start.Add(time.Duration(i) * step)
			bucketEnd := bucketStart.Add(step)
			if !startAt.Before(bucketEnd) || !endAt.After(bucketStart) {
				continue
			}
			buckets[i].status = worseOpenAIDayStatus(buckets[i].status, impact.Status)
			event, ok := buckets[i].events[eventKey]
			if !ok {
				event = &openaiUptimeEvent{
					Name:           eventName,
					ImpactStatus:   impact.Status,
					ComponentNames: []string{},
					IncidentStatus: incidentStatus,
					URL:            url,
				}
				buckets[i].events[eventKey] = event
			}
			if openaiComponentImpactRank[impact.Status] > openaiComponentImpactRank[event.ImpactStatus] {
				event.ImpactStatus = impact.Status
			}
			event.ComponentNames = uniqueStrings(append(event.ComponentNames, componentName))
		}
	}
	return buckets
}

type openaiUptimeBucket struct {
	status string
	events map[string]*openaiUptimeEvent
}

func openaiIncidentLinkMap(links []openaiIncidentLink) map[string]openaiIncidentLink {
	out := make(map[string]openaiIncidentLink, len(links))
	for _, link := range links {
		if link.ID == "" {
			continue
		}
		out[link.ID] = link
	}
	return out
}

func openaiUptimeEventsFromBucket(bucket openaiUptimeBucket) []openaiUptimeEvent {
	if len(bucket.events) == 0 {
		return nil
	}
	out := make([]openaiUptimeEvent, 0, len(bucket.events))
	for _, event := range bucket.events {
		if event == nil {
			continue
		}
		out = append(out, *event)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ImpactStatus < out[j].ImpactStatus
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func buildOpenAIUptimeSeries(
	componentIDs map[string]struct{},
	componentNames map[string]string,
	impacts []openaiImpactWindow,
	incidentLinks []openaiIncidentLink,
	now time.Time,
) []openaiUptimeDay {
	start, _ := openaiUptimeWindow(now)
	buckets := buildOpenAIUptimeBuckets(
		componentIDs,
		componentNames,
		impacts,
		incidentLinks,
		now,
		start,
		openaiUptimeDays,
		24*time.Hour,
	)
	out := make([]openaiUptimeDay, len(buckets))
	for i, bucket := range buckets {
		out[i] = openaiUptimeDay{
			Date:   start.AddDate(0, 0, i).Format("2006-01-02"),
			Status: bucket.status,
			Events: openaiUptimeEventsFromBucket(bucket),
		}
	}
	return out
}

func buildOpenAIHourlySeries(
	componentIDs map[string]struct{},
	componentNames map[string]string,
	impacts []openaiImpactWindow,
	incidentLinks []openaiIncidentLink,
	now time.Time,
) []openaiUptimeHour {
	start, _ := openaiHourlyWindow(now)
	buckets := buildOpenAIUptimeBuckets(
		componentIDs,
		componentNames,
		impacts,
		incidentLinks,
		now,
		start,
		openaiUptimeHours,
		time.Hour,
	)
	out := make([]openaiUptimeHour, len(buckets))
	for i, bucket := range buckets {
		out[i] = openaiUptimeHour{
			Ts:     start.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
			Status: bucket.status,
			Events: openaiUptimeEventsFromBucket(bucket),
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func buildOpenAIHistory(
	apiComponentIDs map[string]string,
	impacts []openaiImpactWindow,
	incidentLinks []openaiIncidentLink,
	now time.Time,
) []openaiStatusIncident {
	if len(apiComponentIDs) == 0 || len(impacts) == 0 {
		return []openaiStatusIncident{}
	}
	start, endExclusive := openaiUptimeWindow(now)
	linkByID := openaiIncidentLinkMap(incidentLinks)
	byKey := map[string]*openaiStatusIncident{}
	for _, impact := range impacts {
		componentName, ok := apiComponentIDs[impact.ComponentID]
		if !ok {
			continue
		}
		startAt, ok := parseOpenAITime(impact.StartAt)
		if !ok {
			continue
		}
		endAt := now.UTC()
		if parsedEnd, ok := parseOpenAITime(impact.EndAt); ok {
			endAt = parsedEnd
		}
		if !endAt.After(startAt) {
			endAt = startAt.Add(time.Second)
		}
		if !endAt.After(start) || !startAt.Before(endExclusive) {
			continue
		}
		eventKey := impact.StatusPageIncidentID
		if eventKey == "" {
			eventKey = impact.ID
		}
		if eventKey == "" {
			continue
		}
		item, exists := byKey[eventKey]
		if !exists {
			link, hasLink := linkByID[impact.StatusPageIncidentID]
			name := componentName
			status := ""
			url := ""
			updatedAt := impact.StartAt
			if hasLink {
				name = link.Name
				status = link.Status
				url = link.Permalink
				if link.PublishedAt != "" {
					updatedAt = link.PublishedAt
				}
			}
			item = &openaiStatusIncident{
				ID:                 eventKey,
				Name:               name,
				Status:             status,
				Impact:             impact.Status,
				AffectedComponents: []string{},
				UpdatedAt:          updatedAt,
				URL:                url,
			}
			byKey[eventKey] = item
		}
		if openaiComponentImpactRank[impact.Status] > openaiComponentImpactRank[item.Impact] {
			item.Impact = impact.Status
		}
		item.AffectedComponents = uniqueStrings(append(item.AffectedComponents, componentName))
	}

	out := make([]openaiStatusIncident, 0, len(byKey))
	for _, item := range byKey {
		if item == nil {
			continue
		}
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, oki := parseOpenAITime(out[i].UpdatedAt)
		tj, okj := parseOpenAITime(out[j].UpdatedAt)
		if oki && okj && !ti.Equal(tj) {
			return ti.After(tj)
		}
		if oki != okj {
			return oki
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}
