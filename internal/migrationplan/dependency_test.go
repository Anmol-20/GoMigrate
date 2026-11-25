package migrationplan

import (
	"strings"
	"testing"

	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
)

func TestOrderNewTablesParentBeforeChild(t *testing.T) {
	schema := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name: "orders",
				ForeignKeys: []mysqlclient.ForeignKey{
					{
						Name:              "fk_orders_customers",
						Columns:           []string{"customer_id"},
						ReferencedTable:   "customers",
						ReferencedColumns: []string{"id"},
					},
				},
			},
			{
				Name: "customers",
			},
		},
	}

	ordered, warnings := orderNewTables(
		schema,
		[]string{"orders", "customers"},
	)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}

	if len(ordered) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(ordered))
	}

	if ordered[0] != "customers" || ordered[1] != "orders" {
		t.Fatalf("unexpected table order: %v", ordered)
	}
}

func TestOrderNewTablesDetectsCycle(t *testing.T) {
	schema := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name: "table_a",
				ForeignKeys: []mysqlclient.ForeignKey{
					{
						Name:              "fk_a_b",
						Columns:           []string{"b_id"},
						ReferencedTable:   "table_b",
						ReferencedColumns: []string{"id"},
					},
				},
			},
			{
				Name: "table_b",
				ForeignKeys: []mysqlclient.ForeignKey{
					{
						Name:              "fk_b_a",
						Columns:           []string{"a_id"},
						ReferencedTable:   "table_a",
						ReferencedColumns: []string{"id"},
					},
				},
			},
		},
	}

	ordered, warnings := orderNewTables(
		schema,
		[]string{"table_a", "table_b"},
	)

	if len(ordered) != 2 {
		t.Fatalf("expected both tables to be retained, got %v", ordered)
	}

	if len(warnings) == 0 {
		t.Fatal("expected a circular dependency warning")
	}

	if !strings.Contains(warnings[0], "cycle detected") {
		t.Fatalf("unexpected warning: %s", warnings[0])
	}
}

func TestValidateForeignKeyReferences(t *testing.T) {
	schema := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name: "customers",
				Columns: []mysqlclient.Column{
					{Name: "id"},
				},
				Indexes: []mysqlclient.Index{
					{
						Name:    "PRIMARY",
						Primary: true,
						Columns: []string{"id"},
					},
				},
			},
			{
				Name: "orders",
				Columns: []mysqlclient.Column{
					{Name: "id"},
					{Name: "customer_id"},
				},
				ForeignKeys: []mysqlclient.ForeignKey{
					{
						Name:              "fk_orders_customers",
						Columns:           []string{"customer_id"},
						ReferencedTable:   "customers",
						ReferencedColumns: []string{"id"},
					},
				},
			},
		},
	}

	if err := validateForeignKeyReferences(schema); err != nil {
		t.Fatalf("expected valid foreign key references, got %v", err)
	}
}

func TestValidateForeignKeyReferencesRejectsMissingParentTable(t *testing.T) {
	schema := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name: "orders",
				Columns: []mysqlclient.Column{
					{Name: "customer_id"},
				},
				ForeignKeys: []mysqlclient.ForeignKey{
					{
						Name:              "fk_orders_customers",
						Columns:           []string{"customer_id"},
						ReferencedTable:   "customers",
						ReferencedColumns: []string{"id"},
					},
				},
			},
		},
	}

	if err := validateForeignKeyReferences(schema); err == nil {
		t.Fatal("expected error for missing referenced table")
	}
}

func TestValidateForeignKeyReferencesRejectsMissingParentIndex(t *testing.T) {
	schema := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name: "customers",
				Columns: []mysqlclient.Column{
					{Name: "id"},
				},
			},
			{
				Name: "orders",
				Columns: []mysqlclient.Column{
					{Name: "customer_id"},
				},
				ForeignKeys: []mysqlclient.ForeignKey{
					{
						Name:              "fk_orders_customers",
						Columns:           []string{"customer_id"},
						ReferencedTable:   "customers",
						ReferencedColumns: []string{"id"},
					},
				},
			},
		},
	}

	if err := validateForeignKeyReferences(schema); err == nil {
		t.Fatal("expected error for missing referenced index")
	}
}

func TestHasReferencedIndexUsesLeftmostColumns(t *testing.T) {
	indexes := []mysqlclient.Index{
		{
			Name:    "idx_customer_status",
			Columns: []string{"customer_id", "status"},
		},
	}

	if !hasReferencedIndex(indexes, []string{"customer_id"}) {
		t.Fatal("expected index to support its leftmost column")
	}

	if !hasReferencedIndex(indexes, []string{"customer_id", "status"}) {
		t.Fatal("expected index to support its complete column sequence")
	}

	if hasReferencedIndex(indexes, []string{"status"}) {
		t.Fatal("index should not support a non-leftmost column")
	}
}

func TestBuildImpactSummary(t *testing.T) {
	plan := &Plan{
		Operations: []Operation{
			{
				Type:               "CREATE_TABLE",
				RiskLevel:          RiskMedium,
				RollbackAssessment: RollbackManual,
			},
			{
				Type:               "MODIFY_COLUMN",
				RiskLevel:          RiskHigh,
				RequiresReview:     true,
				RollbackAssessment: RollbackManual,
			},
			{
				Type:               "ADD_INDEX",
				RiskLevel:          RiskMedium,
				RollbackAssessment: RollbackSchemaOnly,
			},
			{
				Type:               "ADD_FOREIGN_KEY",
				RiskLevel:          RiskHigh,
				RequiresReview:     true,
				RollbackAssessment: RollbackSchemaOnly,
			},
		},
	}

	buildImpactSummary(plan)

	if plan.Summary.TotalOperations != 4 {
		t.Fatalf("expected 4 operations, got %d", plan.Summary.TotalOperations)
	}

	if plan.Summary.MediumRiskOperations != 2 {
		t.Fatalf("expected 2 medium-risk operations, got %d", plan.Summary.MediumRiskOperations)
	}

	if plan.Summary.HighRiskOperations != 2 {
		t.Fatalf("expected 2 high-risk operations, got %d", plan.Summary.HighRiskOperations)
	}

	if plan.Summary.OperationsRequiringReview != 2 {
		t.Fatalf("expected 2 operations requiring review, got %d", plan.Summary.OperationsRequiringReview)
	}

	if plan.Summary.SchemaOnlyRollbackOperations != 2 {
		t.Fatalf("expected 2 schema-only rollback assessments, got %d", plan.Summary.SchemaOnlyRollbackOperations)
	}

	if plan.Summary.ManualRollbackOperations != 2 {
		t.Fatalf("expected 2 manual rollback assessments, got %d", plan.Summary.ManualRollbackOperations)
	}
}
