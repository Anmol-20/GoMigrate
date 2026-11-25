package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/joho/godotenv"

	"github.com/Anmol-20/gomigrate/internal/config"
	"github.com/Anmol-20/gomigrate/internal/database"
	"github.com/Anmol-20/gomigrate/internal/handler"
	"github.com/Anmol-20/gomigrate/internal/repository"
	"github.com/Anmol-20/gomigrate/internal/security"
	"github.com/Anmol-20/gomigrate/internal/service"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "UP",
		"service": "GoMigrate API",
	})
}

func readinessHandler(db *sql.DB) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		ctx, cancel := context.WithTimeout(
			r.Context(),
			2*time.Second,
		)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			http.Error(
				w,
				`{"status":"NOT_READY","database":"DOWN"}`,
				http.StatusServiceUnavailable,
			)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		json.NewEncoder(w).Encode(map[string]string{
			"status":   "READY",
			"database": "UP",
		})
	}
}

func main() {

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found; using system environment")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	db, err := database.NewMySQL(cfg)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()

	encryptor, err := security.NewEncryptor(
		cfg.EncryptionKey,
	)
	if err != nil {
		log.Fatalf("Encryption initialization failed: %v", err)
	}

	// Repository layer.
	connectionRepo := repository.NewMySQLConnectionRepository(db)

	// Service layer.
	connectionService := service.NewConnectionService(
		connectionRepo,
		encryptor,
	)

	// Handler layer.
	connectionHandler := handler.NewConnectionHandler(
		connectionService,
	)

	schemaComparisonService := service.NewSchemaComparisonService(
		connectionService,
	)

	schemaComparisonHandler := handler.NewSchemaComparisonHandler(
		schemaComparisonService,
	)
	migrationPlanRepo := repository.NewMySQLMigrationPlanRepository(db)
	migrationPlanService := service.NewMigrationPlanService(
		connectionService,
		migrationPlanRepo,
	)

	migrationPlanHandler := handler.NewMigrationPlanHandler(
		migrationPlanService,
	)
	migrationExecutionRepo := repository.NewMySQLMigrationExecutionRepository(db)
	migrationExecutionService := service.NewMigrationExecutionService(
		migrationPlanRepo, migrationExecutionRepo, connectionService,
	)
	migrationExecutionHandler := handler.NewMigrationExecutionHandler(migrationExecutionService)

	// HTTP router.
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /ready", readinessHandler(db))

	mux.HandleFunc(
		"POST /api/v1/schema/compare",
		schemaComparisonHandler.Compare,
	)

	mux.HandleFunc(
		"POST /api/v1/migration-plans",
		migrationPlanHandler.Generate,
	)
	mux.HandleFunc("POST /api/v1/migration-plans/{id}/execute", migrationExecutionHandler.Execute)
	mux.HandleFunc("GET /api/v1/migration-executions/{id}", migrationExecutionHandler.GetByID)
	mux.HandleFunc("GET /api/v1/migration-executions/{id}/reconcile", migrationExecutionHandler.Reconcile)
	mux.HandleFunc("GET /api/v1/migration-executions/stale", migrationExecutionHandler.ListStale)

	mux.HandleFunc(
		"POST /api/v1/connections/{id}/test",
		connectionHandler.TestConnection,
	)

	mux.HandleFunc(
		"POST /api/v1/connections",
		connectionHandler.Create,
	)

	mux.HandleFunc(
		"GET /api/v1/connections",
		connectionHandler.List,
	)

	mux.HandleFunc(
		"GET /api/v1/connections/{id}",
		connectionHandler.GetByID,
	)

	mux.HandleFunc(
		"DELETE /api/v1/connections/{id}",
		connectionHandler.Delete,
	)

	mux.HandleFunc(
		"GET /api/v1/connections/{id}/schema",
		connectionHandler.DiscoverSchema,
	)

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
