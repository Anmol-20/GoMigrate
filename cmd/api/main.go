package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/Anmol-20/gomigrate/internal/config"
	"github.com/Anmol-20/gomigrate/internal/database"
	"github.com/joho/godotenv"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json")

	response := HealthResponse{
		Status:  "UP",
		Service: "GoMigrate API",
	}

	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(response)
}

func readinessHandler(db *sql.DB) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "application/json")

		ctx, cancel := context.WithTimeout(
			r.Context(),
			2*time.Second,
		)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {

			w.WriteHeader(
				http.StatusServiceUnavailable,
			)

			json.NewEncoder(w).Encode(map[string]string{
				"status":   "NOT_READY",
				"database": "DOWN",
			})

			return
		}

		w.WriteHeader(http.StatusOK)

		json.NewEncoder(w).Encode(map[string]string{
			"status":   "READY",
			"database": "UP",
		})
	}
}

func main() {
	// Load local environment variables.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found; using system environment variables")
	}

	// Load application configuration.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Initialize MySQL connection.
	db, err := database.NewMySQL(cfg)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()

	log.Println("MySQL connection established")

	// Initialize HTTP router.
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /ready", readinessHandler(db))

	// Initialize HTTP server.
	server := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf(
		"GoMigrate API running on port %s",
		cfg.ServerPort,
	)

	if err := server.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}
