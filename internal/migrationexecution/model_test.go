package migrationexecution

import (
	"errors"
	"testing"
)

func TestValidExecutionTransition(t *testing.T) {
	tests := []struct {
		name, from, to string
		wantErr        bool
	}{
		{"pending to running", ExecutionPending, ExecutionRunning, false},
		{"pending to cancelled", ExecutionPending, ExecutionCancelled, false},
		{"running to completed", ExecutionRunning, ExecutionCompleted, false},
		{"running to failed", ExecutionRunning, ExecutionFailed, false},
		{"completed cannot restart", ExecutionCompleted, ExecutionRunning, true},
		{"failed cannot complete directly", ExecutionFailed, ExecutionCompleted, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidExecutionTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("expected ErrInvalidTransition, got %v", err)
			}
		})
	}
}

func TestValidOperationTransition(t *testing.T) {
	tests := []struct {
		name, from, to string
		wantErr        bool
	}{
		{"pending to running", OperationPending, OperationRunning, false},
		{"pending to skipped", OperationPending, OperationSkipped, false},
		{"running to completed", OperationRunning, OperationCompleted, false},
		{"running to failed", OperationRunning, OperationFailed, false},
		{"completed cannot rerun", OperationCompleted, OperationRunning, true},
		{"failed cannot complete", OperationFailed, OperationCompleted, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidOperationTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidOperationTransition) {
				t.Fatalf("expected ErrInvalidOperationTransition, got %v", err)
			}
		})
	}
}
