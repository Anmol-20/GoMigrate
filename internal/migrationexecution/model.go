// Package migrationexecution contains the persistent execution domain model.
// SQL execution is intentionally implemented in a later layer.
package migrationexecution

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidTransition          = errors.New("invalid migration execution status transition")
	ErrInvalidOperationTransition = errors.New("invalid migration operation status transition")
)

const (
	ExecutionPending   = "PENDING"
	ExecutionRunning   = "RUNNING"
	ExecutionCompleted = "COMPLETED"
	ExecutionFailed    = "FAILED"
	ExecutionCancelled = "CANCELLED"

	OperationPending   = "PENDING"
	OperationRunning   = "RUNNING"
	OperationCompleted = "COMPLETED"
	OperationFailed    = "FAILED"
	OperationSkipped   = "SKIPPED"
)

type Execution struct {
	ID           int64       `json:"id"`
	PlanID       int64       `json:"plan_id"`
	Status       string      `json:"status"`
	StartedAt    *time.Time  `json:"started_at,omitempty"`
	CompletedAt  *time.Time  `json:"completed_at,omitempty"`
	ErrorMessage string      `json:"error_message,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	Operations   []Operation `json:"operations"`
}

type Operation struct {
	ID           int64      `json:"id"`
	ExecutionID  int64      `json:"execution_id"`
	OperationID  int        `json:"operation_id"`
	Type         string     `json:"type"`
	Table        string     `json:"table"`
	Status       string     `json:"status"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func ValidExecutionTransition(from, to string) error {
	allowed := map[string]map[string]bool{
		ExecutionPending: {ExecutionRunning: true, ExecutionCancelled: true},
		ExecutionRunning: {ExecutionCompleted: true, ExecutionFailed: true, ExecutionCancelled: true},
	}
	if !allowed[from][to] {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}

func ValidOperationTransition(from, to string) error {
	allowed := map[string]map[string]bool{
		OperationPending: {OperationRunning: true, OperationSkipped: true},
		OperationRunning: {OperationCompleted: true, OperationFailed: true},
	}
	if !allowed[from][to] {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidOperationTransition, from, to)
	}
	return nil
}
