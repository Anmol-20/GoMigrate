package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Anmol-20/gomigrate/internal/migrationexecution"
)

// MigrationExecutionRepository defines persistence operations used by the
// execution service. Status updates are conditional to prevent stale workers
// from silently overwriting a newer state.
type MigrationExecutionRepository interface {
	Create(ctx context.Context, execution *migrationexecution.Execution) error
	GetByID(ctx context.Context, id int64) (*migrationexecution.Execution, error)
	TransitionExecution(ctx context.Context, id int64, from, to, message string) error
	TransitionOperation(ctx context.Context, executionID int64, operationID int, from, to, message string) error
}

type MySQLMigrationExecutionRepository struct{ db *sql.DB }

func NewMySQLMigrationExecutionRepository(db *sql.DB) *MySQLMigrationExecutionRepository {
	return &MySQLMigrationExecutionRepository{db: db}
}

func (r *MySQLMigrationExecutionRepository) Create(ctx context.Context, execution *migrationexecution.Execution) error {
	if execution == nil || execution.PlanID <= 0 {
		return fmt.Errorf("valid execution and plan ID are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create execution: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `INSERT INTO migration_executions (plan_id, status) VALUES (?, ?)`, execution.PlanID, migrationexecution.ExecutionPending)
	if err != nil {
		return fmt.Errorf("insert migration execution: %w", err)
	}
	execution.ID, err = result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get migration execution ID: %w", err)
	}

	var planSnapshot struct {
		Operations []struct {
			ID    int    `json:"id"`
			Type  string `json:"type"`
			Table string `json:"table"`
		} `json:"operations"`
	}
	var payload []byte
	if err := tx.QueryRowContext(ctx, `SELECT plan_json FROM migration_plans WHERE id = ?`, execution.PlanID).Scan(&payload); err != nil {
		return fmt.Errorf("load migration plan: %w", err)
	}
	if err := json.Unmarshal(payload, &planSnapshot); err != nil {
		return fmt.Errorf("decode migration plan operations: %w", err)
	}
	operations := planSnapshot.Operations
	for _, op := range operations {
		if _, err := tx.ExecContext(ctx, `INSERT INTO migration_execution_operations (execution_id, operation_id, operation_type, table_name, status) VALUES (?, ?, ?, ?, ?)`, execution.ID, op.ID, op.Type, op.Table, migrationexecution.OperationPending); err != nil {
			return fmt.Errorf("insert execution operation %d: %w", op.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create execution: %w", err)
	}
	execution.Status = migrationexecution.ExecutionPending
	return nil
}

func (r *MySQLMigrationExecutionRepository) GetByID(
	ctx context.Context,
	id int64,
) (*migrationexecution.Execution, error) {
	if id <= 0 {
		return nil, fmt.Errorf("execution ID must be positive")
	}

	var e migrationexecution.Execution
	var errorMessage sql.NullString

	err := r.db.QueryRowContext(ctx, `
        SELECT
            id,
            plan_id,
            status,
            started_at,
            completed_at,
            error_message,
            created_at,
            updated_at
        FROM migration_executions
        WHERE id = ?
    `, id).Scan(
		&e.ID,
		&e.PlanID,
		&e.Status,
		&e.StartedAt,
		&e.CompletedAt,
		&errorMessage,
		&e.CreatedAt,
		&e.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if errorMessage.Valid {
		e.ErrorMessage = errorMessage.String
	}

	rows, err := r.db.QueryContext(ctx, `
        SELECT
            id,
            execution_id,
            operation_id,
            operation_type,
            table_name,
            status,
            started_at,
            completed_at,
            error_message,
            created_at,
            updated_at
        FROM migration_execution_operations
        WHERE execution_id = ?
        ORDER BY operation_id
    `, id)
	if err != nil {
		return nil, fmt.Errorf("list execution operations: %w", err)
	}
	defer rows.Close()

	e.Operations = make([]migrationexecution.Operation, 0)

	for rows.Next() {
		var op migrationexecution.Operation
		var operationErrorMessage sql.NullString

		if err := rows.Scan(
			&op.ID,
			&op.ExecutionID,
			&op.OperationID,
			&op.Type,
			&op.Table,
			&op.Status,
			&op.StartedAt,
			&op.CompletedAt,
			&operationErrorMessage,
			&op.CreatedAt,
			&op.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if operationErrorMessage.Valid {
			op.ErrorMessage = operationErrorMessage.String
		}

		e.Operations = append(e.Operations, op)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &e, nil
}

func (r *MySQLMigrationExecutionRepository) TransitionExecution(
	ctx context.Context,
	id int64,
	from, to, message string,
) error {
	if err := migrationexecution.ValidExecutionTransition(from, to); err != nil {
		return err
	}

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE migration_executions
		SET status = ?,
			error_message = NULLIF(?, ''),
			started_at = CASE WHEN ? = 'RUNNING' THEN CURRENT_TIMESTAMP ELSE started_at END,
			completed_at = CASE WHEN ? IN ('COMPLETED', 'FAILED', 'CANCELLED') THEN CURRENT_TIMESTAMP ELSE completed_at END
		WHERE id = ? AND status = ?`,
		to,
		message,
		to,
		to,
		id,
		from,
	)
	if err != nil {
		return fmt.Errorf("update execution status: %w", err)
	}

	return requireOneRow(result, "execution status changed or execution not found")
}

func (r *MySQLMigrationExecutionRepository) TransitionOperation(ctx context.Context, executionID int64, operationID int, from, to, message string) error {
	if err := migrationexecution.ValidOperationTransition(from, to); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE migration_execution_operations SET status = ?, error_message = NULLIF(?, ''), started_at = CASE WHEN ? = 'RUNNING' THEN CURRENT_TIMESTAMP ELSE started_at END, completed_at = CASE WHEN ? IN ('COMPLETED', 'FAILED', 'SKIPPED') THEN CURRENT_TIMESTAMP ELSE completed_at END WHERE execution_id = ? AND operation_id = ? AND status = ?`, to, message, to, to, executionID, operationID, from)
	if err != nil {
		return fmt.Errorf("update operation status: %w", err)
	}
	return requireOneRow(result, "operation status changed or operation not found")
}

// ListStaleRunning returns RUNNING executions whose updated_at timestamp is older
// than the supplied number of minutes. It is diagnostic-only and does not mutate rows.
func (r *MySQLMigrationExecutionRepository) ListStaleRunning(ctx context.Context, olderThanMinutes int) ([]migrationexecution.Execution, error) {
	if olderThanMinutes < 1 {
		return nil, fmt.Errorf("older-than threshold must be at least one minute")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, plan_id, status, started_at, completed_at, error_message, created_at, updated_at FROM migration_executions WHERE status = 'RUNNING' AND updated_at < DATE_SUB(CURRENT_TIMESTAMP, INTERVAL ? MINUTE) ORDER BY updated_at ASC`, olderThanMinutes)
	if err != nil {
		return nil, fmt.Errorf("list stale executions: %w", err)
	}
	defer rows.Close()
	result := make([]migrationexecution.Execution, 0)
	for rows.Next() {
		var e migrationexecution.Execution
		if err := rows.Scan(&e.ID, &e.PlanID, &e.Status, &e.StartedAt, &e.CompletedAt, &e.ErrorMessage, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func requireOneRow(result sql.Result, message string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("%s", message)
	}
	return nil
}
