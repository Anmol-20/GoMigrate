package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/Anmol-20/gomigrate/internal/connection"
	"github.com/Anmol-20/gomigrate/internal/service"
)

type MigrationPlanHandler struct {
	service *service.MigrationPlanService
}

func NewMigrationPlanHandler(
	svc *service.MigrationPlanService,
) *MigrationPlanHandler {

	return &MigrationPlanHandler{
		service: svc,
	}
}

type GenerateMigrationPlanRequest struct {
	SourceConnectionID int64 `json:"source_connection_id"`
	TargetConnectionID int64 `json:"target_connection_id"`
}

func (h *MigrationPlanHandler) Generate(
	w http.ResponseWriter,
	r *http.Request,
) {

	// Step 1: Decode request body.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req GenerateMigrationPlanRequest

	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Reject additional JSON values.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(
			w,
			http.StatusBadRequest,
			"request must contain one JSON object",
		)
		return
	}

	// Step 2: Validate connection IDs.
	if req.SourceConnectionID <= 0 ||
		req.TargetConnectionID <= 0 ||
		req.SourceConnectionID == req.TargetConnectionID {

		writeError(
			w,
			http.StatusBadRequest,
			"invalid source or target connection ID",
		)
		return
	}

	// Step 3: Generate migration plan.
	plan, err := h.service.Generate(
		r.Context(),
		req.SourceConnectionID,
		req.TargetConnectionID,
	)

	if err != nil {
		log.Printf("migration plan generation error: %v", err)

		switch {
		case errors.Is(err, service.ErrInvalidInput):
			writeError(
				w,
				http.StatusBadRequest,
				"invalid migration plan request",
			)

		case errors.Is(err, connection.ErrNotFound):
			writeError(
				w,
				http.StatusNotFound,
				"connection not found",
			)

		case errors.Is(err, service.ErrConnectionFailed),
			errors.Is(err, service.ErrSchemaDiscoveryFailed):
			writeError(
				w,
				http.StatusBadGateway,
				"unable to discover database schema",
			)

		default:
			writeError(
				w,
				http.StatusInternalServerError,
				"migration plan generation failed",
			)
		}

		return
	}

	// Step 4: Return generated plan.
	writeJSON(w, http.StatusOK, plan)
}
