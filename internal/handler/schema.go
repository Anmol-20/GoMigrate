package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Anmol-20/gomigrate/internal/connection"
	"github.com/Anmol-20/gomigrate/internal/service"
)

type SchemaComparisonHandler struct {
	service *service.SchemaComparisonService
}

func NewSchemaComparisonHandler(
	svc *service.SchemaComparisonService,
) *SchemaComparisonHandler {

	return &SchemaComparisonHandler{
		service: svc,
	}
}

type CompareSchemaRequest struct {
	SourceConnectionID int64 `json:"source_connection_id"`
	TargetConnectionID int64 `json:"target_connection_id"`
}

func (h *SchemaComparisonHandler) Compare(
	w http.ResponseWriter,
	r *http.Request,
) {

	// Step 1: Decode request body.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req CompareSchemaRequest

	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Reject additional JSON values after the first object.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request must contain one JSON object")
		return
	}

	// Step 2: Validate IDs.
	if req.SourceConnectionID <= 0 ||
		req.TargetConnectionID <= 0 ||
		req.SourceConnectionID == req.TargetConnectionID {

		writeError(w, http.StatusBadRequest, "invalid source or target connection ID")
		return
	}

	// Step 3: Compare schemas.
	result, err := h.service.Compare(
		r.Context(),
		req.SourceConnectionID,
		req.TargetConnectionID,
	)

	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "invalid comparison request")

		case errors.Is(err, connection.ErrNotFound):
			writeError(w, http.StatusNotFound, "connection not found")

		case errors.Is(err, service.ErrConnectionFailed),
			errors.Is(err, service.ErrSchemaDiscoveryFailed):
			writeError(w, http.StatusBadGateway, "unable to discover database schema")

		default:
			writeError(w, http.StatusInternalServerError, "schema comparison failed")
		}

		return
	}

	// Step 4: Return comparison result.
	writeJSON(w, http.StatusOK, result)
}
