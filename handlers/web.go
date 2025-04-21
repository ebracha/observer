package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ebracha/airflow-observer/models"
	"github.com/ebracha/airflow-observer/services"
	"github.com/ebracha/airflow-observer/store"
)

// Common types
type Alert struct {
	DagID     string
	Timestamp time.Time
	Severity  string
}

type DagStats struct {
	ID           string
	Name         string
	SuccessCount int
	FailureCount int
	AvgDuration  float64
	LastRunTime  time.Time
	Status       string
}

type TrendDataPoint struct {
	Date  string `json:"Date"`
	Count int    `json:"Count"`
}

type TrendData struct {
	Days    []TrendDataPoint
	Hours   []TrendDataPoint
	Minutes []TrendDataPoint
}

type DashboardData struct {
	SeverityCount  map[string]int
	Alerts         []Alert
	LastUpdate     time.Time
	ComplianceRate float64
	TotalDAGs      int
	HealthyDAGs    int
	WarningDAGs    int
	CriticalDAGs   int
	Violations     []models.Violation
	TopDagsJSON    string
	TrendJSON      string
}

type WebHandler struct {
	store *store.Store
}

func NewWebHandler(store *store.Store) *WebHandler {
	return &WebHandler{
		store: store,
	}
}

// Helper functions
func (h *WebHandler) renderTemplate(w http.ResponseWriter, templateName string, data interface{}) error {
	funcMap := template.FuncMap{
		"unmarshal": func(s string) interface{} {
			var result interface{}
			json.Unmarshal([]byte(s), &result)
			return result
		},
	}

	templatePath := fmt.Sprintf("templates/%s.html", templateName)
	tmpl, err := template.New(templateName).Funcs(funcMap).ParseFiles(templatePath)
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %v", templatePath, err)
	}

	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("failed to execute template %s: %v", templateName, err)
	}
	return nil
}

func (h *WebHandler) handleError(w http.ResponseWriter, err error, message string) {
	log.Printf("%s: %v", message, err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func (h *WebHandler) convertRulesToSLARules(rules []*store.Rule) []models.SLARule {
	var slaRules []models.SLARule
	for _, rule := range rules {
		slaRule := models.SLARule{
			ID:           len(slaRules) + 1,
			DagID:        rule.Tags["dag_id"],
			FieldName:    rule.Tags["field_name"],
			Condition:    rule.Tags["condition"],
			Value:        fmt.Sprintf("%.2f", rule.Threshold),
			Severity:     rule.Tags["severity"],
			CreatedAt:    rule.CreatedAt,
			LastViolated: nil,
		}
		if windowMins, err := strconv.Atoi(rule.Tags["window_mins"]); err == nil {
			slaRule.WindowMins = windowMins
		}
		if countThresh, err := strconv.Atoi(rule.Tags["count_thresh"]); err == nil {
			slaRule.CountThresh = countThresh
		}
		slaRules = append(slaRules, slaRule)
	}
	return slaRules
}

// Handler functions
func (h *WebHandler) IndexHandler(w http.ResponseWriter, r *http.Request) {
	// Define template functions
	funcMap := template.FuncMap{
		"unmarshal": func(s string) interface{} {
			var result interface{}
			json.Unmarshal([]byte(s), &result)
			return result
		},
		"safeJS": func(s string) template.JS {
			return template.JS(s)
		},
	}

	// Create and parse the template
	tmpl := template.New("index.html").Funcs(funcMap)
	tmpl, err := tmpl.ParseFiles("templates/index.html")
	if err != nil {
		log.Printf("Template parsing error in index.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Execute the template
	if err := tmpl.Execute(w, nil); err != nil {
		log.Printf("Template execution error in indexHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (h *WebHandler) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Get metrics from the last 24 hours
	filter := store.MetricFilter{
		StartTime: time.Now().Add(-24 * time.Hour),
		EndTime:   time.Now(),
	}

	metrics, err := h.store.Metrics().List(ctx, filter)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting metrics: %v", err), http.StatusInternalServerError)
		return
	}

	// Get rules
	rules, err := h.store.Rules().ListRules(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting rules: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert rules to SLARules
	slaRules := make([]models.SLARule, len(rules))
	for i, rule := range rules {
		slaRules[i] = models.SLARule{
			ID:           i + 1,
			DagID:        rule.Tags["dag_id"],
			FieldName:    rule.Tags["field_name"],
			Condition:    rule.Tags["condition"],
			Value:        fmt.Sprintf("%.2f", rule.Threshold),
			WindowMins:   getIntFromTags(rule.Tags, "window_mins"),
			CountThresh:  getIntFromTags(rule.Tags, "count_thresh"),
			CreatedAt:    rule.CreatedAt,
			LastViolated: nil,
			Severity:     rule.Tags["severity"],
		}
	}

	// Check for violations
	violations := services.CheckViolations(metrics, slaRules)
	severityCount := make(map[string]int)
	dagStatus := make(map[string]string) // Maps DAG ID to its worst severity status ("Healthy", "Warning", "Critical")
	for _, v := range violations {
		severityCount[v.Severity]++
		// Determine the worst status for each DAG based on violations
		currentSeverity := dagStatus[v.DagID]
		if currentSeverity == "" || currentSeverity == "Healthy" {
			dagStatus[v.DagID] = v.Severity // Initial or upgrade from Healthy
		} else if currentSeverity == "Warning" && v.Severity == "Critical" {
			dagStatus[v.DagID] = v.Severity // Upgrade from Warning to Critical
		}
	}

	// Get latest metric time
	latestTime, err := h.store.Metrics().GetLatestMetricTime(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting latest metric time: %v", err), http.StatusInternalServerError)
		return
	}

	// Gather alerts
	alerts := make([]Alert, 0, len(violations)) // Pre-allocate slice
	for _, v := range violations {
		alerts = append(alerts, Alert{
			DagID:     v.DagID,
			Timestamp: v.Timestamp,
			Severity:  v.Severity,
		})
	}
	// Optional: Sort alerts if needed, e.g., by timestamp descending
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].Timestamp.After(alerts[j].Timestamp)
	})

	// Calculate DAG statistics
	dagStats := make(map[string]*DagStats)
	trendData := TrendData{ // Keep trend data calculation as is (counts events over time)
		Days:    make([]TrendDataPoint, 0),
		Hours:   make([]TrendDataPoint, 0),
		Minutes: make([]TrendDataPoint, 0),
	}

	// Process metrics to aggregate stats per unique DAG and calculate trend
	for _, m := range metrics {
		// Initialize DagStats if seeing this DAG for the first time
		if _, exists := dagStats[m.DagID]; !exists {
			dagStats[m.DagID] = &DagStats{
				ID:   m.DagID,
				Name: m.DagID, // Consider fetching a friendlier name if available
			}
		}
		stats := dagStats[m.DagID]

		// Process only DAG-level events for Success/Failure counts and Duration/LastRunTime
		if m.TaskID == nil { // This is a DAG-level metric
			if m.EventType == "dag_success" {
				stats.SuccessCount++ // Count successes for this DAG
			} else if m.EventType == "dag_failed" {
				stats.FailureCount++ // Count failures for this DAG
			}

			// Update average duration incrementally
			currentTotalRuns := stats.SuccessCount + stats.FailureCount
			if m.Duration != nil && currentTotalRuns > 0 {
				if currentTotalRuns == 1 {
					stats.AvgDuration = *m.Duration // First run
				} else {
					// Weighted average: (oldAvg * (n-1) + newDuration) / n
					stats.AvgDuration = (stats.AvgDuration*float64(currentTotalRuns-1) + *m.Duration) / float64(currentTotalRuns)
				}
			}

			// Update LastRunTime if this event is later
			if m.ExecutionTime != "" {
				if execTime, err := time.Parse(time.RFC3339, m.ExecutionTime); err == nil {
					if execTime.After(stats.LastRunTime) { // Handles zero time case correctly
						stats.LastRunTime = execTime
					}

					// Add to days
					day := execTime.Format("2006-01-02")
					found := false
					for i := range trendData.Days {
						if trendData.Days[i].Date == day {
							trendData.Days[i].Count++
							found = true
							break
						}
					}
					if !found {
						trendData.Days = append(trendData.Days, TrendDataPoint{Date: day, Count: 1})
					}

					// Add to hours
					hour := execTime.Format("2006-01-02 15:00")
					found = false
					for i := range trendData.Hours {
						if trendData.Hours[i].Date == hour {
							trendData.Hours[i].Count++
							found = true
							break
						}
					}
					if !found {
						trendData.Hours = append(trendData.Hours, TrendDataPoint{Date: hour, Count: 1})
					}

					// Add to minutes
					minute := execTime.Format("2006-01-02 15:04")
					found = false
					for i := range trendData.Minutes {
						if trendData.Minutes[i].Date == minute {
							trendData.Minutes[i].Count++
							found = true
							break
						}
					}
					if !found {
						trendData.Minutes = append(trendData.Minutes, TrendDataPoint{Date: minute, Count: 1})
					}
				}
			}
		}
	}

	// Count DAGs by status based on the final aggregated stats and violations
	totalDAGs := len(dagStats)
	healthyDAGs := 0
	warningDAGs := 0
	criticalDAGs := 0

	for dagID, stats := range dagStats {
		status, exists := dagStatus[dagID]
		if !exists || status == "" { // No violations for this DAG
			stats.Status = "Healthy"
			healthyDAGs++
		} else {
			stats.Status = status // Status determined by violation severity
			if status == "Warning" {
				warningDAGs++
			} else if status == "Critical" {
				criticalDAGs++
			}
		}
	}

	// Sort each time period's data
	sort.Slice(trendData.Days, func(i, j int) bool {
		return trendData.Days[i].Date < trendData.Days[j].Date
	})
	sort.Slice(trendData.Hours, func(i, j int) bool {
		return trendData.Hours[i].Date < trendData.Hours[j].Date
	})
	sort.Slice(trendData.Minutes, func(i, j int) bool {
		return trendData.Minutes[i].Date < trendData.Minutes[j].Date
	})

	trendJSON, err := json.Marshal(trendData)
	if err != nil {
		log.Printf("Error marshaling trend data: %v", err)
		trendJSON = []byte("{}")
	}

	// Calculate compliance rate
	complianceRate := 0.0
	if totalDAGs > 0 {
		complianceRate = float64(healthyDAGs) / float64(totalDAGs) * 100
	}

	// Convert dagStats to slice and sort by last run time
	var topDags []DagStats
	for _, stats := range dagStats {
		topDags = append(topDags, *stats)
	}
	sort.Slice(topDags, func(i, j int) bool {
		return topDags[i].LastRunTime.After(topDags[j].LastRunTime)
	})

	// Take top 5 DAGs
	if len(topDags) > 5 {
		topDags = topDags[:5]
	}

	// Convert to JSON
	topDagsJSON, err := json.Marshal(topDags)
	if err != nil {
		log.Printf("Error marshaling top DAGs: %v", err)
		topDagsJSON = []byte("[]")
	}

	data := DashboardData{
		SeverityCount:  severityCount,
		Alerts:         alerts,
		LastUpdate:     latestTime,
		ComplianceRate: complianceRate,
		TotalDAGs:      totalDAGs,
		HealthyDAGs:    healthyDAGs,
		WarningDAGs:    warningDAGs,
		CriticalDAGs:   criticalDAGs,
		Violations:     violations,
		TopDagsJSON:    string(topDagsJSON),
		TrendJSON:      string(trendJSON),
	}

	// Define template functions
	funcMap := template.FuncMap{
		"unmarshal": func(s string) interface{} {
			var result interface{}
			json.Unmarshal([]byte(s), &result)
			return result
		},
		"safeJS": func(s string) template.JS {
			return template.JS(s)
		},
	}

	// Create and parse the template
	tmpl := template.New("dashboard.html").Funcs(funcMap)
	tmpl, err = tmpl.ParseFiles("templates/dashboard.html")
	if err != nil {
		log.Printf("Template parsing error in dashboard.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Execute the template
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Template execution error in dashboardHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// Helper function to get integer from tags
func getIntFromTags(tags map[string]string, key string) int {
	if val, ok := tags[key]; ok {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return 0
}

func (h *WebHandler) MonitoringHandler(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// Get metrics
	metrics, err := h.store.Metrics().List(ctx, store.MetricFilter{})
	if err != nil {
		h.handleError(w, err, "Error getting metrics")
		return
	}

	// Get latest metric time
	latestTime, err := h.store.Metrics().GetLatestMetricTime(ctx)
	if err != nil {
		h.handleError(w, err, "Error getting latest metric time")
		return
	}

	// Get rules
	rules, err := h.store.Rules().ListRules(ctx)
	if err != nil {
		h.handleError(w, err, "Error getting rules")
		return
	}

	// Convert rules to SLARules
	slaRules := h.convertRulesToSLARules(rules)
	violations := services.CheckViolations(metrics, slaRules)

	// Process violations
	severityCount := make(map[string]int)
	for _, v := range violations {
		severityCount[v.Severity]++
	}
	if len(severityCount) == 0 {
		severityCount["Minor"] = 0
		severityCount["Critical"] = 0
	}
	severityJSON, err := json.Marshal(severityCount)
	if err != nil {
		h.handleError(w, err, "Error marshaling severity count")
		return
	}

	// Process alerts
	type Alert struct {
		DagID     string
		Timestamp string
		Severity  string
	}
	var alerts []Alert
	for i, v := range violations {
		if i >= 5 {
			break
		}
		alerts = append(alerts, Alert{
			DagID:     v.DagID,
			Timestamp: v.Timestamp.Format("2006-01-02 15:04"),
			Severity:  v.Severity,
		})
	}
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].Timestamp > alerts[j].Timestamp
	})

	// Calculate metrics for healthy/unhealthy DAGs
	dagViolationCount := make(map[string]int)
	for _, v := range violations {
		dagViolationCount[v.DagID]++
	}
	healthyCount := 0
	unhealthyCount := 0
	uniqueDags := make(map[string]bool)
	for _, metric := range metrics {
		if metric.TaskID == nil {
			uniqueDags[metric.DagID] = true
		}
	}
	for dagID := range uniqueDags {
		if dagViolationCount[dagID] == 0 {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	// Calculate duration metrics
	durationMetrics := make(map[string]float64)
	for _, metric := range metrics {
		if metric.Duration != nil {
			durationMetrics[metric.DagID] = *metric.Duration
		}
	}

	// Calculate success/failure counts
	successCount := 0
	failedCount := 0
	for _, m := range metrics {
		if strings.Contains(m.EventType, "success") {
			successCount++
		} else if strings.Contains(m.EventType, "failed") {
			failedCount++
		}
	}

	pieData := struct {
		Success int
		Failed  int
	}{Success: successCount, Failed: failedCount}
	pieJSON, err := json.Marshal(pieData)
	if err != nil {
		h.handleError(w, err, "Error marshaling pie data")
		return
	}

	// Process pagination
	pageStr := r.URL.Query().Get("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	const itemsPerPage = 5
	totalItems := len(violations)
	totalPages := (totalItems + itemsPerPage - 1) / itemsPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * itemsPerPage
	end := start + itemsPerPage
	if end > totalItems {
		end = totalItems
	}

	// Convert violations to display format
	type ViolatedRule struct {
		DagID     string
		TaskID    string
		Value     string
		Severity  string
		Timestamp string
	}
	var activeViolations []ViolatedRule
	for _, v := range violations {
		activeViolations = append(activeViolations, ViolatedRule{
			DagID:     v.DagID,
			TaskID:    v.TaskID,
			Value:     fmt.Sprintf("%.2f", v.Value),
			Severity:  v.Severity,
			Timestamp: v.Timestamp.Format("2006-01-02 15:04"),
		})
	}
	sort.Slice(activeViolations, func(i, j int) bool {
		return activeViolations[i].Timestamp > activeViolations[j].Timestamp
	})
	paginatedViolations := activeViolations[start:end]

	// Define template functions
	tmpl := template.New("monitoring.html").Funcs(template.FuncMap{
		"safeJS": func(b []byte) template.JS { return template.JS(b) },
		"add":    func(a, b int) int { return a + b },
		"sub":    func(a, b int) int { return a - b },
	})

	// Parse and execute template
	tmpl, err = tmpl.ParseFiles("templates/monitoring.html")
	if err != nil {
		h.handleError(w, err, "Template parsing error in monitoring.html")
		return
	}

	err = tmpl.Execute(w, struct {
		SeverityJSON        []byte
		Alerts              []Alert
		HealthyCount        int
		UnhealthyCount      int
		TotalMetrics        int
		LastMetricTime      string
		PieJSON             []byte
		PaginatedViolations []ViolatedRule
		Page                int
		TotalPages          int
	}{
		SeverityJSON:        severityJSON,
		Alerts:              alerts,
		HealthyCount:        healthyCount,
		UnhealthyCount:      unhealthyCount,
		TotalMetrics:        len(metrics),
		LastMetricTime:      latestTime.Format("2006-01-02 15:04:05"),
		PieJSON:             pieJSON,
		PaginatedViolations: paginatedViolations,
		Page:                page,
		TotalPages:          totalPages,
	})
	if err != nil {
		h.handleError(w, err, "Template execution error in monitoringHandler")
	}
}

func (h *WebHandler) MetricsTableHandler(w http.ResponseWriter, r *http.Request) {
	metrics, err := h.store.Metrics().List(context.Background(), store.MetricFilter{})
	if err != nil {
		log.Printf("Error getting metrics: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Aggregate stats per DAG
	type DagStats struct {
		SuccessCount           int
		FailureCount           int
		RunCount               int
		ConsecutiveFailures    int
		MaxConsecutiveFailures int
		LastSuccessTime        *time.Time
		LatestDuration         *float64
	}

	dagStats := make(map[string]*DagStats)
	for _, m := range metrics {
		if _, exists := dagStats[m.DagID]; !exists {
			dagStats[m.DagID] = &DagStats{}
		}
		stats := dagStats[m.DagID]

		execTime, _ := time.Parse(time.RFC3339, m.ExecutionTime)
		isTaskEvent := m.TaskID != nil
		isDagEvent := !isTaskEvent

		// Update counts
		if isTaskEvent {
			stats.RunCount++
			if m.EventType == "task_success" {
				stats.SuccessCount++
			} else if m.EventType == "task_failed" {
				stats.FailureCount++
			}
		} else if isDagEvent && m.EventType == "dag_success" {
			stats.RunCount++
			stats.SuccessCount++
		} else if isDagEvent && m.EventType == "dag_failed" {
			stats.RunCount++
			stats.FailureCount++
		}

		// Track consecutive failures
		if m.EventType == "task_failed" || m.EventType == "dag_failed" {
			stats.ConsecutiveFailures++
			if stats.ConsecutiveFailures > stats.MaxConsecutiveFailures {
				stats.MaxConsecutiveFailures = stats.ConsecutiveFailures
			}
		} else if m.EventType == "task_success" || m.EventType == "dag_success" {
			stats.ConsecutiveFailures = 0
			lastSuccess := execTime
			stats.LastSuccessTime = &lastSuccess
		}

		// Latest duration
		if m.Duration != nil {
			stats.LatestDuration = m.Duration
		}
	}

	// Prepare display metrics with readiness
	var displayMetrics []models.MetricDisplay
	for _, m := range metrics {
		durationStr := "N/A"
		if m.Duration != nil {
			durationStr = fmt.Sprintf("%.2f", *m.Duration)
		}

		// Compute readiness for this DAG
		stats := dagStats[m.DagID]
		successRate := 100.0
		if stats.RunCount > 0 {
			successRate = float64(stats.SuccessCount) / float64(stats.RunCount) * 100
		}

		freshnessScore := 100.0
		if stats.LastSuccessTime != nil {
			now := time.Now()
			freshnessScore = 100 - (float64(now.Sub(*stats.LastSuccessTime).Minutes()) / 5) // 5-min threshold
		}

		latencyScore := 100.0
		if stats.LatestDuration != nil {
			expectedDuration := 10.0 // Adjust based on your SLA
			latencyScore = 100 - ((*stats.LatestDuration - expectedDuration) / expectedDuration * 100)
		}

		consecutiveFailurePenalty := float64(stats.MaxConsecutiveFailures) / 5 * 100 // 5 as max threshold

		w1, w2, w3, w4 := 0.5, 0.3, 0.2, 0.1 // Weights
		readiness := (w1*successRate + w2*freshnessScore + w3*latencyScore - w4*consecutiveFailurePenalty) / (w1 + w2 + w3)
		readiness = math.Max(0, math.Min(100, readiness)) // Clamp between 0-100

		displayMetrics = append(displayMetrics, models.MetricDisplay{
			EventType:     m.EventType,
			DagID:         m.DagID,
			TaskID:        m.TaskID,
			ExecutionTime: m.ExecutionTime,
			StartTime:     m.StartTime,
			Duration:      durationStr,
			Readiness:     readiness,
		})
	}

	// Define the template with custom functions
	tmpl := template.New("metrics.html").Funcs(template.FuncMap{
		"js": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
	})

	// Parse the external template file
	tmpl, err = tmpl.ParseFiles("templates/metrics.html")
	if err != nil {
		log.Printf("Template parsing error in metrics.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Render the template with the data
	err = tmpl.Execute(w, struct{ Metrics []models.MetricDisplay }{displayMetrics})
	if err != nil {
		log.Printf("Template execution error in metricsTableHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (h *WebHandler) ViolationsHandler(w http.ResponseWriter, r *http.Request) {
	metrics, err := h.store.Metrics().List(context.Background(), store.MetricFilter{})
	if err != nil {
		log.Printf("Error getting metrics: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rules, err := h.store.Rules().ListRules(context.Background())
	if err != nil {
		log.Printf("Error getting rules: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Convert Rules to SLARules
	var slaRules []models.SLARule
	for _, rule := range rules {
		slaRule := models.SLARule{
			ID:           len(slaRules) + 1,
			DagID:        rule.Tags["dag_id"],
			FieldName:    rule.Tags["field_name"],
			Condition:    rule.Tags["condition"],
			Value:        fmt.Sprintf("%.2f", rule.Threshold),
			Severity:     rule.Tags["severity"],
			CreatedAt:    rule.CreatedAt,
			LastViolated: nil,
		}
		slaRules = append(slaRules, slaRule)
	}

	violations := services.CheckViolations(metrics, slaRules)

	// Define the template
	tmpl := template.New("violations.html").Funcs(template.FuncMap{
		"safeJS": func(b []byte) template.JS { return template.JS(b) },
	})

	// Parse the external template file
	tmpl, err = tmpl.ParseFiles("templates/violations.html")
	if err != nil {
		log.Printf("Template parsing error in violations.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Render the template with the data
	err = tmpl.Execute(w, struct {
		Violations []models.Violation
	}{
		Violations: violations,
	})
	if err != nil {
		log.Printf("Template execution error in violationsHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (h *WebHandler) ClientsHandler(w http.ResponseWriter, r *http.Request) {
	// Mock client data (replace with real data if available)
	type Client struct {
		Type       string
		IP         string
		Hostname   string
		LastSeen   string
		Connection string
	}
	clients := []Client{
		{Type: "Airflow", IP: "192.168.1.10", Hostname: "airflow.test.com", LastSeen: time.Now().Add(-5 * time.Minute).Format("2006-01-02 15:04"), Connection: "Plugin"},
		{Type: "Control-M", IP: "10.0.0.15", Hostname: "control-m.bmc.com", LastSeen: time.Now().Add(-2 * time.Hour).Format("2006-01-02 15:04"), Connection: "Plugin"},
		{Type: "Airflow", IP: "172.16.254.1", Hostname: "airflow2.test.com", LastSeen: time.Now().Format("2006-01-02 15:04"), Connection: "OpenTelemetry"},
	}

	tmpl := template.New("clients.html").Funcs(template.FuncMap{
		"mod": func(i, j int) int { return i % j }, // For alternating row colors
	})

	tmpl, err := tmpl.ParseFiles("templates/clients.html")
	if err != nil {
		log.Printf("Template parsing error in clients.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, clients)
	if err != nil {
		log.Printf("Template execution error in clientsHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (h *WebHandler) RulesHandler(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.Rules().ListRules(context.Background())
	if err != nil {
		log.Printf("Error getting rules: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Convert Rules to SLARules
	var slaRules []models.SLARule
	for _, rule := range rules {
		slaRule := models.SLARule{
			ID:           len(slaRules) + 1,
			DagID:        rule.Tags["dag_id"],
			FieldName:    rule.Tags["field_name"],
			Condition:    rule.Tags["condition"],
			Value:        fmt.Sprintf("%.2f", rule.Threshold),
			Severity:     rule.Tags["severity"],
			CreatedAt:    rule.CreatedAt,
			LastViolated: nil,
		}
		if windowMins, err := strconv.Atoi(rule.Tags["window_mins"]); err == nil {
			slaRule.WindowMins = windowMins
		}
		if countThresh, err := strconv.Atoi(rule.Tags["count_thresh"]); err == nil {
			slaRule.CountThresh = countThresh
		}
		slaRules = append(slaRules, slaRule)
	}

	metricFields := []string{"event_type", "task_id", "execution_time", "duration"}
	severityOptions := []string{"Minor", "Critical"}

	switch r.Method {
	case http.MethodGet:
		tmpl := template.New("rules.html")

		tmpl, err := tmpl.ParseFiles("templates/rules.html")
		if err != nil {
			log.Printf("Template parsing error in rules.html: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, struct {
			Rules           []models.SLARule
			MetricFields    []string
			SeverityOptions []string
		}{slaRules, metricFields, severityOptions})
		if err != nil {
			log.Printf("Template execution error in rulesHandler: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		err := r.ParseForm()
		if err != nil {
			log.Printf("Failed to parse form in POST: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Convert form values to Rule
		threshold, err := strconv.ParseFloat(r.FormValue("value"), 64)
		if err != nil {
			log.Printf("Invalid threshold value: %v", err)
			http.Error(w, "Invalid threshold value", http.StatusBadRequest)
			return
		}

		rule := &store.Rule{
			ID:          fmt.Sprintf("rule:%s:%s", r.FormValue("dag_id"), r.FormValue("field_name")),
			Name:        fmt.Sprintf("Rule for %s on %s", r.FormValue("dag_id"), r.FormValue("field_name")),
			Description: fmt.Sprintf("Check if %s %s %s", r.FormValue("field_name"), r.FormValue("condition"), r.FormValue("value")),
			Type:        "metric",
			Threshold:   threshold,
			Tags: map[string]string{
				"dag_id":     r.FormValue("dag_id"),
				"field_name": r.FormValue("field_name"),
				"condition":  r.FormValue("condition"),
				"severity":   r.FormValue("severity"),
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		err = h.store.Rules().StoreRule(context.Background(), rule)
		if err != nil {
			log.Printf("Error storing rule: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		h.renderRulesTable(w)

	case http.MethodDelete:
		indexStr := r.URL.Path[len("/rules/"):]
		index, err := strconv.Atoi(indexStr)
		if err != nil || index < 0 || index >= len(rules) {
			log.Printf("Invalid rule index for DELETE: %s", indexStr)
			http.Error(w, "Invalid rule index", http.StatusBadRequest)
			return
		}
		err = h.store.Rules().DeleteRule(context.Background(), rules[index].ID)
		if err != nil {
			log.Printf("Error deleting rule: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		h.renderRulesTable(w)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WebHandler) renderRulesTable(w http.ResponseWriter) {
	rules, err := h.store.Rules().ListRules(context.Background())
	if err != nil {
		log.Printf("Error getting rules: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Convert Rules to SLARules
	var slaRules []models.SLARule
	for _, rule := range rules {
		slaRule := models.SLARule{
			ID:           len(slaRules) + 1,
			DagID:        rule.Tags["dag_id"],
			FieldName:    rule.Tags["field_name"],
			Condition:    rule.Tags["condition"],
			Value:        fmt.Sprintf("%.2f", rule.Threshold),
			Severity:     rule.Tags["severity"],
			CreatedAt:    rule.CreatedAt,
			LastViolated: nil,
		}
		if windowMins, err := strconv.Atoi(rule.Tags["window_mins"]); err == nil {
			slaRule.WindowMins = windowMins
		}
		if countThresh, err := strconv.Atoi(rule.Tags["count_thresh"]); err == nil {
			slaRule.CountThresh = countThresh
		}
		slaRules = append(slaRules, slaRule)
	}

	// Define the template
	tmpl := template.New("rules_table.html")

	// Parse the external template file
	tmpl, err = tmpl.ParseFiles("templates/rules_table.html")
	if err != nil {
		log.Printf("Template parsing error in rules_table.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Render the template with the data
	err = tmpl.Execute(w, struct{ Rules []models.SLARule }{slaRules})
	if err != nil {
		log.Printf("Template execution error in renderRulesTable: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
