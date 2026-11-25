package mysqlclient

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newSchemaMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create SQL mock: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db, mock
}

func TestDiscoverSchemaEmpty(t *testing.T) {
	db, mock := newSchemaMock(t)

	mock.ExpectQuery("SELECT DATABASE\\(\\)").
		WillReturnRows(
			sqlmock.NewRows([]string{"DATABASE()"}).
				AddRow("test_db"),
		)

	mock.ExpectQuery("FROM information_schema.tables").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"table_name",
				"table_type",
				"engine",
				"table_collation",
				"character_set_name",
			}),
		)

	schema, err := DiscoverSchema(context.Background(), db)
	if err != nil {
		t.Fatalf("DiscoverSchema returned error: %v", err)
	}

	if schema.Database != "test_db" {
		t.Errorf("expected database test_db, got %q", schema.Database)
	}

	if len(schema.Tables) != 0 {
		t.Errorf("expected 0 tables, got %d", len(schema.Tables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverColumns(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{
		Name:    "customers",
		Columns: make([]Column, 0),
	}

	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("customers").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"column_name",
				"data_type",
				"column_type",
				"is_nullable",
				"column_default",
				"column_key",
				"extra",
				"ordinal_position",
			}).
				AddRow(
					"id",
					"bigint",
					"bigint",
					"NO",
					nil,
					"PRI",
					"auto_increment",
					1,
				).
				AddRow(
					"name",
					"varchar",
					"varchar(100)",
					"NO",
					nil,
					"",
					"",
					2,
				).
				AddRow(
					"email",
					"varchar",
					"varchar(255)",
					"YES",
					nil,
					"UNI",
					"",
					3,
				).
				AddRow(
					"created_at",
					"timestamp",
					"timestamp",
					"YES",
					"CURRENT_TIMESTAMP",
					"",
					"",
					4,
				),
		)

	err := discoverColumns(context.Background(), db, table)
	if err != nil {
		t.Fatalf("discoverColumns returned error: %v", err)
	}

	if len(table.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(table.Columns))
	}

	id := table.Columns[0]

	if id.Name != "id" {
		t.Errorf("expected id, got %q", id.Name)
	}

	if id.Nullable {
		t.Error("expected id to be NOT NULL")
	}

	if id.Extra != "auto_increment" {
		t.Errorf("unexpected extra value: %q", id.Extra)
	}

	email := table.Columns[2]

	if !email.Nullable {
		t.Error("expected email to be nullable")
	}

	createdAt := table.Columns[3]

	if createdAt.DefaultValue == nil {
		t.Fatal("expected created_at default value")
	}

	if *createdAt.DefaultValue != "CURRENT_TIMESTAMP" {
		t.Errorf(
			"unexpected created_at default: %q",
			*createdAt.DefaultValue,
		)
	}

	if table.Columns[0].OrdinalPosition != 1 ||
		table.Columns[3].OrdinalPosition != 4 {
		t.Error("column ordinal positions were not preserved")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverIndexes(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{
		Name:    "orders",
		Indexes: make([]Index, 0),
	}

	mock.ExpectQuery("FROM information_schema.statistics").
		WithArgs("orders").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"index_name",
				"non_unique",
				"column_name",
				"expression",
				"sub_part",
				"collation",
				"index_type",
			}).
				AddRow("PRIMARY", 0, "id", nil, nil, "A", "BTREE").
				AddRow("idx_customer_order", 1, "customer_id", nil, nil, "A", "BTREE").
				AddRow("idx_customer_order", 1, "order_date", nil, nil, "D", "BTREE").
				AddRow("uq_order_reference", 0, "reference", nil, 100, "A", "BTREE"),
		)

	err := discoverIndexes(context.Background(), db, table)
	if err != nil {
		t.Fatalf("discoverIndexes returned error: %v", err)
	}

	if len(table.Indexes) != 3 {
		t.Fatalf("expected 3 indexes, got %d", len(table.Indexes))
	}

	primary := table.Indexes[0]

	if !primary.Primary || !primary.Unique {
		t.Error("expected PRIMARY index to be primary and unique")
	}

	if len(primary.Columns) != 1 || primary.Columns[0] != "id" {
		t.Errorf("unexpected primary index columns: %v", primary.Columns)
	}

	composite := table.Indexes[1]

	if composite.Name != "idx_customer_order" {
		t.Errorf("unexpected index name: %q", composite.Name)
	}

	if composite.Unique {
		t.Error("expected secondary index to be non-unique")
	}

	if len(composite.Columns) != 2 ||
		composite.Columns[0] != "customer_id" ||
		composite.Columns[1] != "order_date" {
		t.Errorf("unexpected composite index columns: %v", composite.Columns)
	}

	unique := table.Indexes[2]

	if !unique.Unique {
		t.Error("expected unique index")
	}

	if composite.Parts[0].Order != "ASC" {
		t.Errorf("expected first composite part ASC, got %q", composite.Parts[0].Order)
	}

	if composite.Parts[1].Order != "DESC" {
		t.Errorf("expected second composite part DESC, got %q", composite.Parts[1].Order)
	}

	if unique.Parts[0].PrefixLength != 100 {
		t.Errorf("expected prefix length 100, got %d", unique.Parts[0].PrefixLength)
	}

	if unique.Type != "BTREE" {
		t.Errorf("expected BTREE index, got %q", unique.Type)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverCompositeForeignKey(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{
		Name:        "child_table",
		ForeignKeys: make([]ForeignKey, 0),
	}

	mock.ExpectQuery("FROM information_schema.key_column_usage").
		WithArgs("child_table").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"constraint_name",
				"column_name",
				"referenced_table_name",
				"referenced_column_name",
				"update_rule",
				"delete_rule",
			}).
				AddRow(
					"fk_child_parent",
					"tenant_id",
					"parent_table",
					"tenant_id",
					"CASCADE",
					"RESTRICT",
				).
				AddRow(
					"fk_child_parent",
					"entity_id",
					"parent_table",
					"entity_id",
					"CASCADE",
					"RESTRICT",
				),
		)

	err := discoverForeignKeys(context.Background(), db, table)
	if err != nil {
		t.Fatalf("discoverForeignKeys returned error: %v", err)
	}

	if len(table.ForeignKeys) != 1 {
		t.Fatalf("expected 1 foreign key, got %d", len(table.ForeignKeys))
	}

	fk := table.ForeignKeys[0]

	if fk.Name != "fk_child_parent" {
		t.Errorf("unexpected FK name: %q", fk.Name)
	}

	if fk.ReferencedTable != "parent_table" {
		t.Errorf("unexpected referenced table: %q", fk.ReferencedTable)
	}

	if len(fk.Columns) != 2 ||
		fk.Columns[0] != "tenant_id" ||
		fk.Columns[1] != "entity_id" {
		t.Errorf("unexpected local columns: %v", fk.Columns)
	}

	if len(fk.ReferencedColumns) != 2 ||
		fk.ReferencedColumns[0] != "tenant_id" ||
		fk.ReferencedColumns[1] != "entity_id" {
		t.Errorf("unexpected referenced columns: %v", fk.ReferencedColumns)
	}

	if fk.UpdateRule != "CASCADE" || fk.DeleteRule != "RESTRICT" {
		t.Errorf(
			"unexpected FK rules: update=%s delete=%s",
			fk.UpdateRule,
			fk.DeleteRule,
		)
	}

	// Legacy fields must be empty for composite foreign keys.
	if fk.Column != "" || fk.ReferencedColumn != "" {
		t.Error("legacy single-column fields should be empty for composite FK")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverSingleColumnForeignKey(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{
		Name:        "orders",
		ForeignKeys: make([]ForeignKey, 0),
	}

	mock.ExpectQuery("FROM information_schema.key_column_usage").
		WithArgs("orders").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"constraint_name",
				"column_name",
				"referenced_table_name",
				"referenced_column_name",
				"update_rule",
				"delete_rule",
			}).
				AddRow(
					"fk_orders_customers",
					"customer_id",
					"customers",
					"id",
					"CASCADE",
					"RESTRICT",
				),
		)

	err := discoverForeignKeys(context.Background(), db, table)
	if err != nil {
		t.Fatalf("discoverForeignKeys returned error: %v", err)
	}

	if len(table.ForeignKeys) != 1 {
		t.Fatalf("expected 1 foreign key, got %d", len(table.ForeignKeys))
	}

	fk := table.ForeignKeys[0]

	if fk.Column != "customer_id" {
		t.Errorf("unexpected legacy column: %q", fk.Column)
	}

	if fk.ReferencedColumn != "id" {
		t.Errorf("unexpected legacy referenced column: %q", fk.ReferencedColumn)
	}

	if len(fk.Columns) != 1 || fk.Columns[0] != "customer_id" {
		t.Errorf("unexpected FK columns: %v", fk.Columns)
	}

	if len(fk.ReferencedColumns) != 1 || fk.ReferencedColumns[0] != "id" {
		t.Errorf("unexpected referenced columns: %v", fk.ReferencedColumns)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverSchemaDatabaseQueryError(t *testing.T) {
	db, mock := newSchemaMock(t)

	mock.ExpectQuery("SELECT DATABASE\\(\\)").
		WillReturnError(errors.New("database unavailable"))

	_, err := DiscoverSchema(context.Background(), db)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() == "" {
		t.Fatal("expected a descriptive error")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverColumnsQueryError(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{Name: "customers"}

	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("customers").
		WillReturnError(errors.New("metadata query failed"))

	err := discoverColumns(context.Background(), db, table)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDiscoverIndexesQueryError(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{Name: "customers"}

	mock.ExpectQuery("FROM information_schema.statistics").
		WithArgs("customers").
		WillReturnError(errors.New("metadata query failed"))

	err := discoverIndexes(context.Background(), db, table)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDiscoverForeignKeysQueryError(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{Name: "customers"}

	mock.ExpectQuery("FROM information_schema.key_column_usage").
		WithArgs("customers").
		WillReturnError(errors.New("metadata query failed"))

	err := discoverForeignKeys(context.Background(), db, table)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDiscoverSchemaPopulated(t *testing.T) {
	db, mock := newSchemaMock(t)

	// 1. Mock the active database name.
	mock.ExpectQuery("SELECT DATABASE\\(\\)").
		WillReturnRows(
			sqlmock.NewRows([]string{"DATABASE()"}).
				AddRow("test_db"),
		)

	// 2. Mock two tables returned by information_schema.tables.
	mock.ExpectQuery("FROM information_schema.tables").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"table_name",
				"table_type",
				"engine",
				"table_collation",
				"character_set_name",
			}).
				AddRow(
					"customers",
					"BASE TABLE",
					"InnoDB",
					"utf8mb4_0900_ai_ci",
					"utf8mb4",
				).
				AddRow(
					"orders",
					"BASE TABLE",
					"InnoDB",
					"utf8mb4_0900_ai_ci",
					"utf8mb4",
				),
		)

	// 3. Mock customers columns.
	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("customers").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"column_name",
				"data_type",
				"column_type",
				"is_nullable",
				"column_default",
				"column_key",
				"extra",
				"ordinal_position",
			}).
				AddRow(
					"id", "bigint", "bigint",
					"NO", nil, "PRI", "auto_increment", 1,
				).
				AddRow(
					"name", "varchar", "varchar(100)",
					"NO", nil, "", "", 2,
				).
				AddRow(
					"email", "varchar", "varchar(255)",
					"YES", nil, "UNI", "", 3,
				),
		)

	// 4. Mock customers indexes.
	mock.ExpectQuery("FROM information_schema.statistics").
		WithArgs("customers").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"index_name",
				"non_unique",
				"column_name",
				"expression",
				"sub_part",
				"collation",
				"index_type",
			}).
				AddRow("PRIMARY", 0, "id", nil, nil, "A", "BTREE").
				AddRow("uq_customers_email", 0, "email", nil, nil, "A", "BTREE"),
		)

	// 5. Customers has no foreign keys.
	mock.ExpectQuery("FROM information_schema.key_column_usage").
		WithArgs("customers").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"constraint_name",
				"column_name",
				"referenced_table_name",
				"referenced_column_name",
				"update_rule",
				"delete_rule",
			}),
		)

	// 6. Mock orders columns.
	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("orders").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"column_name",
				"data_type",
				"column_type",
				"is_nullable",
				"column_default",
				"column_key",
				"extra",
				"ordinal_position",
			}).
				AddRow(
					"id", "bigint", "bigint",
					"NO", nil, "PRI", "auto_increment", 1,
				).
				AddRow(
					"customer_id", "bigint", "bigint",
					"NO", nil, "MUL", "", 2,
				).
				AddRow(
					"total_amount", "decimal", "decimal(10,2)",
					"NO", nil, "", "", 3,
				),
		)

	// 7. Mock orders indexes.
	mock.ExpectQuery("FROM information_schema.statistics").
		WithArgs("orders").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"index_name",
				"non_unique",
				"column_name",
				"expression",
				"sub_part",
				"collation",
				"index_type",
			}).
				AddRow("PRIMARY", 0, "id", nil, nil, "A", "BTREE").
				AddRow("idx_orders_customer", 1, "customer_id", nil, nil, "A", "BTREE"),
		)

	// 8. Mock orders foreign key.
	mock.ExpectQuery("FROM information_schema.key_column_usage").
		WithArgs("orders").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"constraint_name",
				"column_name",
				"referenced_table_name",
				"referenced_column_name",
				"update_rule",
				"delete_rule",
			}).
				AddRow(
					"fk_orders_customers",
					"customer_id",
					"customers",
					"id",
					"CASCADE",
					"RESTRICT",
				),
		)

	// 9. Execute the complete schema discovery pipeline.
	schema, err := DiscoverSchema(context.Background(), db)
	if err != nil {
		t.Fatalf("DiscoverSchema returned error: %v", err)
	}

	// 10. Verify database name.
	if schema.Database != "test_db" {
		t.Errorf(
			"expected database test_db, got %q",
			schema.Database,
		)
	}

	// 11. Verify table discovery.
	if len(schema.Tables) != 2 {
		t.Fatalf(
			"expected 2 tables, got %d",
			len(schema.Tables),
		)
	}

	customers := schema.Tables[0]
	orders := schema.Tables[1]

	if customers.Name != "customers" {
		t.Errorf("expected customers, got %q", customers.Name)
	}

	if orders.Name != "orders" {
		t.Errorf("expected orders, got %q", orders.Name)
	}

	// 12. Verify table properties.
	for _, table := range schema.Tables {
		if table.Type != "BASE TABLE" {
			t.Errorf(
				"unexpected table type for %s: %s",
				table.Name,
				table.Type,
			)
		}

		if table.Engine != "InnoDB" {
			t.Errorf(
				"unexpected engine for %s: %s",
				table.Name,
				table.Engine,
			)
		}

		if table.Charset != "utf8mb4" {
			t.Errorf(
				"unexpected charset for %s: %s",
				table.Name,
				table.Charset,
			)
		}

		if table.Collation != "utf8mb4_0900_ai_ci" {
			t.Errorf(
				"unexpected collation for %s: %s",
				table.Name,
				table.Collation,
			)
		}
	}

	// 13. Verify customers columns.
	if len(customers.Columns) != 3 {
		t.Fatalf(
			"expected 3 customers columns, got %d",
			len(customers.Columns),
		)
	}

	if customers.Columns[0].Name != "id" ||
		customers.Columns[0].OrdinalPosition != 1 {
		t.Error("customers primary key column metadata is incorrect")
	}

	if customers.Columns[2].Name != "email" ||
		!customers.Columns[2].Nullable {
		t.Error("customers email column metadata is incorrect")
	}

	// 14. Verify customers indexes.
	if len(customers.Indexes) != 2 {
		t.Fatalf(
			"expected 2 customers indexes, got %d",
			len(customers.Indexes),
		)
	}

	if !customers.Indexes[0].Primary {
		t.Error("expected customers PRIMARY index")
	}

	if !customers.Indexes[1].Unique ||
		customers.Indexes[1].Name != "uq_customers_email" {
		t.Error("customers unique email index metadata is incorrect")
	}

	// 15. Verify orders columns.
	if len(orders.Columns) != 3 {
		t.Fatalf(
			"expected 3 orders columns, got %d",
			len(orders.Columns),
		)
	}

	if orders.Columns[1].Name != "customer_id" {
		t.Errorf(
			"expected customer_id, got %q",
			orders.Columns[1].Name,
		)
	}

	// 16. Verify orders indexes.
	if len(orders.Indexes) != 2 {
		t.Fatalf(
			"expected 2 orders indexes, got %d",
			len(orders.Indexes),
		)
	}

	if orders.Indexes[1].Name != "idx_orders_customer" ||
		orders.Indexes[1].Unique {
		t.Error("orders secondary index metadata is incorrect")
	}

	// 17. Verify orders foreign key.
	if len(orders.ForeignKeys) != 1 {
		t.Fatalf(
			"expected 1 orders foreign key, got %d",
			len(orders.ForeignKeys),
		)
	}

	fk := orders.ForeignKeys[0]

	if fk.Name != "fk_orders_customers" {
		t.Errorf("unexpected FK name: %q", fk.Name)
	}

	if fk.ReferencedTable != "customers" {
		t.Errorf(
			"unexpected referenced table: %q",
			fk.ReferencedTable,
		)
	}

	if len(fk.Columns) != 1 ||
		fk.Columns[0] != "customer_id" {
		t.Errorf("unexpected FK columns: %v", fk.Columns)
	}

	if len(fk.ReferencedColumns) != 1 ||
		fk.ReferencedColumns[0] != "id" {
		t.Errorf(
			"unexpected referenced columns: %v",
			fk.ReferencedColumns,
		)
	}

	if fk.UpdateRule != "CASCADE" ||
		fk.DeleteRule != "RESTRICT" {
		t.Error("foreign key update/delete rules are incorrect")
	}

	// 18. Verify all mocked queries were executed.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverSchemaViewWithNullMetadata(t *testing.T) {
	db, mock := newSchemaMock(t)

	// 1. Mock database name.
	mock.ExpectQuery("SELECT DATABASE\\(\\)").
		WillReturnRows(
			sqlmock.NewRows([]string{"DATABASE()"}).
				AddRow("test_db"),
		)

	// 2. Mock a view with NULL engine, collation and charset.
	mock.ExpectQuery("FROM information_schema.tables").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"table_name",
				"table_type",
				"engine",
				"table_collation",
				"character_set_name",
			}).
				AddRow(
					"customer_summary",
					"VIEW",
					nil,
					nil,
					nil,
				),
		)

	// 3. Mock view columns.
	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("customer_summary").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"column_name",
				"data_type",
				"column_type",
				"is_nullable",
				"column_default",
				"column_key",
				"extra",
				"ordinal_position",
			}).
				AddRow(
					"customer_count",
					"bigint",
					"bigint",
					"NO",
					nil,
					"",
					"",
					1,
				),
		)

	// 4. Views have no indexes in this fixture.
	mock.ExpectQuery("FROM information_schema.statistics").
		WithArgs("customer_summary").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"index_name",
				"non_unique",
				"column_name",
				"expression",
				"sub_part",
				"collation",
				"index_type",
			}),
		)

	// 5. Views have no foreign keys in this fixture.
	mock.ExpectQuery("FROM information_schema.key_column_usage").
		WithArgs("customer_summary").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"constraint_name",
				"column_name",
				"referenced_table_name",
				"referenced_column_name",
				"update_rule",
				"delete_rule",
			}),
		)

	// 6. Run complete schema discovery.
	schema, err := DiscoverSchema(context.Background(), db)
	if err != nil {
		t.Fatalf("DiscoverSchema returned error: %v", err)
	}

	if schema.Database != "test_db" {
		t.Errorf("unexpected database name: %q", schema.Database)
	}

	if len(schema.Tables) != 1 {
		t.Fatalf("expected 1 view, got %d objects", len(schema.Tables))
	}

	view := schema.Tables[0]

	// Verify view identity.
	if view.Name != "customer_summary" {
		t.Errorf("unexpected view name: %q", view.Name)
	}

	if view.Type != "VIEW" {
		t.Errorf("expected VIEW, got %q", view.Type)
	}

	// NULL table properties should become empty strings.
	if view.Engine != "" {
		t.Errorf("expected empty engine, got %q", view.Engine)
	}

	if view.Collation != "" {
		t.Errorf("expected empty collation, got %q", view.Collation)
	}

	if view.Charset != "" {
		t.Errorf("expected empty charset, got %q", view.Charset)
	}

	// Verify the view's column metadata.
	if len(view.Columns) != 1 {
		t.Fatalf("expected 1 view column, got %d", len(view.Columns))
	}

	column := view.Columns[0]

	if column.Name != "customer_count" {
		t.Errorf("unexpected column name: %q", column.Name)
	}

	if column.DefaultValue != nil {
		t.Errorf(
			"expected nil default value, got %q",
			*column.DefaultValue,
		)
	}

	if len(view.Indexes) != 0 {
		t.Errorf("expected 0 indexes, got %d", len(view.Indexes))
	}

	if len(view.ForeignKeys) != 0 {
		t.Errorf("expected 0 foreign keys, got %d", len(view.ForeignKeys))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverColumnsNullDefault(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{
		Name:    "products",
		Columns: make([]Column, 0),
	}

	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("products").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"column_name",
				"data_type",
				"column_type",
				"is_nullable",
				"column_default",
				"column_key",
				"extra",
				"ordinal_position",
			}).
				AddRow(
					"id",
					"bigint",
					"bigint",
					"NO",
					nil,
					"PRI",
					"auto_increment",
					1,
				).
				AddRow(
					"stock",
					"int",
					"int",
					"YES",
					nil,
					"",
					"",
					2,
				),
		)

	err := discoverColumns(context.Background(), db, table)
	if err != nil {
		t.Fatalf("discoverColumns returned error: %v", err)
	}

	if len(table.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(table.Columns))
	}

	for _, column := range table.Columns {
		if column.DefaultValue != nil {
			t.Errorf(
				"expected nil default for column %s, got %q",
				column.Name,
				*column.DefaultValue,
			)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverFunctionalIndexes(t *testing.T) {
	db, mock := newSchemaMock(t)

	table := &Table{
		Name:    "users",
		Indexes: make([]Index, 0),
	}

	mock.ExpectQuery("FROM information_schema.statistics").
		WithArgs("users").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"index_name",
				"non_unique",
				"column_name",
				"expression",
				"sub_part",
				"collation",
				"index_type",
			}).
				// Ordinary column in a mixed index.
				AddRow("idx_tenant_email", 1, "tenant_id", nil, nil, "A", "BTREE").
				// Functional part of the same index.
				AddRow("idx_tenant_email", 1, nil, "lower(email)", nil, "A", "BTREE").
				// A functional-only index.
				AddRow("idx_email_lower", 0, nil, "lower(email)", nil, "A", "BTREE"),
		)

	err := discoverIndexes(context.Background(), db, table)
	if err != nil {
		t.Fatalf("discoverIndexes returned error: %v", err)
	}

	if len(table.Indexes) != 2 {
		t.Fatalf("expected 2 indexes, got %d", len(table.Indexes))
	}

	mixed := table.Indexes[0]

	if mixed.Name != "idx_tenant_email" {
		t.Errorf("unexpected index name: %q", mixed.Name)
	}

	if mixed.Unique {
		t.Error("expected mixed index to be non-unique")
	}

	// Backward-compatible Columns contains ordinary columns only.
	if len(mixed.Columns) != 1 || mixed.Columns[0] != "tenant_id" {
		t.Errorf("unexpected mixed index columns: %v", mixed.Columns)
	}

	// Parts retains both index parts and their original order.
	if len(mixed.Parts) != 2 {
		t.Fatalf("expected 2 mixed index parts, got %d", len(mixed.Parts))
	}

	if mixed.Parts[0].Column != "tenant_id" {
		t.Errorf("unexpected first index part: %+v", mixed.Parts[0])
	}

	if mixed.Parts[1].Expression != "lower(email)" {
		t.Errorf("unexpected second index part: %+v", mixed.Parts[1])
	}

	functional := table.Indexes[1]

	if functional.Name != "idx_email_lower" {
		t.Errorf("unexpected functional index name: %q", functional.Name)
	}

	if !functional.Unique {
		t.Error("expected functional index to be unique")
	}

	if len(functional.Columns) != 0 {
		t.Errorf("expected no ordinary columns, got %v", functional.Columns)
	}

	if len(functional.Parts) != 1 {
		t.Fatalf("expected 1 functional index part, got %d", len(functional.Parts))
	}

	if functional.Parts[0].Expression != "lower(email)" {
		t.Errorf(
			"unexpected functional expression: %q",
			functional.Parts[0].Expression,
		)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet SQL expectations: %v", err)
	}
}

func TestDiscoverColumnsScanError(t *testing.T) {
	db, mock := newSchemaMock(t)
	table := &Table{Name: "customers"}

	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("customers").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"column_name",
				"data_type",
				"column_type",
				"is_nullable",
				"column_default",
				"column_key",
				"extra",
				"ordinal_position",
			}).AddRow(
				"id",
				"bigint",
				"bigint",
				"NO",
				nil,
				"PRI",
				"auto_increment",
				"invalid-position",
			),
		)

	err := discoverColumns(context.Background(), db, table)

	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

func TestDiscoverColumnsIterationError(t *testing.T) {
	db, mock := newSchemaMock(t)
	table := &Table{Name: "customers"}

	iterationErr := errors.New("simulated column iteration failure")

	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("customers").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"column_name",
				"data_type",
				"column_type",
				"is_nullable",
				"column_default",
				"column_key",
				"extra",
				"ordinal_position",
			}).
				AddRow(
					"id", "bigint", "bigint",
					"NO", nil, "PRI", "auto_increment", 1,
				).
				AddRow(
					"name", "varchar", "varchar(100)",
					"NO", nil, "", "", 2,
				).
				RowError(1, iterationErr),
		)

	err := discoverColumns(context.Background(), db, table)

	if !errors.Is(err, iterationErr) {
		t.Fatalf("expected iteration error %v, got %v", iterationErr, err)
	}
}

func TestDiscoverIndexesScanError(t *testing.T) {
	db, mock := newSchemaMock(t)
	table := &Table{Name: "customers"}

	mock.ExpectQuery("FROM information_schema.statistics").
		WithArgs("customers").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"index_name",
				"non_unique",
				"column_name",
				"expression",
				"sub_part",
				"collation",
				"index_type",
			}).AddRow(
				"idx_email",
				"invalid-value",
				"email",
				nil,
				nil,
				"A",
				"BTREE",
			),
		)

	err := discoverIndexes(context.Background(), db, table)

	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

func TestDiscoverIndexesIterationError(t *testing.T) {
	db, mock := newSchemaMock(t)
	table := &Table{Name: "customers"}

	iterationErr := errors.New("simulated index iteration failure")

	mock.ExpectQuery("FROM information_schema.statistics").
		WithArgs("customers").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"index_name",
				"non_unique",
				"column_name",
				"expression",
				"sub_part",
				"collation",
				"index_type",
			}).
				AddRow("idx_email", 1, "email", nil, nil, "A", "BTREE").
				AddRow("idx_name", 1, "name", nil, nil, "A", "BTREE").
				RowError(1, iterationErr),
		)

	err := discoverIndexes(context.Background(), db, table)

	if !errors.Is(err, iterationErr) {
		t.Fatalf("expected iteration error %v, got %v", iterationErr, err)
	}
}

func TestDiscoverForeignKeysScanError(t *testing.T) {
	db, mock := newSchemaMock(t)
	table := &Table{Name: "orders"}

	mock.ExpectQuery("FROM information_schema.key_column_usage").
		WithArgs("orders").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"constraint_name",
				"column_name",
				"referenced_table_name",
				"referenced_column_name",
				"update_rule",
				"delete_rule",
			}).AddRow(
				"fk_orders_customers",
				"customer_id",
				"customers",
				nil, // NULL cannot be scanned into a Go string.
				"CASCADE",
				"RESTRICT",
			),
		)

	err := discoverForeignKeys(context.Background(), db, table)

	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

func TestDiscoverForeignKeysIterationError(t *testing.T) {
	db, mock := newSchemaMock(t)
	table := &Table{Name: "orders"}

	iterationErr := errors.New("simulated foreign key iteration failure")

	mock.ExpectQuery("FROM information_schema.key_column_usage").
		WithArgs("orders").
		WillReturnRows(
			sqlmock.NewRows([]string{
				"constraint_name",
				"column_name",
				"referenced_table_name",
				"referenced_column_name",
				"update_rule",
				"delete_rule",
			}).
				AddRow(
					"fk_orders_customers",
					"customer_id",
					"customers",
					"id",
					"CASCADE",
					"RESTRICT",
				).
				AddRow(
					"fk_orders_products",
					"product_id",
					"products",
					"id",
					"CASCADE",
					"RESTRICT",
				).
				RowError(1, iterationErr),
		)

	err := discoverForeignKeys(context.Background(), db, table)

	if !errors.Is(err, iterationErr) {
		t.Fatalf("expected iteration error %v, got %v", iterationErr, err)
	}
}

func TestDiscoverSchemaContextDeadlineExceeded(t *testing.T) {
	db, mock := newSchemaMock(t)

	mock.ExpectQuery("SELECT DATABASE\\(\\)").
		WillDelayFor(100 * time.Millisecond).
		WillReturnRows(
			sqlmock.NewRows([]string{"database"}).
				AddRow("test_db"),
		)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Millisecond,
	)
	defer cancel()

	_, err := DiscoverSchema(ctx, db)

	if err == nil {
		t.Fatal("expected an error when context deadline is exceeded")
	}

	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf(
			"expected context deadline exceeded, got %v",
			ctx.Err(),
		)
	}
}
