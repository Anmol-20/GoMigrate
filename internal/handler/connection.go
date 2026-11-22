package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Anmol-20/gomigrate/internal/connection"
	"github.com/Anmol-20/gomigrate/internal/service"
)

type ConnectionHandler struct {
	service *service.ConnectionService
}

func NewConnectionHandler(
	svc *service.ConnectionService,
) *ConnectionHandler {

	return &ConnectionHandler{
		service: svc,
	}
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	data any,
) {

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		return
	}
}

func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {

	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func (h *ConnectionHandler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req service.CreateConnectionRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"Invalid request body",
		)
		return
	}

	// Reject trailing JSON values.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(
			w,
			http.StatusBadRequest,
			"Request must contain one JSON object",
		)
		return
	}

	response, err := h.service.Create(r.Context(), req)

	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}

		writeError(
			w,
			http.StatusInternalServerError,
			"Failed to create connection",
		)
		return
	}

	writeJSON(
		w,
		http.StatusCreated,
		response,
	)
}

func (h *ConnectionHandler) GetByID(
	w http.ResponseWriter,
	r *http.Request,
) {

	id, err := strconv.ParseInt(
		r.PathValue("id"),
		10,
		64,
	)

	if err != nil || id <= 0 {
		writeError(
			w,
			http.StatusBadRequest,
			"Invalid connection ID",
		)
		return
	}

	response, err := h.service.GetByID(
		r.Context(),
		id,
	)

	if err != nil {
		if errors.Is(err, connection.ErrNotFound) {
			writeError(
				w,
				http.StatusNotFound,
				"Connection not found",
			)
			return
		}

		if errors.Is(err, service.ErrInvalidInput) {
			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}

		writeError(
			w,
			http.StatusInternalServerError,
			"Failed to retrieve connection",
		)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *ConnectionHandler) List(
	w http.ResponseWriter,
	r *http.Request,
) {

	responses, err := h.service.List(r.Context())

	if err != nil {
		writeError(
			w,
			http.StatusInternalServerError,
			"Failed to list connections",
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]any{
			"connections": responses,
		},
	)
}

func (h *ConnectionHandler) Delete(
	w http.ResponseWriter,
	r *http.Request,
) {

	id, err := strconv.ParseInt(
		r.PathValue("id"),
		10,
		64,
	)

	if err != nil || id <= 0 {
		writeError(
			w,
			http.StatusBadRequest,
			"Invalid connection ID",
		)
		return
	}

	err = h.service.Delete(r.Context(), id)

	if err != nil {
		if errors.Is(err, connection.ErrNotFound) {
			writeError(
				w,
				http.StatusNotFound,
				"Connection not found",
			)
			return
		}

		if errors.Is(err, service.ErrInvalidInput) {
			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}

		writeError(
			w,
			http.StatusInternalServerError,
			"Failed to delete connection",
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]string{
			"message": "Connection deleted successfully",
		},
	)
}
