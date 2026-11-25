package service

import (
	"context"
	"fmt"

	"github.com/Anmol-20/gomigrate/internal/migrationplan"
	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
	"github.com/Anmol-20/gomigrate/internal/schemadiff"
)

type MigrationPlanRepository interface {
	Create(ctx context.Context, plan *migrationplan.Plan) error
	GetByID(ctx context.Context, id int64) (*migrationplan.Plan, error)
}

type MigrationPlanService struct {
	schemaProvider SchemaProvider
	generator      *migrationplan.Generator
	planRepository MigrationPlanRepository
}

func NewMigrationPlanService(
	provider SchemaProvider,
	repository MigrationPlanRepository,
) *MigrationPlanService {

	return &MigrationPlanService{
		schemaProvider: provider,
		generator:      migrationplan.NewGenerator(),
		planRepository: repository,
	}
}

func (s *MigrationPlanService) Generate(
	ctx context.Context,
	sourceID int64,
	targetID int64,
) (*migrationplan.Plan, error) {

	if sourceID <= 0 || targetID <= 0 {
		return nil, fmt.Errorf(
			"%w: source and target connection IDs must be positive",
			ErrInvalidInput,
		)
	}

	if sourceID == targetID {
		return nil, fmt.Errorf(
			"%w: source and target connections must be different",
			ErrInvalidInput,
		)
	}

	// Step 1: Discover source schema.
	sourceSchema, err := s.schemaProvider.DiscoverSchema(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("discover source schema: %w", err)
	}

	// Step 2: Discover target schema.
	targetSchema, err := s.schemaProvider.DiscoverSchema(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("discover target schema: %w", err)
	}

	// Step 3: Compare schemas.
	comparison := schemadiff.Compare(
		sourceSchema,
		targetSchema,
	)

	// Step 4: Generate migration plan.
	plan, err := s.generator.Generate(
		sourceSchema,
		comparison,
	)
	if err != nil {
		return nil, fmt.Errorf("generate migration plan: %w", err)
	}

	// Step 5: Persist an immutable snapshot for later execution.
	if s.planRepository == nil {
		return nil, fmt.Errorf("migration plan repository is not configured")
	}

	plan.SourceConnectionID = sourceID
	plan.TargetConnectionID = targetID

	if err := s.planRepository.Create(ctx, plan); err != nil {
		return nil, fmt.Errorf("persist migration plan: %w", err)
	}

	return plan, nil
}

// Compile-time check that the schema provider returns the expected schema type.
var _ interface {
	DiscoverSchema(context.Context, int64) (*mysqlclient.Schema, error)
} = (SchemaProvider)(nil)
