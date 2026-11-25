package migrationplan

import (
	"fmt"
	"strings"
)

func classifyOperation(operation *Operation) {
	if operation == nil {
		return
	}

	operation.RiskLevel = RiskLow
	operation.RequiresReview = false
	operation.RiskReason = ""
	operation.Warnings = nil
	operation.RollbackAssessment = RollbackManual
	operation.RollbackReason = ""

	switch operation.Type {
	case "CREATE_TABLE":
		operation.RiskLevel = RiskMedium
		operation.RiskReason = "Creates a new table and its initial structure."
		operation.RollbackReason = "Dropping the table could permanently delete data created after migration."

	case "ADD_COLUMN":
		operation.RiskLevel = RiskMedium
		operation.RiskReason = "Adds a column; review defaults, nullability and storage impact."
		operation.RollbackReason = "Dropping the column could permanently delete values written after migration."

	case "MODIFY_COLUMN":
		operation.RiskLevel = RiskHigh
		operation.RequiresReview = true
		operation.RiskReason = "Changing a column may cause data conversion, truncation or constraint failures."
		operation.RollbackReason = "The previous column definition and any converted data cannot be reliably reconstructed from the source schema alone."
		operation.Warnings = append(
			operation.Warnings,
			"Review existing data compatibility before applying this column modification.",
		)

	case "ADD_INDEX":
		operation.RiskLevel = RiskMedium
		operation.RiskReason = "Creates an index; this may consume storage and affect write performance."
		operation.RollbackAssessment = RollbackSchemaOnly
		operation.RollbackReason = "The index can generally be removed with DROP INDEX, subject to dependency and workload review."

	case "ADD_FOREIGN_KEY":
		operation.RiskLevel = RiskHigh
		operation.RequiresReview = true
		operation.RiskReason = "Existing rows must satisfy the new foreign key constraint."
		operation.RollbackAssessment = RollbackSchemaOnly
		operation.RollbackReason = "The constraint can generally be removed with ALTER TABLE DROP FOREIGN KEY, subject to dependency review."
		operation.Warnings = append(
			operation.Warnings,
			"Verify that existing records satisfy the referenced constraint.",
		)

	default:
		operation.RiskLevel = RiskCritical
		operation.RequiresReview = true
		operation.RiskReason = "Unsupported operation type."
		operation.RollbackReason = "Unsupported operation; manual rollback analysis is required."
	}
}

func classifyPlan(plan *Plan) {
	if plan == nil {
		return
	}

	for i := range plan.Operations {
		classifyOperation(&plan.Operations[i])
	}
}

func validateOperationSQL(operation Operation) error {
	sql := strings.TrimSpace(operation.SQL)
	upper := strings.ToUpper(sql)

	// Generated operations must contain exactly one statement.
	if strings.Contains(sql, ";") {
		return fmt.Errorf("multiple SQL statements or statement terminator are not allowed")
	}

	// Generated SQL must not contain SQL comments.
	if strings.Contains(sql, "--") ||
		strings.Contains(sql, "/*") ||
		strings.Contains(sql, "*/") ||
		strings.Contains(sql, "#") {
		return fmt.Errorf("SQL comments are not allowed in generated operations")
	}

	// Reject destructive SQL statements by checking the leading SQL keyword.
	// Do not search for destructive keywords anywhere in the SQL because
	// legitimate clauses such as ON DELETE CASCADE are not DELETE statements.
	fields := strings.Fields(upper)

	if len(fields) > 0 {
		destructiveStatements := map[string]bool{
			"DROP":     true,
			"TRUNCATE": true,
			"DELETE":   true,
			"REPLACE":  true,
		}

		if destructiveStatements[fields[0]] {
			return fmt.Errorf(
				"destructive SQL statement %q is not allowed",
				fields[0],
			)
		}
	}

	var expectedPrefix string

	switch operation.Type {
	case "CREATE_TABLE":
		expectedPrefix = "CREATE TABLE "
	case "ADD_COLUMN":
		expectedPrefix = "ALTER TABLE "
		if !strings.Contains(upper, " ADD COLUMN ") {
			return fmt.Errorf("ADD_COLUMN operation must contain ADD COLUMN")
		}
	case "MODIFY_COLUMN":
		expectedPrefix = "ALTER TABLE "
		if !strings.Contains(upper, " MODIFY COLUMN ") {
			return fmt.Errorf("MODIFY_COLUMN operation must contain MODIFY COLUMN")
		}
	case "ADD_INDEX":
		expectedPrefix = "CREATE "
		if !strings.Contains(upper, " INDEX ") {
			return fmt.Errorf("ADD_INDEX operation must contain an index creation statement")
		}
	case "ADD_FOREIGN_KEY":
		expectedPrefix = "ALTER TABLE "
		if !strings.Contains(upper, " ADD CONSTRAINT ") ||
			!strings.Contains(upper, " FOREIGN KEY ") {
			return fmt.Errorf("ADD_FOREIGN_KEY operation must contain a foreign key constraint")
		}
	default:
		return fmt.Errorf("unsupported operation type %q", operation.Type)
	}

	if !strings.HasPrefix(strings.ToUpper(sql), expectedPrefix) {
		return fmt.Errorf(
			"SQL does not match expected statement type for %s",
			operation.Type,
		)
	}

	return nil
}
