package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Anmol-20/gomigrate/internal/migrationplan"
)

// MySQLMigrationPlanRepository persists the exact plan snapshot that was
// returned for review. Execution must load this snapshot rather than
// regenerate a plan from potentially changed schemas.
type MigrationPlanRepository interface {
	Create(ctx context.Context, plan *migrationplan.Plan) error
	GetByID(ctx context.Context, id int64) (*migrationplan.Plan, error)
}

type MySQLMigrationPlanRepository struct {
	db *sql.DB
}

func NewMySQLMigrationPlanRepository(db *sql.DB) *MySQLMigrationPlanRepository {
	return &MySQLMigrationPlanRepository{db: db}
}

func (r *MySQLMigrationPlanRepository) Create(
	ctx context.Context,
	plan *migrationplan.Plan,
) error {
	if plan == nil {
		return fmt.Errorf("migration plan cannot be nil")
	}
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("validate migration plan: %w", err)
	}
	if plan.SourceConnectionID <= 0 || plan.TargetConnectionID <= 0 || plan.SourceConnectionID == plan.TargetConnectionID {
		return fmt.Errorf("valid, distinct source and target connection IDs are required")
	}

	// Exclude persistence metadata from the snapshot so the checksum is stable
	// and can be independently verified when the plan is loaded.
	snapshot := *plan
	snapshot.ID = 0
	snapshot.Checksum = ""
	snapshot.CreatedAt = nil
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal migration plan snapshot: %w", err)
	}
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])

	result, err := r.db.ExecContext(ctx, `
		INSERT INTO migration_plans (
			source_connection_id, target_connection_id,
			source_database, target_database, plan_json, plan_checksum
		) VALUES (?, ?, ?, ?, ?, ?)
	`, plan.SourceConnectionID, plan.TargetConnectionID,
		plan.SourceDatabase, plan.TargetDatabase, payload, checksum)
	if err != nil {
		return fmt.Errorf("insert migration plan: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get migration plan ID: %w", err)
	}
	var createdAt time.Time
	if err := r.db.QueryRowContext(ctx, `SELECT created_at FROM migration_plans WHERE id = ?`, id).Scan(&createdAt); err != nil {
		return fmt.Errorf("load migration plan creation time: %w", err)
	}
	plan.ID = id
	plan.Checksum = checksum
	plan.CreatedAt = &createdAt
	return nil
}

// GetByID returns the persisted snapshot only after verifying its checksum.
func (r *MySQLMigrationPlanRepository) GetByID(ctx context.Context, id int64) (*migrationplan.Plan, error) {
	if id <= 0 {
		return nil, fmt.Errorf("migration plan ID must be positive")
	}
	var plan migrationplan.Plan
	var payload []byte
	var checksum string
	var createdAt time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT id, source_connection_id, target_connection_id,
		       plan_json, plan_checksum, created_at
		FROM migration_plans WHERE id = ?
	`, id).Scan(&plan.ID, &plan.SourceConnectionID, &plan.TargetConnectionID, &payload, &checksum, &createdAt)
	if err != nil {
		return nil, err
	}
	sourceConnectionID := plan.SourceConnectionID
	targetConnectionID := plan.TargetConnectionID
	if err := json.Unmarshal(payload, &plan); err != nil {
		return nil, fmt.Errorf("decode migration plan snapshot: %w", err)
	}
	// Persistence metadata is held in relational columns, not trusted from JSON.
	plan.ID = id
	plan.SourceConnectionID = sourceConnectionID
	plan.TargetConnectionID = targetConnectionID
	plan.Checksum = checksum
	plan.CreatedAt = &createdAt
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate stored migration plan: %w", err)
	}
	if plan.SourceConnectionID <= 0 || plan.TargetConnectionID <= 0 || plan.SourceConnectionID == plan.TargetConnectionID {
		return nil, fmt.Errorf("stored migration plan has invalid connection references")
	}
	snapshot := plan
	snapshot.ID = 0
	snapshot.Checksum = ""
	snapshot.CreatedAt = nil
	canonical, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal stored migration plan: %w", err)
	}
	digest := sha256.Sum256(canonical)
	if hex.EncodeToString(digest[:]) != checksum {
		return nil, fmt.Errorf("stored migration plan checksum mismatch")
	}
	return &plan, nil
}
