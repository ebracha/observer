package models

import "time"

type Metric struct {
	EventType     string   `json:"event_type"`
	DagID         string   `json:"dag_id"`
	TaskID        *string  `json:"task_id"`
	ExecutionTime string   `json:"execution_time"`
	StartTime     *string  `json:"start_time"`
	Duration      *float64 `json:"duration"`
}

type MetricDisplay struct {
	EventType     string  `json:"event_type"`
	DagID         string  `json:"dag_id"`
	TaskID        *string `json:"task_id"`
	ExecutionTime string  `json:"execution_time"`
	StartTime     *string `json:"start_time"`
	Duration      string  `json:"duration"`
	Readiness     float64 `json:"readiness"`
}

type SLARule struct {
	ID           int        `json:"id"`
	DagID        string     `json:"dag_id"`
	FieldName    string     `json:"field_name"`
	Condition    string     `json:"condition"`
	Value        string     `json:"value"`
	WindowMins   int        `json:"window_mins"`
	CountThresh  int        `json:"count_thresh"`
	CreatedAt    time.Time  `json:"created_at"`
	LastViolated *time.Time `json:"last_violated"`
	Severity     string     `json:"severity"`
}

type Violation struct {
	DagID     string
	TaskID    string
	FieldName string
	Value     string
	Condition string
	RuleValue string
	SLAMissed bool
	Timestamp time.Time
}

type LineageEvent struct {
	EventTime string `json:"eventTime"`
	EventType string `json:"eventType"`
	Inputs    []struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"inputs"`
	Job struct {
		Facets struct {
			JobType struct {
				Producer       string `json:"_producer"`
				SchemaURL      string `json:"_schemaURL"`
				Integration    string `json:"integration"`
				JobType        string `json:"jobType"`
				ProcessingType string `json:"processingType"`
			} `json:"jobType"`
		} `json:"facets"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"job"`
	Outputs []struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"outputs"`
	Producer string `json:"producer"`
	Run      struct {
		Facets struct {
			AirflowState struct {
				Producer    string            `json:"_producer"`
				SchemaURL   string            `json:"_schemaURL"`
				DagRunState string            `json:"dagRunState"`
				TasksState  map[string]string `json:"tasksState"`
			} `json:"airflowState"`
			Debug struct {
				Producer  string            `json:"_producer"`
				SchemaURL string            `json:"_schemaURL"`
				Packages  map[string]string `json:"packages"`
			} `json:"debug"`
		} `json:"facets"`
		RunId string `json:"runId"`
	} `json:"run"`
	SchemaURL string `json:"schemaURL"`
}
