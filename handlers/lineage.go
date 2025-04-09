package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/ebracha/airflow-observer/models"
	"github.com/ebracha/airflow-observer/services"
	"github.com/ebracha/airflow-observer/storage"
)

func ReceiveLineage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var event models.LineageEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	storage.Lineage.Lock()
	storage.Lineage.Events = append(storage.Lineage.Events, event)
	storage.Lineage.Unlock()

	_ = services.ProcessLineageEvents()
	log.Printf("Received lineage event: %s for job %s/%s", event.EventType, event.Job.Namespace, event.Job.Name)
	w.WriteHeader(http.StatusOK)
}
