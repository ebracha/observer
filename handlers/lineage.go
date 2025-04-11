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
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if event.EventType == "" || event.Job.Name == "" || event.Job.Namespace == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	err := storage.GetLineageStore().Create(event)
	if err != nil {
		log.Printf("Failed to create lineage event: %v", err)
		http.Error(w, "Failed to create lineage event", http.StatusInternalServerError)
		return
	}

	services.ProcessLineageEvents()

	log.Printf("Received lineage event: %s job: %s/%s", event.EventType, event.Job.Namespace, event.Job.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
