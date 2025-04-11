package handlers

import (
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
	"github.com/ebracha/airflow-observer/storage"
)

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Printf("Template parsing error in index.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, nil)
	if err != nil {
		log.Printf("Template execution error in indexHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func DashboardHandler(w http.ResponseWriter, r *http.Request) {
	storage.Metrics.Lock()
	metrics := storage.Metrics.Data
	storage.Metrics.Unlock()
	storage.Rules.Lock()
	rules := storage.Rules.Data
	storage.Rules.Unlock()

	violations := services.CheckViolations(metrics, rules)
	violationTimeSeries := make(map[string][]struct {
		Time  time.Time
		Count int
	})
	topDags := make(map[string]int)
	eventTypeCount := make(map[string]int)
	var complianceRate float64
	if len(violations) > 0 {
		complianceRate = float64(len(metrics)-len(violations)) / float64(len(metrics)) * 100
	} else {
		complianceRate = 100
	}

	for _, m := range metrics {
		eventTypeCount[m.EventType]++
	}
	for _, v := range violations {
		hour := v.Timestamp.Truncate(time.Hour)
		violationTimeSeries[v.DagID] = append(violationTimeSeries[v.DagID], struct {
			Time  time.Time
			Count int
		}{hour, 1})
		topDags[v.DagID]++
	}

	type TrendPoint struct {
		Date  string
		Count int
	}
	trendDataByGranularity := make(map[string][]TrendPoint)
	now := time.Now()
	for _, granularity := range []string{"Minutes", "Hours", "Days"} {
		totalViolations := make(map[string]int)
		var format string
		var cutoff time.Time
		var step time.Duration
		var steps int
		switch granularity {
		case "Minutes":
			format = "15:04"
			cutoff = now.Add(-time.Hour)
			step = time.Minute * 1
			steps = 60
		case "Hours":
			format = "2006-01-02 15:00"
			cutoff = now.Add(-24 * time.Hour)
			step = time.Hour
			steps = 24
		case "Days":
			format = "2006-01-02"
			cutoff = now.AddDate(0, 0, -7)
			step = time.Hour * 24
			steps = 7
		}
		for _, timeSeries := range violationTimeSeries {
			for _, point := range timeSeries {
				if point.Time.Before(cutoff) {
					continue
				}
				dateStr := point.Time.Format(format)
				totalViolations[dateStr] += point.Count
			}
		}
		var trendData []TrendPoint
		for i := 0; i < steps; i++ {
			date := now.Add(-step * time.Duration(i))
			dateStr := date.Format(format)
			count := totalViolations[dateStr]
			trendData = append(trendData, TrendPoint{Date: dateStr, Count: count})
		}
		sort.Slice(trendData, func(i, j int) bool {
			return trendData[i].Date < trendData[j].Date
		})
		trendDataByGranularity[granularity] = trendData
	}
	trendJSON, err := json.Marshal(trendDataByGranularity)
	if err != nil {
		log.Printf("Error marshaling trend data: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	violationTSJSON, err := json.Marshal(violationTimeSeries)
	if err != nil {
		log.Printf("Error marshaling violation time series: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	topDagsJSON, err := json.Marshal(topDags)
	if err != nil {
		log.Printf("Error marshaling top DAGs: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	eventTypeJSON, err := json.Marshal(eventTypeCount)
	if err != nil {
		log.Printf("Error marshaling event type count: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	dagIDs := make([]string, 0, len(violationTimeSeries))
	for dagID := range violationTimeSeries {
		dagIDs = append(dagIDs, dagID)
	}
	defaultDag := ""
	if len(dagIDs) > 0 {
		defaultDag = dagIDs[0]
	}

	// Define the template with custom functions
	tmpl := template.New("dashboard.html").Funcs(template.FuncMap{
		"safeJS": func(b []byte) template.JS { return template.JS(b) },
		"unmarshal": func(b []byte) map[string]int {
			var m map[string]int
			err := json.Unmarshal(b, &m)
			if err != nil {
				log.Printf("Error unmarshaling JSON: %v", err)
				return map[string]int{}
			}
			return m
		},
	})

	// Parse the external template file
	tmpl, err = tmpl.ParseFiles("templates/dashboard.html")
	if err != nil {
		log.Printf("Template parsing error in dashboard.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Render the template with the data
	err = tmpl.Execute(w, struct {
		ViolationTSJSON []byte
		TopDagsJSON     []byte
		EventTypeJSON   []byte
		ComplianceRate  float64
		DagIDs          []string
		DefaultDag      string
		Violations      []models.Violation
		TrendJSON       []byte
	}{
		ViolationTSJSON: violationTSJSON,
		TopDagsJSON:     topDagsJSON,
		EventTypeJSON:   eventTypeJSON,
		ComplianceRate:  complianceRate,
		DagIDs:          dagIDs,
		DefaultDag:      defaultDag,
		Violations:      violations,
		TrendJSON:       trendJSON,
	})
	if err != nil {
		log.Printf("Template execution error in dashboardHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func MonitoringHandler(w http.ResponseWriter, r *http.Request) {
	storage.Metrics.Lock()
	metrics := storage.Metrics.Data
	storage.Metrics.Unlock()
	storage.Rules.Lock()
	rules := storage.Rules.Data
	storage.Rules.Unlock()

	violations := services.CheckViolations(metrics, rules)
	severityCount := make(map[string]int)
	for _, v := range violations {
		for _, rule := range rules {
			if rule.FieldName == v.FieldName && rule.Condition == v.Condition {
				severityCount[rule.Severity]++
				break
			}
		}
	}
	if len(severityCount) == 0 {
		severityCount["Minor"] = 0
		severityCount["Critical"] = 0
	}
	severityJSON, err := json.Marshal(severityCount)
	if err != nil {
		log.Printf("Error marshaling severity count: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

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
		severity := "Minor"
		for _, rule := range rules {
			if rule.FieldName == v.FieldName && rule.Condition == v.Condition {
				severity = rule.Severity
				break
			}
		}
		alerts = append(alerts, Alert{
			DagID:     v.DagID,
			Timestamp: v.Timestamp.Format("2006-01-02 15:04"),
			Severity:  severity,
		})
	}
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].Timestamp > alerts[j].Timestamp
	})

	dagViolationCount := make(map[string]int)
	for _, v := range violations {
		dagViolationCount[v.DagID]++
	}
	healthyCount := 0
	unhealthyCount := 0
	uniqueDags := make(map[string]bool)
	for _, m := range metrics {
		if m.TaskID == nil {
			uniqueDags[m.DagID] = true
		}
	}
	for dagID := range uniqueDags {
		if dagViolationCount[dagID] == 0 {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	events, err := storage.GetLineageStore().ReadAll()
	if err != nil {
		log.Printf("Error reading lineage events: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	totalMetrics := len(events)

	var lastMetricTime string
	if len(events) > 0 {
		latest, err := storage.GetLineageStore().GetLatestMetricTime()
		if err != nil {
			log.Printf("Error getting latest metric time: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		lastMetricTime = latest.Format("2006-01-02 15:04")
	} else {
		lastMetricTime = "N/A"
	}

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
		log.Printf("Error marshaling pie data: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	type ViolatedRule struct {
		DagID     string
		FieldName string
		Condition string
		Value     string
		Severity  string
		Timestamp string
	}
	var activeViolations []ViolatedRule
	for _, v := range violations {
		for _, rule := range rules {
			if rule.FieldName == v.FieldName && rule.Condition == v.Condition {
				activeViolations = append(activeViolations, ViolatedRule{
					DagID:     v.DagID,
					FieldName: v.FieldName,
					Condition: v.Condition,
					Value:     v.RuleValue,
					Severity:  rule.Severity,
					Timestamp: v.Timestamp.Format("2006-01-02 15:04"),
				})
				break
			}
		}
	}
	sort.Slice(activeViolations, func(i, j int) bool {
		return activeViolations[i].Timestamp > activeViolations[j].Timestamp
	})

	const itemsPerPage = 5
	pageStr := r.URL.Query().Get("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	totalItems := len(activeViolations)
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
	paginatedViolations := activeViolations[start:end]

	// Define the template with custom functions
	tmpl := template.New("monitoring.html").Funcs(template.FuncMap{
		"safeJS": func(b []byte) template.JS { return template.JS(b) },
		"add":    func(a, b int) int { return a + b },
		"sub":    func(a, b int) int { return a - b },
	})

	// Parse the external template file
	tmpl, err = tmpl.ParseFiles("templates/monitoring.html")
	if err != nil {
		log.Printf("Template parsing error in monitoring.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Render the template with the data
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
		TotalMetrics:        totalMetrics,
		LastMetricTime:      lastMetricTime,
		PieJSON:             pieJSON,
		PaginatedViolations: paginatedViolations,
		Page:                page,
		TotalPages:          totalPages,
	})
	if err != nil {
		log.Printf("Template execution error in monitoringHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func MetricsTableHandler(w http.ResponseWriter, r *http.Request) {
	storage.Metrics.Lock()
	metrics := storage.Metrics.Data
	storage.Metrics.Unlock()

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
	tmpl, err := tmpl.ParseFiles("templates/metrics.html")
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

func ViolationsHandler(w http.ResponseWriter, r *http.Request) {
	storage.Metrics.Lock()
	metrics := storage.Metrics.Data
	storage.Metrics.Unlock()
	storage.Rules.Lock()
	rules := storage.Rules.Data
	storage.Rules.Unlock()

	violations := services.CheckViolations(metrics, rules)

	// Define the template
	tmpl := template.New("violations.html")

	// Parse the external template file
	tmpl, err := tmpl.ParseFiles("templates/violations.html")
	if err != nil {
		log.Printf("Template parsing error in violations.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Render the template with the data
	err = tmpl.Execute(w, struct{ Violations []models.Violation }{violations})
	if err != nil {
		log.Printf("Template execution error in violationsHandler: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func ClientsHandler(w http.ResponseWriter, r *http.Request) {
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

func RulesHandler(w http.ResponseWriter, r *http.Request) {
	storage.Rules.Lock()
	defer storage.Rules.Unlock()

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
		}{storage.Rules.Data, metricFields, severityOptions})
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
		newRule := models.SLARule{
			DagID:     r.FormValue("dag_id"),
			FieldName: r.FormValue("field_name"),
			Condition: r.FormValue("condition"),
			Value:     r.FormValue("value"),
			Severity:  r.FormValue("severity"),
		}
		storage.Rules.Data = append(storage.Rules.Data, newRule)
		log.Printf("Added new rule: %+v, Total rules: %d", newRule, len(storage.Rules.Data))
		w.Header().Set("Content-Type", "text/html")
		renderRulesTable(w)

	case http.MethodPut:
		err := r.ParseForm()
		if err != nil {
			log.Printf("Failed to parse form in PUT: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		indexStr := r.URL.Path[len("/rules/"):]
		index, err := strconv.Atoi(indexStr)
		if err != nil || index < 0 || index >= len(storage.Rules.Data) {
			log.Printf("Invalid rule index: %s", indexStr)
			http.Error(w, "Invalid rule index", http.StatusBadRequest)
			return
		}
		storage.Rules.Data[index] = models.SLARule{
			DagID:        r.FormValue("dag_id"),
			FieldName:    r.FormValue("field_name"),
			Condition:    r.FormValue("condition"),
			Value:        r.FormValue("value"),
			Severity:     r.FormValue("severity"),
			LastViolated: storage.Rules.Data[index].LastViolated,
		}
		log.Printf("Updated rule at index %d: %+v", index, storage.Rules.Data[index])
		w.Header().Set("Content-Type", "text/html")
		renderRulesTable(w)

	case http.MethodDelete:
		indexStr := r.URL.Path[len("/rules/"):]
		index, err := strconv.Atoi(indexStr)
		if err != nil || index < 0 || index >= len(storage.Rules.Data) {
			log.Printf("Invalid rule index for DELETE: %s", indexStr)
			http.Error(w, "Invalid rule index", http.StatusBadRequest)
			return
		}
		storage.Rules.Data = append(storage.Rules.Data[:index], storage.Rules.Data[index+1:]...)
		log.Printf("Deleted rule at index %d, Total rules: %d", index, len(storage.Rules.Data))
		w.Header().Set("Content-Type", "text/html")
		renderRulesTable(w)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func renderRulesTable(w http.ResponseWriter) {
	// Define the template
	tmpl := template.New("rules_table.html")

	// Parse the external template file
	tmpl, err := tmpl.ParseFiles("templates/rules_table.html")
	if err != nil {
		log.Printf("Template parsing error in rules_table.html: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Render the template with the data
	err = tmpl.Execute(w, struct{ Rules []models.SLARule }{storage.Rules.Data})
	if err != nil {
		log.Printf("Template execution error in renderRulesTable: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
