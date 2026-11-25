package service

import (
	"context"
	"fmt"

	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
	"github.com/Anmol-20/gomigrate/internal/schemadiff"
)

// SchemaProvider defines the capability required to discover a database schema.
type SchemaProvider interface {
	DiscoverSchema(
		ctx context.Context,
		id int64,
	) (*mysqlclient.Schema, error)
}

type SchemaComparisonService struct {
	schemaProvider SchemaProvider
}

func NewSchemaComparisonService(
	provider SchemaProvider,
) *SchemaComparisonService {

	return &SchemaComparisonService{
		schemaProvider: provider,
	}
}

func (s *SchemaComparisonService) Compare(
	ctx context.Context,
	sourceID int64,
	targetID int64,
) (*schemadiff.Result, error) {

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

	// Step 1: Discover source database schema.
	sourceSchema, err := s.schemaProvider.DiscoverSchema(
		ctx,
		sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("discover source schema: %w", err)
	}

	// Step 2: Discover target database schema.
	targetSchema, err := s.schemaProvider.DiscoverSchema(
		ctx,
		targetID,
	)
	if err != nil {
		return nil, fmt.Errorf("discover target schema: %w", err)
	}

	// Step 3: Compare the two schemas.
	result := schemadiff.Compare(
		sourceSchema,
		targetSchema,
	)

	return result, nil
}
