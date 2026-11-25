package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/Anmol-20/gomigrate/internal/connection"
	"github.com/Anmol-20/gomigrate/internal/service"
)

type MigrationExecutionHandler struct {
	service *service.MigrationExecutionService
}

func NewMigrationExecutionHandler(svc *service.MigrationExecutionService) *MigrationExecutionHandler {
	return &MigrationExecutionHandler{service: svc}
}

func (h *MigrationExecutionHandler) Execute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid migration plan ID")
		return
	}
	execution, err := h.service.Execute(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "Invalid migration plan ID")
			return
		}
		if errors.Is(err, connection.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Target connection not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "Migration execution could not be started")
		return
	}
	status := http.StatusAccepted
	if execution.Status == "FAILED" {
		status = http.StatusConflict
	}
	writeJSON(w, status, execution)
}

func (h *MigrationExecutionHandler) GetByID(
	w http.ResponseWriter,
	r *http.Request,
) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid migration execution ID")
		return
	}

	execution, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Migration execution not found")
			return
		}

		writeError(w, http.StatusInternalServerError, "Could not retrieve migration execution")
		return
	}

	writeJSON(w, http.StatusOK, execution)
}

// ListStale is a read-only diagnostic endpoint. Default threshold is 30 minutes.
func (h *MigrationExecutionHandler) ListStale(w http.ResponseWriter, r *http.Request) {
	threshold := 30
	if raw := r.URL.Query().Get("older_than_minutes"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 10080 {
			writeError(w, http.StatusBadRequest, "older_than_minutes must be between 1 and 10080")
			return
		}
		threshold = parsed
	}
	executions, err := h.service.StaleExecutionReport(r.Context(), threshold)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not retrieve stale execution report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"older_than_minutes": threshold,
		"count":              len(executions),
		"executions":         executions,
		"warning":            "Diagnostic only. RUNNING operation outcomes are not inferred and no SQL is rerun.",
	})
}

// Reconcile returns a read-only schema reconciliation report for an execution.
func (h *MigrationExecutionHandler) Reconcile(
	w http.ResponseWriter,
	r *http.Request,
) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid migration execution ID")
		return
	}

	report, err := h.service.ReconcileExecution(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "Invalid migration execution ID")
			return
		}

		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Migration execution or associated plan not found")
			return
		}

		writeError(w, http.StatusInternalServerError, "Migration execution could not be reconciled")
		return
	}

	writeJSON(w, http.StatusOK, report)
}
