package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ebracha/airflow-observer/handlers"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/lineage/events", handlers.ReceiveLineage)
	mux.HandleFunc("/", handlers.IndexHandler)
	mux.HandleFunc("/dashboard", handlers.DashboardHandler)
	mux.HandleFunc("/monitoring", handlers.MonitoringHandler)
	mux.HandleFunc("/metrics", handlers.MetricsTableHandler)
	mux.HandleFunc("/violations", handlers.ViolationsHandler)
	mux.HandleFunc("/rules/", handlers.RulesHandler)
	mux.HandleFunc("/rules", handlers.RulesHandler)
	mux.HandleFunc("/clients", handlers.ClientsHandler)

	server := &http.Server{
		Addr:         ":8000",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	serverErrors := make(chan error, 1)

	go func() {
		log.Println("Serving UI at :8000 and accepting metrics at :8000/lineage/events")
		serverErrors <- server.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGTSTP)

	select {
	case err := <-serverErrors:
		log.Printf("Server error: %v", err)

	case sig := <-shutdown:
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Attempt to gracefully shutdown the server
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Could not gracefully shutdown server: %v", err)
			if err := server.Close(); err != nil {
				log.Printf("Could not force close server: %v", err)
			}
		} else {
			log.Println("Server gracefully shutdown")
		}

		if sig == syscall.SIGTSTP {
			log.Println("Server suspended")
			os.Exit(130)
		}
	}

	os.Exit(0)
}
