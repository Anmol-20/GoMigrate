package schemadiff

import (
	"testing"

	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
)

func TestCompareEmptyTarget(t *testing.T) {

	source := &mysqlclient.Schema{
		Database: "source_db",
		Tables: []mysqlclient.Table{
			{Name: "customers"},
			{Name: "orders"},
			{Name: "products"},
		},
	}

	target := &mysqlclient.Schema{
		Database: "target_db",
		Tables:   []mysqlclient.Table{},
	}

	result := Compare(source, target)

	if len(result.SourceOnlyTables) != 3 {
		t.Fatalf(
			"expected 3 source-only tables, got %d",
			len(result.SourceOnlyTables),
		)
	}

	if len(result.TargetOnlyTables) != 0 {
		t.Fatalf(
			"expected 0 target-only tables, got %d",
			len(result.TargetOnlyTables),
		)
	}

	if len(result.CommonTables) != 0 {
		t.Fatalf(
			"expected 0 common tables, got %d",
			len(result.CommonTables),
		)
	}
}

func TestCompareColumnDifferences(t *testing.T) {

	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name: "customers",
				Columns: []mysqlclient.Column{
					{
						Name:       "id",
						ColumnType: "bigint",
					},
					{
						Name:       "name",
						ColumnType: "varchar(100)",
					},
					{
						Name:       "email",
						ColumnType: "varchar(255)",
					},
				},
			},
		},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name: "customers",
				Columns: []mysqlclient.Column{
					{
						Name:       "id",
						ColumnType: "bigint",
					},
					{
						Name:       "name",
						ColumnType: "varchar(150)",
					},
					{
						Name:       "phone",
						ColumnType: "varchar(20)",
					},
				},
			},
		},
	}

	result := Compare(source, target)

	if len(result.CommonTables) != 1 {
		t.Fatalf("expected 1 common table")
	}

	table := result.CommonTables[0]

	if len(table.MissingColumnsInTarget) != 1 ||
		table.MissingColumnsInTarget[0] != "email" {
		t.Errorf("email should be missing in target")
	}

	if len(table.ExtraColumnsInTarget) != 1 ||
		table.ExtraColumnsInTarget[0] != "phone" {
		t.Errorf("phone should be extra in target")
	}

	if len(table.ColumnDifferences) != 1 {
		t.Fatalf(
			"expected 1 column difference, got %d",
			len(table.ColumnDifferences),
		)
	}

	diff := table.ColumnDifferences[0]

	if diff.ColumnName != "name" ||
		diff.Field != "column_type" {
		t.Errorf("unexpected column difference: %+v", diff)
	}
}

func TestCompareIndexes(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name: "customers",
			Indexes: []mysqlclient.Index{{
				Name:    "email",
				Unique:  true,
				Columns: []string{"email"},
			}},
		}},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name: "customers",
		}},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.MissingIndexesInTarget) != 1 {
		t.Fatalf("expected one missing index")
	}

	if table.MissingIndexesInTarget[0] != "email" {
		t.Fatalf("expected missing email index")
	}
}

func TestCompareForeignKeys(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name: "orders",
			ForeignKeys: []mysqlclient.ForeignKey{{
				Name:             "fk_orders_customers",
				Column:           "customer_id",
				ReferencedTable:  "customers",
				ReferencedColumn: "id",
				UpdateRule:       "NO ACTION",
				DeleteRule:       "NO ACTION",
			}},
		}},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name: "orders",
		}},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.MissingForeignKeysInTarget) != 1 {
		t.Fatalf("expected one missing foreign key")
	}
}

func TestCompareDifferentEngines(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:   "customers",
				Engine: "InnoDB",
			},
		},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:   "customers",
				Engine: "MyISAM",
			},
		},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.TableDifferences) != 1 {
		t.Fatalf(
			"expected 1 table difference, got %d",
			len(table.TableDifferences),
		)
	}

	diff := table.TableDifferences[0]

	if diff.Field != "engine" ||
		diff.Source != "InnoDB" ||
		diff.Target != "MyISAM" {
		t.Errorf("unexpected table difference: %+v", diff)
	}
}

func TestCompareDifferentCharsets(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:    "customers",
				Charset: "utf8mb4",
			},
		},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:    "customers",
				Charset: "latin1",
			},
		},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.TableDifferences) != 1 {
		t.Fatalf(
			"expected 1 table difference, got %d",
			len(table.TableDifferences),
		)
	}

	diff := table.TableDifferences[0]

	if diff.Field != "charset" ||
		diff.Source != "utf8mb4" ||
		diff.Target != "latin1" {
		t.Errorf("unexpected table difference: %+v", diff)
	}
}

func TestCompareDifferentCollations(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:      "customers",
				Collation: "utf8mb4_0900_ai_ci",
			},
		},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:      "customers",
				Collation: "utf8mb4_general_ci",
			},
		},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.TableDifferences) != 1 {
		t.Fatalf(
			"expected 1 table difference, got %d",
			len(table.TableDifferences),
		)
	}

	diff := table.TableDifferences[0]

	if diff.Field != "collation" ||
		diff.Source != "utf8mb4_0900_ai_ci" ||
		diff.Target != "utf8mb4_general_ci" {
		t.Errorf("unexpected table difference: %+v", diff)
	}
}

func TestCompareTablePropertiesCaseInsensitive(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:      "customers",
				Engine:    "InnoDB",
				Charset:   "utf8mb4",
				Collation: "utf8mb4_0900_ai_ci",
			},
		},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:      "customers",
				Engine:    "innodb",
				Charset:   "UTF8MB4",
				Collation: "UTF8MB4_0900_AI_CI",
			},
		},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.TableDifferences) != 0 {
		t.Errorf(
			"expected no table differences for case-insensitive comparison, got %+v",
			table.TableDifferences,
		)
	}
}

func TestCompareIdenticalTableProperties(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:      "customers",
				Engine:    "InnoDB",
				Charset:   "utf8mb4",
				Collation: "utf8mb4_0900_ai_ci",
			},
		},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{
			{
				Name:      "customers",
				Engine:    "InnoDB",
				Charset:   "utf8mb4",
				Collation: "utf8mb4_0900_ai_ci",
			},
		},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.TableDifferences) != 0 {
		t.Errorf(
			"expected no table differences, got %+v",
			table.TableDifferences,
		)
	}
}

func TestCompareIdenticalCompositeForeignKeys(t *testing.T) {
	sourceFK := mysqlclient.ForeignKey{
		Name:              "fk_order_product",
		Columns:           []string{"order_id", "product_id"},
		ReferencedTable:   "order_products",
		ReferencedColumns: []string{"order_id", "product_id"},
		UpdateRule:        "CASCADE",
		DeleteRule:        "RESTRICT",
	}

	targetFK := mysqlclient.ForeignKey{
		Name:              "fk_order_product",
		Columns:           []string{"order_id", "product_id"},
		ReferencedTable:   "order_products",
		ReferencedColumns: []string{"order_id", "product_id"},
		UpdateRule:        "CASCADE",
		DeleteRule:        "RESTRICT",
	}

	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name:        "order_items",
			ForeignKeys: []mysqlclient.ForeignKey{sourceFK},
		}},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name:        "order_items",
			ForeignKeys: []mysqlclient.ForeignKey{targetFK},
		}},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.ForeignKeyDifferences) != 0 {
		t.Errorf(
			"expected no foreign key differences, got %+v",
			table.ForeignKeyDifferences,
		)
	}

	if len(table.MissingForeignKeysInTarget) != 0 {
		t.Errorf("expected no missing foreign keys")
	}

	if len(table.ExtraForeignKeysInTarget) != 0 {
		t.Errorf("expected no extra foreign keys")
	}
}

func TestCompareCompositeForeignKeyColumnOrder(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name: "order_items",
			ForeignKeys: []mysqlclient.ForeignKey{{
				Name:              "fk_order_product",
				Columns:           []string{"order_id", "product_id"},
				ReferencedTable:   "order_products",
				ReferencedColumns: []string{"order_id", "product_id"},
				UpdateRule:        "CASCADE",
				DeleteRule:        "RESTRICT",
			}},
		}},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name: "order_items",
			ForeignKeys: []mysqlclient.ForeignKey{{
				Name:              "fk_order_product",
				Columns:           []string{"product_id", "order_id"},
				ReferencedTable:   "order_products",
				ReferencedColumns: []string{"order_id", "product_id"},
				UpdateRule:        "CASCADE",
				DeleteRule:        "RESTRICT",
			}},
		}},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.ForeignKeyDifferences) != 1 {
		t.Fatalf(
			"expected 1 foreign key difference, got %d",
			len(table.ForeignKeyDifferences),
		)
	}

	diff := table.ForeignKeyDifferences[0]

	if diff.Field != "columns" {
		t.Errorf("expected columns difference, got %q", diff.Field)
	}
}

func TestCompareCompositeForeignKeyReferencedColumns(t *testing.T) {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name: "order_items",
			ForeignKeys: []mysqlclient.ForeignKey{{
				Name:              "fk_order_product",
				Columns:           []string{"order_id", "product_id"},
				ReferencedTable:   "order_products",
				ReferencedColumns: []string{"order_id", "product_id"},
				UpdateRule:        "CASCADE",
				DeleteRule:        "RESTRICT",
			}},
		}},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name: "order_items",
			ForeignKeys: []mysqlclient.ForeignKey{{
				Name:              "fk_order_product",
				Columns:           []string{"order_id", "product_id"},
				ReferencedTable:   "order_products",
				ReferencedColumns: []string{"product_id", "order_id"},
				UpdateRule:        "CASCADE",
				DeleteRule:        "RESTRICT",
			}},
		}},
	}

	result := Compare(source, target)
	table := result.CommonTables[0]

	if len(table.ForeignKeyDifferences) != 1 {
		t.Fatalf(
			"expected 1 foreign key difference, got %d",
			len(table.ForeignKeyDifferences),
		)
	}

	diff := table.ForeignKeyDifferences[0]

	if diff.Field != "referenced_columns" {
		t.Errorf(
			"expected referenced_columns difference, got %q",
			diff.Field,
		)
	}
}

func compareSingleIndex(
	sourceIndex mysqlclient.Index,
	targetIndex mysqlclient.Index,
) TableComparison {
	source := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name:    "customers",
			Indexes: []mysqlclient.Index{sourceIndex},
		}},
	}

	target := &mysqlclient.Schema{
		Tables: []mysqlclient.Table{{
			Name:    "customers",
			Indexes: []mysqlclient.Index{targetIndex},
		}},
	}

	result := Compare(source, target)
	return result.CommonTables[0]
}

func TestCompareIdenticalFunctionalIndexes(t *testing.T) {
	source := mysqlclient.Index{
		Name:   "idx_email_lower",
		Unique: true,
		Parts: []mysqlclient.IndexPart{
			{Expression: "lower(`email`)"},
		},
	}

	target := mysqlclient.Index{
		Name:   "idx_email_lower",
		Unique: true,
		Parts: []mysqlclient.IndexPart{
			{Expression: "lower(`email`)"},
		},
	}

	table := compareSingleIndex(source, target)

	if len(table.IndexDifferences) != 0 {
		t.Errorf("expected no index differences, got %+v", table.IndexDifferences)
	}
}

func TestCompareDifferentFunctionalIndexExpressions(t *testing.T) {
	source := mysqlclient.Index{
		Name: "idx_email",
		Parts: []mysqlclient.IndexPart{
			{Expression: "lower(`email`)"},
		},
	}

	target := mysqlclient.Index{
		Name: "idx_email",
		Parts: []mysqlclient.IndexPart{
			{Expression: "upper(`email`)"},
		},
	}

	table := compareSingleIndex(source, target)

	if len(table.IndexDifferences) != 1 {
		t.Fatalf(
			"expected 1 index difference, got %d",
			len(table.IndexDifferences),
		)
	}

	diff := table.IndexDifferences[0]

	if diff.Source.Parts[0].Expression != "lower(`email`)" {
		t.Errorf("unexpected source expression: %+v", diff.Source.Parts)
	}

	if diff.Target.Parts[0].Expression != "upper(`email`)" {
		t.Errorf("unexpected target expression: %+v", diff.Target.Parts)
	}
}

func TestCompareMixedIndexParts(t *testing.T) {
	source := mysqlclient.Index{
		Name: "idx_tenant_email",
		Parts: []mysqlclient.IndexPart{
			{Column: "tenant_id"},
			{Expression: "lower(`email`)"},
		},
	}

	target := mysqlclient.Index{
		Name: "idx_tenant_email",
		Parts: []mysqlclient.IndexPart{
			{Column: "tenant_id"},
			{Expression: "upper(`email`)"},
		},
	}

	table := compareSingleIndex(source, target)

	if len(table.IndexDifferences) != 1 {
		t.Fatalf(
			"expected 1 index difference, got %d",
			len(table.IndexDifferences),
		)
	}

	diff := table.IndexDifferences[0]

	if diff.Source.Parts[0].Column != "tenant_id" ||
		diff.Source.Parts[1].Expression != "lower(`email`)" {
		t.Errorf("unexpected source index parts: %+v", diff.Source.Parts)
	}
}

func TestCompareIndexPartsOrder(t *testing.T) {
	source := mysqlclient.Index{
		Name: "idx_tenant_email",
		Parts: []mysqlclient.IndexPart{
			{Column: "tenant_id"},
			{Expression: "lower(`email`)"},
		},
	}

	target := mysqlclient.Index{
		Name: "idx_tenant_email",
		Parts: []mysqlclient.IndexPart{
			{Expression: "lower(`email`)"},
			{Column: "tenant_id"},
		},
	}

	table := compareSingleIndex(source, target)

	if len(table.IndexDifferences) != 1 {
		t.Fatalf(
			"expected 1 index difference due to ordering, got %d",
			len(table.IndexDifferences),
		)
	}
}

func TestCompareLegacyIndexColumns(t *testing.T) {
	source := mysqlclient.Index{
		Name:    "idx_email",
		Unique:  true,
		Columns: []string{"email"},
	}

	target := mysqlclient.Index{
		Name:    "idx_email",
		Unique:  true,
		Columns: []string{"email"},
	}

	table := compareSingleIndex(source, target)

	if len(table.IndexDifferences) != 0 {
		t.Errorf(
			"expected no differences for identical legacy indexes, got %+v",
			table.IndexDifferences,
		)
	}
}

func TestCompareIndexPrefixLength(t *testing.T) {
	source := mysqlclient.Index{
		Name: "idx_email",
		Parts: []mysqlclient.IndexPart{
			{Column: "email", PrefixLength: 100},
		},
	}

	target := mysqlclient.Index{
		Name: "idx_email",
		Parts: []mysqlclient.IndexPart{
			{Column: "email", PrefixLength: 50},
		},
	}

	table := compareSingleIndex(source, target)

	if len(table.IndexDifferences) != 1 {
		t.Fatalf("expected prefix-length difference, got %+v",
			table.IndexDifferences)
	}

	if table.IndexDifferences[0].Source.Parts[0].PrefixLength != 100 {
		t.Fatal("source prefix length was not preserved")
	}
}

func TestCompareIndexSortDirection(t *testing.T) {
	source := mysqlclient.Index{
		Name: "idx_created",
		Parts: []mysqlclient.IndexPart{
			{Column: "created_at", Order: "DESC"},
		},
	}

	target := mysqlclient.Index{
		Name: "idx_created",
		Parts: []mysqlclient.IndexPart{
			{Column: "created_at", Order: "ASC"},
		},
	}

	table := compareSingleIndex(source, target)

	if len(table.IndexDifferences) != 1 {
		t.Fatalf("expected sort-direction difference, got %+v",
			table.IndexDifferences)
	}
}

func TestCompareIndexType(t *testing.T) {
	source := mysqlclient.Index{
		Name: "idx_name",
		Type: "BTREE",
		Parts: []mysqlclient.IndexPart{
			{Column: "name"},
		},
	}

	target := mysqlclient.Index{
		Name: "idx_name",
		Type: "HASH",
		Parts: []mysqlclient.IndexPart{
			{Column: "name"},
		},
	}

	table := compareSingleIndex(source, target)

	if len(table.IndexDifferences) != 1 {
		t.Fatalf("expected index-type difference, got %+v",
			table.IndexDifferences)
	}

	if table.IndexDifferences[0].Source.Type != "BTREE" ||
		table.IndexDifferences[0].Target.Type != "HASH" {
		t.Fatal("index types were not preserved in comparison")
	}
}
