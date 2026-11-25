package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestGetByIDHandlesNullableErrorMessages(t *testing.T) {
	tests := []struct {
		name              string
		executionError    any
		operationError    any
		expectedExecError string
		expectedOpError   string
	}{
		{
			name:           "successful execution with NULL errors",
			executionError: nil,
			operationError: nil,
		},
		{
			name:              "failed execution with populated errors",
			executionError:    "execution failed",
			operationError:    "operation failed",
			expectedExecError: "execution failed",
			expectedOpError:   "operation failed",
		},
	}

	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("create sqlmock: %v", err)
			}
			defer db.Close()

			repo := NewMySQLMigrationExecutionRepository(db)

			executionQuery := regexp.QuoteMeta(
				`SELECT
            id,
            plan_id,
            status,
            started_at,
            completed_at,
            error_message,
            created_at,
            updated_at
        FROM migration_executions
        WHERE id = ?`,
			)

			mock.ExpectQuery(executionQuery).
				WithArgs(int64(1)).
				WillReturnRows(
					sqlmock.NewRows([]string{
						"id",
						"plan_id",
						"status",
						"started_at",
						"completed_at",
						"error_message",
						"created_at",
						"updated_at",
					}).AddRow(
						int64(1),
						int64(1),
						"COMPLETED",
						now,
						now,
						tt.executionError,
						now,
						now,
					),
				)

			operationQuery := regexp.QuoteMeta(
				`SELECT
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
        ORDER BY operation_id`,
			)

			mock.ExpectQuery(operationQuery).
				WithArgs(int64(1)).
				WillReturnRows(
					sqlmock.NewRows([]string{
						"id",
						"execution_id",
						"operation_id",
						"operation_type",
						"table_name",
						"status",
						"started_at",
						"completed_at",
						"error_message",
						"created_at",
						"updated_at",
					}).AddRow(
						int64(1),
						int64(1),
						1,
						"CREATE_TABLE",
						"customers",
						"COMPLETED",
						now,
						now,
						tt.operationError,
						now,
						now,
					),
				)

			execution, err := repo.GetByID(context.Background(), 1)
			if err != nil {
				t.Fatalf("GetByID returned error: %v", err)
			}

			if execution.ErrorMessage != tt.expectedExecError {
				t.Errorf(
					"execution error = %q, want %q",
					execution.ErrorMessage,
					tt.expectedExecError,
				)
			}

			if len(execution.Operations) != 1 {
				t.Fatalf(
					"got %d operations, want 1",
					len(execution.Operations),
				)
			}

			if execution.Operations[0].ErrorMessage != tt.expectedOpError {
				t.Errorf(
					"operation error = %q, want %q",
					execution.Operations[0].ErrorMessage,
					tt.expectedOpError,
				)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled SQL expectations: %v", err)
			}
		})
	}
}
