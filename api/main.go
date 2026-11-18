package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// HealthResponse represents the health API response.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

// healthHandler handles GET /health requests.
func healthHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json")

	response := HealthResponse{
		Status:  "UP",
		Service: "GoMigrate API",
	}

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode health response: %v", err)
	}
}

func main() {

	// Create HTTP router.
	mux := http.NewServeMux()

	// Register health endpoint.
	mux.HandleFunc("GET /health", healthHandler)

	// Configure HTTP server.
	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("GoMigrate API starting on port 8080")

	// Start HTTP server.
	if err := server.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
