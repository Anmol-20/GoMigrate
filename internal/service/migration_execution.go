package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Anmol-20/gomigrate/internal/mysqlclient"

	"github.com/Anmol-20/gomigrate/internal/migrationexecution"
	"github.com/Anmol-20/gomigrate/internal/migrationplan"
)

type ExecutionPlanRepository interface {
	GetByID(ctx context.Context, id int64) (*migrationplan.Plan, error)
}

type ExecutionRepository interface {
	Create(ctx context.Context, execution *migrationexecution.Execution) error
	GetByID(ctx context.Context, id int64) (*migrationexecution.Execution, error)
	TransitionExecution(ctx context.Context, id int64, from, to, message string) error
	TransitionOperation(ctx context.Context, executionID int64, operationID int, from, to, message string) error
	ListStaleRunning(ctx context.Context, olderThanMinutes int) ([]migrationexecution.Execution, error)
}

type ValidationDatabaseProvider interface {
	OpenValidationDatabase(ctx context.Context, targetConnectionID int64) (*sql.DB, error)
}

type MigrationExecutionService struct {
	plans      ExecutionPlanRepository
	executions ExecutionRepository
	databases  ValidationDatabaseProvider
}

func NewMigrationExecutionService(plans ExecutionPlanRepository, executions ExecutionRepository, databases ValidationDatabaseProvider) *MigrationExecutionService {
	return &MigrationExecutionService{plans: plans, executions: executions, databases: databases}
}

// Execute runs a persisted, checksum-verified plan sequentially against the
// dedicated validation database. MySQL DDL is not treated as transactional.
func (s *MigrationExecutionService) Execute(ctx context.Context, planID int64) (*migrationexecution.Execution, error) {
	if planID <= 0 {
		return nil, fmt.Errorf("%w: invalid migration plan ID", ErrInvalidInput)
	}
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("load migration plan: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate migration plan: %w", err)
	}
	if plan.TargetConnectionID <= 0 {
		return nil, fmt.Errorf("migration plan has no target connection")
	}

	execution := &migrationexecution.Execution{PlanID: plan.ID}
	if err := s.executions.Create(ctx, execution); err != nil {
		return nil, fmt.Errorf("create execution record: %w", err)
	}
	if err := s.executions.TransitionExecution(ctx, execution.ID, migrationexecution.ExecutionPending, migrationexecution.ExecutionRunning, ""); err != nil {
		return nil, fmt.Errorf("start execution record: %w", err)
	}

	db, err := s.databases.OpenValidationDatabase(ctx, plan.TargetConnectionID)
	if err != nil {
		_ = s.executions.TransitionExecution(ctx, execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionFailed, err.Error())
		return s.executions.GetByID(ctx, execution.ID)
	}
	defer db.Close()

	// MySQL advisory locks are session-scoped. Pin execution to one connection
	// so the lock and DDL statements use the same server session.
	conn, err := db.Conn(ctx)
	if err != nil {
		message := fmt.Sprintf("acquire validation DB connection: %v", err)
		_ = s.executions.TransitionExecution(context.Background(), execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionFailed, message)
		return s.executions.GetByID(context.Background(), execution.ID)
	}
	defer conn.Close()

	const validationLockName = "gomigrate:execution:gomigrate_execution_test"
	var lockAcquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", validationLockName).Scan(&lockAcquired); err != nil || !lockAcquired.Valid || lockAcquired.Int64 != 1 {
		message := "validation database is busy; another migration execution holds the lock"
		if err != nil {
			message = fmt.Sprintf("acquire validation database lock: %v", err)
		}
		_ = s.executions.TransitionExecution(context.Background(), execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionFailed, message)
		return s.executions.GetByID(context.Background(), execution.ID)
	}
	defer func() {
		var released sql.NullInt64
		_ = conn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", validationLockName).Scan(&released)
	}()

	var selectedDatabase string
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&selectedDatabase); err != nil || selectedDatabase != ExecutionDatabaseName {
		message := fmt.Sprintf("refusing execution: validation connection selected database %q", selectedDatabase)
		if err != nil {
			message = fmt.Sprintf("verify validation database on execution connection: %v", err)
		}
		_ = s.executions.TransitionExecution(context.Background(), execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionFailed, message)
		return s.executions.GetByID(context.Background(), execution.ID)
	}

	for _, op := range plan.Operations {
		if err := ctx.Err(); err != nil {
			_ = s.executions.TransitionExecution(context.Background(), execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionFailed, err.Error())
			return s.executions.GetByID(context.Background(), execution.ID)
		}
		if err := s.executions.TransitionOperation(ctx, execution.ID, op.ID, migrationexecution.OperationPending, migrationexecution.OperationRunning, ""); err != nil {
			_ = s.executions.TransitionExecution(ctx, execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionFailed, err.Error())
			return nil, fmt.Errorf("start operation %d: %w", op.ID, err)
		}
		if _, err := conn.ExecContext(ctx, op.SQL); err != nil {
			background := context.Background()
			_ = s.executions.TransitionOperation(background, execution.ID, op.ID, migrationexecution.OperationRunning, migrationexecution.OperationFailed, err.Error())
			for _, remaining := range plan.Operations {
				if remaining.ID > op.ID {
					_ = s.executions.TransitionOperation(background, execution.ID, remaining.ID, migrationexecution.OperationPending, migrationexecution.OperationSkipped, "Skipped because an earlier operation failed")
				}
			}
			_ = s.executions.TransitionExecution(background, execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionFailed, fmt.Sprintf("operation %d failed: %v", op.ID, err))
			return s.executions.GetByID(background, execution.ID)
		}
		if err := s.executions.TransitionOperation(ctx, execution.ID, op.ID, migrationexecution.OperationRunning, migrationexecution.OperationCompleted, ""); err != nil {
			_ = s.executions.TransitionExecution(context.Background(), execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionFailed, err.Error())
			return nil, fmt.Errorf("record completion of operation %d: %w", op.ID, err)
		}
	}
	if err := s.executions.TransitionExecution(ctx, execution.ID, migrationexecution.ExecutionRunning, migrationexecution.ExecutionCompleted, ""); err != nil {
		return nil, fmt.Errorf("complete execution record: %w", err)
	}
	return s.executions.GetByID(ctx, execution.ID)
}

func (s *MigrationExecutionService) GetByID(ctx context.Context, id int64) (*migrationexecution.Execution, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: invalid execution ID", ErrInvalidInput)
	}
	return s.executions.GetByID(ctx, id)
}

// StaleExecutionReport provides read-only diagnostics for executions that may
// have been interrupted. It deliberately makes no recovery or retry decisions.
func (s *MigrationExecutionService) StaleExecutionReport(ctx context.Context, olderThanMinutes int) ([]migrationexecution.Execution, error) {
	if olderThanMinutes < 1 || olderThanMinutes > 10080 {
		return nil, fmt.Errorf("older-than threshold must be between 1 and 10080 minutes")
	}
	stale, err := s.executions.ListStaleRunning(ctx, olderThanMinutes)
	if err != nil {
		return nil, fmt.Errorf("list stale executions: %w", err)
	}
	reports := make([]migrationexecution.Execution, 0, len(stale))
	for _, item := range stale {
		full, err := s.executions.GetByID(ctx, item.ID)
		if err != nil {
			return nil, fmt.Errorf("load stale execution %d: %w", item.ID, err)
		}
		reports = append(reports, *full)
	}
	return reports, nil
}

// ReconciliationReport is a read-only comparison between execution history
// and the current validation database schema.
type ReconciliationReport struct {
	ExecutionID        int64                     `json:"execution_id"`
	PlanID             int64                     `json:"plan_id"`
	ValidationDatabase string                    `json:"validation_database"`
	ExecutionStatus    string                    `json:"execution_status"`
	Operations         []OperationReconciliation `json:"operations"`
	Warning            string                    `json:"warning"`
}

type OperationReconciliation struct {
	OperationID    int    `json:"operation_id"`
	Type           string `json:"type"`
	Table          string `json:"table"`
	RecordedStatus string `json:"recorded_status"`
	SchemaFinding  string `json:"schema_finding"`
	Explanation    string `json:"explanation"`
}

// ReconcileExecution inspects only gomigrate_execution_test. CREATE_TABLE can be
// verified by table existence; other operation types remain ambiguous until
// object-level verification is implemented. No SQL is executed or retried.
func (s *MigrationExecutionService) ReconcileExecution(ctx context.Context, executionID int64) (*ReconciliationReport, error) {
	if executionID <= 0 {
		return nil, fmt.Errorf("%w: invalid execution ID", ErrInvalidInput)
	}
	execution, err := s.executions.GetByID(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("load execution: %w", err)
	}
	plan, err := s.plans.GetByID(ctx, execution.PlanID)
	if err != nil {
		return nil, fmt.Errorf("load persisted plan: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate persisted plan: %w", err)
	}
	db, err := s.databases.OpenValidationDatabase(ctx, plan.TargetConnectionID)
	if err != nil {
		return nil, fmt.Errorf("open validation database: %w", err)
	}
	defer db.Close()
	var selected string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&selected); err != nil {
		return nil, fmt.Errorf("verify selected database: %w", err)
	}
	if selected != ExecutionDatabaseName {
		return nil, fmt.Errorf(
			"refusing reconciliation: selected database %q is not %q",
			selected,
			ExecutionDatabaseName,
		)
	}
	schema, err := mysqlclient.DiscoverSchema(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("discover validation schema: %w", err)
	}
	tables := make(map[string]bool, len(schema.Tables))
	for _, table := range schema.Tables {
		tables[strings.ToLower(table.Name)] = true
	}
	statuses := make(map[int]string, len(execution.Operations))
	for _, op := range execution.Operations {
		statuses[op.OperationID] = op.Status
	}
	report := &ReconciliationReport{ExecutionID: execution.ID, PlanID: execution.PlanID, ValidationDatabase: selected, ExecutionStatus: execution.Status, Operations: make([]OperationReconciliation, 0, len(plan.Operations)), Warning: "Read-only diagnostic. Only CREATE_TABLE table existence is verified in this batch. Other schema changes are AMBIGUOUS. No SQL is executed, retried, or rolled back."}
	for _, op := range plan.Operations {
		item := OperationReconciliation{OperationID: op.ID, Type: op.Type, Table: op.Table, RecordedStatus: statuses[op.ID], SchemaFinding: "AMBIGUOUS", Explanation: "This operation type requires object-level schema verification, which is not included in this batch."}
		if op.Type == "CREATE_TABLE" {
			if tables[strings.ToLower(op.Table)] {
				item.SchemaFinding = "PRESENT"
				item.Explanation = "The table exists in the validation database; this alone does not prove every planned table definition detail."
			} else {
				item.SchemaFinding = "ABSENT"
				item.Explanation = "The table does not exist in the validation database."
			}
		}
		report.Operations = append(report.Operations, item)
	}
	return report, nil
}
