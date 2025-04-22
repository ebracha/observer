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
	"github.com/ebracha/airflow-observer/store"
)

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
	Violations     []*models.Violation
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

func (h *WebHandler) handleError(w http.ResponseWriter, err error, message string) {
	log.Printf("%s: %v", message, err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

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
	// Define time window for metrics AND violations
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	// Get metrics from the last 24 hours
	metricFilter := store.MetricFilter{
		StartTime: startTime,
		EndTime:   endTime,
	}
	metrics, err := h.store.Metrics().List(ctx, metricFilter)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting metrics: %v", err), http.StatusInternalServerError)
		return
	}

	violationFilter := store.ViolationFilter{ // Assuming a ViolationFilter exists
		StartTime: startTime,
		EndTime:   endTime,
	}
	violations, err := h.store.Violations().GetViolations(ctx, violationFilter)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting violations: %v", err), http.StatusInternalServerError)
		return
	}

	// Calculate severity count and DAG status based on fetched violations
	severityCount := make(map[string]int)
	dagStatus := make(map[string]string) // Maps DAG ID to its worst severity status ("Healthy", "Warning", "Critical")
	for _, v := range violations {
		severityCount[v.Severity]++
		currentSeverity := dagStatus[v.DagID]
		if currentSeverity == "" || currentSeverity == "Healthy" {
			dagStatus[v.DagID] = v.Severity
		} else if currentSeverity == "Warning" && v.Severity == "Critical" {
			dagStatus[v.DagID] = v.Severity
		}
	}

	// Get latest metric time (still relevant)
	latestTime, err := h.store.Metrics().GetLatestMetricTime(ctx)
	if err != nil {
		log.Printf("Error getting latest metric time: %v", err)
		latestTime = time.Time{} // Use zero time
	}

	// Gather alerts from fetched violations
	alerts := make([]Alert, 0, len(violations))
	for _, v := range violations {
		alerts = append(alerts, Alert{
			DagID:     v.DagID,
			Timestamp: v.Timestamp,
			Severity:  v.Severity,
		})
	}
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].Timestamp.After(alerts[j].Timestamp)
	})

	// --- DAG Statistics Calculation (remains largely the same, uses metrics) ---
	dagStats := make(map[string]*DagStats)
	trendData := TrendData{
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
				Name: m.DagID,
			}
		}
		stats := dagStats[m.DagID]

		// Process only DAG-level events for Success/Failure counts and Duration/LastRunTime
		if m.TaskID == nil { // This is a DAG-level metric
			if m.EventType == "dag_success" {
				stats.SuccessCount++
			} else if m.EventType == "dag_failed" {
				stats.FailureCount++
			}
			// ... (rest of stats aggregation: AvgDuration, LastRunTime)
			currentTotalRuns := stats.SuccessCount + stats.FailureCount
			if m.Duration != nil && currentTotalRuns > 0 {
				if currentTotalRuns == 1 {
					stats.AvgDuration = *m.Duration
				} else {
					stats.AvgDuration = (stats.AvgDuration*float64(currentTotalRuns-1) + *m.Duration) / float64(currentTotalRuns)
				}
			}
			if m.ExecutionTime != "" {
				if execTime, err := time.Parse(time.RFC3339, m.ExecutionTime); err == nil {
					if execTime.After(stats.LastRunTime) {
						stats.LastRunTime = execTime
					}
					// --- Trend Data Calculation ---
					day := execTime.Format("2006-01-02")
					// ... (append to trendData.Days, trendData.Hours, trendData.Minutes)
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
					// --- End Trend Data ---
				}
			}
		}
	}

	// Count DAGs by status based on the final aggregated stats and violations
	totalDAGs := len(dagStats) // Total unique DAGs based on metrics seen
	healthyDAGs := 0
	warningDAGs := 0
	criticalDAGs := 0

	for dagID, stats := range dagStats {
		status, exists := dagStatus[dagID] // Check status derived from stored violations
		if !exists || status == "" {       // No violations for this DAG
			stats.Status = "Healthy"
			healthyDAGs++
		} else {
			stats.Status = status
			if status == "Warning" {
				warningDAGs++
			} else if status == "Critical" {
				criticalDAGs++
			}
		}
	}
	// --- End Refactored DAG Statistics Calculation ---

	// Sort trend data
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

	// Convert dagStats to slice and sort by last run time for Top DAGs
	var topDags []DagStats
	for _, stats := range dagStats {
		topDags = append(topDags, *stats)
	}
	sort.Slice(topDags, func(i, j int) bool {
		return topDags[i].LastRunTime.After(topDags[j].LastRunTime)
	})

	if len(topDags) > 5 {
		topDags = topDags[:5]
	}

	topDagsJSON, err := json.Marshal(topDags)
	if err != nil {
		log.Printf("Error marshaling top DAGs: %v", err)
		topDagsJSON = []byte("[]")
	}

	// Prepare data for the template
	data := DashboardData{
		SeverityCount:  severityCount, // Calculated from stored violations
		Alerts:         alerts,        // Derived from stored violations
		LastUpdate:     latestTime,
		ComplianceRate: complianceRate, // Calculated using status derived from stored violations
		TotalDAGs:      totalDAGs,
		HealthyDAGs:    healthyDAGs,
		WarningDAGs:    warningDAGs,
		CriticalDAGs:   criticalDAGs,
		Violations:     violations, // Pass the fetched violations to the template
		TopDagsJSON:    string(topDagsJSON),
		TrendJSON:      string(trendJSON),
	}

	// Render the template
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
	tmpl := template.New("dashboard.html").Funcs(funcMap)
	tmpl, err = tmpl.ParseFiles("templates/dashboard.html")
	if err != nil {
		log.Printf("Template parsing error in dashboard.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Template execution error in dashboardHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (h *WebHandler) MonitoringHandler(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	startTime := time.Now().Add(-24 * time.Hour) // Example: 24-hour window
	endTime := time.Now()

	// Get metrics
	metricFilter := store.MetricFilter{StartTime: startTime, EndTime: endTime}
	metrics, err := h.store.Metrics().List(ctx, metricFilter)
	if err != nil {
		h.handleError(w, err, "Error getting metrics")
		return
	}

	// Get latest metric time
	latestTime, err := h.store.Metrics().GetLatestMetricTime(ctx)
	if err != nil {
		// Log error and continue, maybe use zero time
		log.Printf("Error getting latest metric time: %v", err)
		latestTime = time.Time{}
	}

	// --- Fetch VIOLATIONS from the ViolationStore ---
	violationFilter := store.ViolationFilter{ // Assuming a ViolationFilter exists
		StartTime: startTime,
		EndTime:   endTime,
	}
	violations, err := h.store.Violations().GetViolations(ctx, violationFilter)
	if err != nil {
		h.handleError(w, err, "Error getting violations")
		return
	}

	// Process violations for display
	severityCount := make(map[string]int)
	for _, v := range violations {
		severityCount[v.Severity]++
	}
	if len(severityCount) == 0 {
		severityCount["Minor"] = 0 // Ensure keys exist for template
		severityCount["Critical"] = 0
	}
	severityJSON, err := json.Marshal(severityCount)
	if err != nil {
		h.handleError(w, err, "Error marshaling severity count")
		return
	}

	// Process alerts from fetched violations
	type Alert struct { // Local type definition for MonitoringHandler alerts
		DagID     string
		Timestamp string
		Severity  string
	}
	var alerts []Alert
	for i, v := range violations {
		if i >= 5 { // Limit alerts displayed
			break
		}
		alerts = append(alerts, Alert{
			DagID:     v.DagID,
			Timestamp: v.Timestamp.Format("2006-01-02 15:04"),
			Severity:  v.Severity,
		})
	}
	sort.Slice(alerts, func(i, j int) bool { // Sort alerts by timestamp desc
		return alerts[i].Timestamp > alerts[j].Timestamp
	})

	// Calculate metrics for healthy/unhealthy DAGs based on fetched violations
	dagViolationCount := make(map[string]int)
	for _, v := range violations {
		dagViolationCount[v.DagID]++
	}
	healthyCount := 0
	unhealthyCount := 0
	uniqueDags := make(map[string]bool)
	for _, metric := range metrics { // Still need metrics to know which DAGs ran
		if metric.TaskID == nil { // Count unique DAGs based on DAG-level metrics
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
		if m.TaskID == nil { // Consider only DAG-level events
			if strings.Contains(m.EventType, "success") {
				successCount++
			} else if strings.Contains(m.EventType, "failed") {
				failedCount++
			}
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

	// Process pagination for violations
	pageStr := r.URL.Query().Get("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	const itemsPerPage = 5
	totalItems := len(violations) // Total violations from store
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

	// Convert fetched violations to display format for pagination
	type ViolatedRule struct { // Local type for display
		DagID       string
		TaskID      string
		Value       string
		Severity    string
		Timestamp   string
		Description string // Add description
	}
	var activeViolations []ViolatedRule
	for _, v := range violations {
		activeViolations = append(activeViolations, ViolatedRule{
			DagID:       v.DagID,
			TaskID:      v.TaskID,
			Value:       fmt.Sprintf("%.2f", v.Value),
			Severity:    v.Severity,
			Timestamp:   v.Timestamp.Format("2006-01-02 15:04"),
			Description: v.Description, // Include description from violation
		})
	}
	sort.Slice(activeViolations, func(i, j int) bool { // Sort all before pagination
		return activeViolations[i].Timestamp > activeViolations[j].Timestamp
	})
	paginatedViolations := activeViolations[start:end]

	// Render template
	tmpl := template.New("monitoring.html").Funcs(template.FuncMap{
		"safeJS": func(b []byte) template.JS { return template.JS(b) },
		"add":    func(a, b int) int { return a + b },
		"sub":    func(a, b int) int { return a - b },
	})
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
		TotalMetrics        int // This might be less relevant now or calculated differently
		LastMetricTime      string
		PieJSON             []byte
		PaginatedViolations []ViolatedRule // Use the paginated fetched violations
		Page                int
		TotalPages          int
	}{
		SeverityJSON:        severityJSON, // Calculated from stored violations
		Alerts:              alerts,       // Derived from stored violations
		HealthyCount:        healthyCount, // Calculated using status derived from stored violations
		UnhealthyCount:      unhealthyCount,
		TotalMetrics:        len(metrics), // Still based on fetched metrics
		LastMetricTime:      latestTime.Format("2006-01-02 15:04:05"),
		PieJSON:             pieJSON, // Calculated from metrics
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
	ctx := context.Background()
	// Define time window or other criteria for fetching violations
	startTime := time.Now().Add(-7 * 24 * time.Hour) // Example: Last 7 days
	endTime := time.Now()

	// --- Fetch VIOLATIONS from the ViolationStore ---
	violationFilter := store.ViolationFilter{ // Assuming a ViolationFilter exists
		StartTime: startTime,
		EndTime:   endTime,
		// Add other filters if needed (e.g., Severity, DagID)
	}
	violations, err := h.store.Violations().GetViolations(ctx, violationFilter)
	if err != nil {
		log.Printf("Error getting violations: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Sort violations, e.g., by timestamp descending
	sort.Slice(violations, func(i, j int) bool {
		return violations[i].Timestamp.After(violations[j].Timestamp)
	})

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

	// Render the template with the fetched violations
	err = tmpl.Execute(w, struct {
		Violations []*models.Violation
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
		var err error       // Declare err once at the top of the block
		err = r.ParseForm() // Use = for assignment
		if err != nil {
			log.Printf("Failed to parse form in POST: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Determine if the field requires a numeric threshold
		fieldName := r.FormValue("field_name")
		requiresNumericThreshold := false
		switch fieldName {
		case "duration": // Add other numeric metric fields here
			requiresNumericThreshold = true
			// Add cases for other potential numeric fields if any
		}

		var threshold float64
		// Removed var err error declaration from here
		formValue := r.FormValue("value")

		if requiresNumericThreshold {
			// Try parsing only if the field is expected to be numeric
			// Use = instead of := to assign to the existing err variable
			threshold, err = strconv.ParseFloat(formValue, 64)
			if err != nil {
				log.Printf("Invalid numeric threshold value for field '%s': %v", fieldName, err)
				http.Error(w, fmt.Sprintf("Invalid numeric value '%s' for field '%s'", formValue, fieldName), http.StatusBadRequest)
				return
			}
		} // No parsing needed for non-numeric fields (like event_type)

		// Create the rule
		rule := &store.Rule{
			ID:          fmt.Sprintf("rule:%s:%s", r.FormValue("dag_id"), fieldName),
			Name:        fmt.Sprintf("Rule for %s on %s", r.FormValue("dag_id"), fieldName),
			Description: fmt.Sprintf("Check if %s %s %s", fieldName, r.FormValue("condition"), formValue),
			Type:        "metric",
			Threshold:   threshold, // Store the parsed float (will be 0 if not numeric)
			Tags: map[string]string{
				"dag_id":     r.FormValue("dag_id"),
				"field_name": fieldName,
				"condition":  r.FormValue("condition"),
				"severity":   r.FormValue("severity"),
				// Store the original string value, especially for non-numeric checks
				"value_str": formValue,
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		// Add optional window/count tags if present
		if windowMinsStr := r.FormValue("window_mins"); windowMinsStr != "" {
			rule.Tags["window_mins"] = windowMinsStr
		}
		if countThreshStr := r.FormValue("count_thresh"); countThreshStr != "" {
			rule.Tags["count_thresh"] = countThreshStr
		}

		// Use = instead of := to assign to the existing err variable
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
