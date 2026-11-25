package migrationplan

import (
	"fmt"
	"time"
)

type Plan struct {
	ID                 int64       `json:"id,omitempty"`
	SourceConnectionID int64       `json:"source_connection_id,omitempty"`
	TargetConnectionID int64       `json:"target_connection_id,omitempty"`
	Checksum           string      `json:"checksum,omitempty"`
	CreatedAt          *time.Time  `json:"created_at,omitempty"`
	SourceDatabase     string      `json:"source_database"`
	TargetDatabase     string      `json:"target_database"`
	Operations         []Operation `json:"operations"`
	Warnings           []string    `json:"warnings"`
	Summary            PlanSummary `json:"summary"`
}

type Operation struct {
	ID                 int      `json:"id"`
	Type               string   `json:"type"`
	Table              string   `json:"table"`
	Description        string   `json:"description"`
	SQL                string   `json:"sql"`
	RiskLevel          string   `json:"risk_level"`
	RequiresReview     bool     `json:"requires_review"`
	RiskReason         string   `json:"risk_reason,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
	RollbackAssessment string   `json:"rollback_assessment"`
	RollbackReason     string   `json:"rollback_reason,omitempty"`
}

type PlanSummary struct {
	TotalOperations              int `json:"total_operations"`
	LowRiskOperations            int `json:"low_risk_operations"`
	MediumRiskOperations         int `json:"medium_risk_operations"`
	HighRiskOperations           int `json:"high_risk_operations"`
	CriticalRiskOperations       int `json:"critical_risk_operations"`
	OperationsRequiringReview    int `json:"operations_requiring_review"`
	SchemaOnlyRollbackOperations int `json:"schema_only_rollback_operations"`
	ManualRollbackOperations     int `json:"manual_rollback_operations"`
}

const (
	RiskLow      = "LOW"
	RiskMedium   = "MEDIUM"
	RiskHigh     = "HIGH"
	RiskCritical = "CRITICAL"
)

const (
	RollbackSchemaOnly = "SCHEMA_ONLY"
	RollbackManual     = "MANUAL"
)

var supportedOperationTypes = map[string]bool{
	"CREATE_TABLE":    true,
	"ADD_COLUMN":      true,
	"MODIFY_COLUMN":   true,
	"ADD_INDEX":       true,
	"ADD_FOREIGN_KEY": true,
}

// Validate checks the structural and safety properties of a migration plan.
// It does not execute SQL or guarantee that MySQL will accept the statements.
func (p *Plan) Validate() error {
	if p == nil {
		return fmt.Errorf("migration plan cannot be nil")
	}

	if p.SourceDatabase == "" || p.TargetDatabase == "" {
		return fmt.Errorf("source and target database names are required")
	}

	if p.SourceDatabase == p.TargetDatabase {
		return fmt.Errorf("source and target databases must be different")
	}

	for i, operation := range p.Operations {
		expectedID := i + 1

		if operation.ID != expectedID {
			return fmt.Errorf(
				"operation at position %d has ID %d; expected %d",
				i, operation.ID, expectedID,
			)
		}

		if !supportedOperationTypes[operation.Type] {
			return fmt.Errorf(
				"operation %d has unsupported type %q",
				operation.ID, operation.Type,
			)
		}

		if operation.Table == "" {
			return fmt.Errorf("operation %d has no table name", operation.ID)
		}

		if operation.SQL == "" {
			return fmt.Errorf("operation %d has empty SQL", operation.ID)
		}

		if err := validateOperationSQL(operation); err != nil {
			return fmt.Errorf("operation %d: %w", operation.ID, err)
		}

		if !validRiskLevel(operation.RiskLevel) {
			return fmt.Errorf(
				"operation %d has invalid risk level %q",
				operation.ID, operation.RiskLevel,
			)
		}

		// if operation.RollbackAssessment != RollbackSchemaOnly &&
		// 	operation.RollbackAssessment != RollbackManual {
		// 	return fmt.Errorf(
		// 		"operation %d has invalid rollback assessment %q",
		// 		operation.ID, operation.RollbackAssessment,
		// 	)
		// }
	}

	return nil
}

func validRiskLevel(level string) bool {
	switch level {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
		return true
	default:
		return false
	}
}
