package service

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/Anmol-20/gomigrate/internal/migrationexecution"
	"github.com/Anmol-20/gomigrate/internal/migrationplan"
)

// isolationPlanRepository provides a migration plan for testing.
type isolationPlanRepository struct {
	plan *migrationplan.Plan
}

func (r *isolationPlanRepository) GetByID(
	ctx context.Context,
	id int64,
) (*migrationplan.Plan, error) {
	if r.plan == nil || r.plan.ID != id {
		return nil, fmt.Errorf("migration plan not found")
	}
	return r.plan, nil
}

// isolationExecutionRepository records execution state transitions.
type isolationExecutionRepository struct {
	execution            migrationexecution.Execution
	operationTransitions int
}

func (r *isolationExecutionRepository) Create(
	ctx context.Context,
	execution *migrationexecution.Execution,
) error {
	execution.ID = 100
	execution.Status = migrationexecution.ExecutionPending
	r.execution = *execution
	return nil
}

func (r *isolationExecutionRepository) GetByID(
	ctx context.Context,
	id int64,
) (*migrationexecution.Execution, error) {
	if r.execution.ID != id {
		return nil, fmt.Errorf("execution not found")
	}

	result := r.execution
	return &result, nil
}

func (r *isolationExecutionRepository) TransitionExecution(
	ctx context.Context,
	id int64,
	from, to, message string,
) error {
	if r.execution.ID != id {
		return fmt.Errorf("execution not found")
	}

	if r.execution.Status != from {
		return fmt.Errorf(
			"unexpected execution status: got %s, want %s",
			r.execution.Status,
			from,
		)
	}

	if err := migrationexecution.ValidExecutionTransition(from, to); err != nil {
		return err
	}

	r.execution.Status = to
	r.execution.ErrorMessage = message

	return nil
}

func (r *isolationExecutionRepository) TransitionOperation(
	ctx context.Context,
	executionID int64,
	operationID int,
	from, to, message string,
) error {
	r.operationTransitions++
	return nil
}

func (r *isolationExecutionRepository) ListStaleRunning(
	ctx context.Context,
	olderThanMinutes int,
) ([]migrationexecution.Execution, error) {
	return nil, nil
}

// isolationDatabaseProvider supplies a mocked database connection.
type isolationDatabaseProvider struct {
	db *sql.DB
}

func (p *isolationDatabaseProvider) OpenValidationDatabase(
	ctx context.Context,
	targetConnectionID int64,
) (*sql.DB, error) {
	if targetConnectionID <= 0 {
		return nil, fmt.Errorf("invalid target connection ID")
	}
	return p.db, nil
}

func TestExecuteRefusesUnexpectedDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create SQL mock: %v", err)
	}
	defer db.Close()

	const lockName = "gomigrate:execution:gomigrate_execution_test"

	// The execution service must acquire its advisory lock first.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, 0)")).
		WithArgs(lockName).
		WillReturnRows(
			sqlmock.NewRows([]string{"GET_LOCK"}).AddRow(1),
		)

	// Simulate an incorrectly selected database.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DATABASE()")).
		WillReturnRows(
			sqlmock.NewRows([]string{"DATABASE()"}).AddRow("target_db"),
		)

	// The lock must be released even when database verification fails.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).
		WithArgs(lockName).
		WillReturnRows(
			sqlmock.NewRows([]string{"RELEASE_LOCK"}).AddRow(1),
		)

	plan := &migrationplan.Plan{
		ID:                 1,
		SourceConnectionID: 1,
		TargetConnectionID: 3,
		SourceDatabase:     "source_db",
		TargetDatabase:     "target_db",
		Operations: []migrationplan.Operation{
			{
				ID:        1,
				Type:      "CREATE_TABLE",
				Table:     "isolation_probe",
				SQL:       "CREATE TABLE `isolation_probe` (`id` bigint NOT NULL)",
				RiskLevel: migrationplan.RiskMedium,
			},
		},
	}

	planRepo := &isolationPlanRepository{plan: plan}
	executionRepo := &isolationExecutionRepository{}
	databaseProvider := &isolationDatabaseProvider{db: db}

	service := NewMigrationExecutionService(
		planRepo,
		executionRepo,
		databaseProvider,
	)

	execution, err := service.Execute(context.Background(), plan.ID)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if execution.Status != migrationexecution.ExecutionFailed {
		t.Fatalf(
			"expected execution status FAILED, got %s",
			execution.Status,
		)
	}

	if !strings.Contains(execution.ErrorMessage, "target_db") {
		t.Fatalf(
			"expected failure message to identify target_db, got %q",
			execution.ErrorMessage,
		)
	}

	if !strings.Contains(execution.ErrorMessage, "refusing execution") {
		t.Fatalf(
			"expected database isolation refusal, got %q",
			execution.ErrorMessage,
		)
	}

	if executionRepo.operationTransitions != 0 {
		t.Fatalf(
			"expected no migration operations to start, got %d transitions",
			executionRepo.operationTransitions,
		)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL mock expectations were not met: %v", err)
	}
}
