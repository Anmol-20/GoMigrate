package migrationplan

import (
	"strings"
	"testing"

	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
	"github.com/Anmol-20/gomigrate/internal/schemadiff"
)

func TestCreateTableSQLIncludesTableProperties(t *testing.T) {
	table := mysqlclient.Table{
		Name:      "customers",
		Type:      "BASE TABLE",
		Engine:    "InnoDB",
		Charset:   "utf8mb4",
		Collation: "utf8mb4_0900_ai_ci",
		Columns: []mysqlclient.Column{
			{
				Name:       "id",
				ColumnType: "bigint",
				Nullable:   false,
			},
		},
	}

	sql, err := createTableSQL(table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci"

	if !strings.Contains(sql, expected) {
		t.Errorf("expected SQL to contain %q, got %q", expected, sql)
	}
}

func TestCreateTableSQLRejectsView(t *testing.T) {
	table := mysqlclient.Table{
		Name: "customer_view",
		Type: "VIEW",
	}

	_, err := createTableSQL(table)
	if err == nil {
		t.Fatal("expected an error for unsupported VIEW")
	}
}

func TestCreateTableSQLRejectsInvalidEngine(t *testing.T) {
	table := mysqlclient.Table{
		Name:      "customers",
		Type:      "BASE TABLE",
		Engine:    "InnoDB; DROP TABLE users",
		Charset:   "utf8mb4",
		Collation: "utf8mb4_0900_ai_ci",
		Columns: []mysqlclient.Column{
			{Name: "id", ColumnType: "bigint"},
		},
	}

	_, err := createTableSQL(table)
	if err == nil {
		t.Fatal("expected an error for invalid engine")
	}
}

func TestGenerateCreateTable(t *testing.T) {
	source := &mysqlclient.Schema{
		Database: "source_db",
		Tables: []mysqlclient.Table{
			{
				Name:      "customers",
				Type:      "BASE TABLE",
				Engine:    "InnoDB",
				Charset:   "utf8mb4",
				Collation: "utf8mb4_0900_ai_ci",

				Columns: []mysqlclient.Column{
					{
						Name:       "id",
						DataType:   "bigint",
						ColumnType: "bigint",
						Nullable:   false,
						Extra:      "auto_increment",
					},
					{
						Name:       "name",
						DataType:   "varchar",
						ColumnType: "varchar(100)",
						Nullable:   false,
					},
				},
				Indexes: []mysqlclient.Index{
					{
						Name:    "PRIMARY",
						Primary: true,
						Unique:  true,
						Columns: []string{"id"},
					},
				},
			},
		},
	}

	comparison := &schemadiff.Result{
		SourceDatabase:   "source_db",
		TargetDatabase:   "target_db",
		SourceOnlyTables: []string{"customers"},
		TargetOnlyTables: []string{},
		CommonTables:     []schemadiff.TableComparison{},
	}

	generator := NewGenerator()

	plan, err := generator.Generate(source, comparison)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(plan.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(plan.Operations))
	}

	operation := plan.Operations[0]

	if operation.Type != "CREATE_TABLE" {
		t.Errorf("expected CREATE_TABLE, got %s", operation.Type)
	}

	expectedSQLParts := []string{
		"CREATE TABLE `customers`",
		"`id` bigint NOT NULL AUTO_INCREMENT",
		"`name` varchar(100) NOT NULL",
		"PRIMARY KEY (`id`)",
	}

	for _, part := range expectedSQLParts {
		if !strings.Contains(operation.SQL, part) {
			t.Errorf("expected SQL to contain %q; got:\n%s", part, operation.SQL)
		}
	}
}

func TestGenerateAddMissingColumn(t *testing.T) {
	source := &mysqlclient.Schema{
		Database: "source_db",
		Tables: []mysqlclient.Table{
			{
				Name: "customers",
				Columns: []mysqlclient.Column{
					{
						Name:       "id",
						DataType:   "bigint",
						ColumnType: "bigint",
						Nullable:   false,
					},
					{
						Name:       "email",
						DataType:   "varchar",
						ColumnType: "varchar(255)",
						Nullable:   true,
					},
				},
			},
		},
	}

	comparison := &schemadiff.Result{
		SourceDatabase: "source_db",
		TargetDatabase: "target_db",
		CommonTables: []schemadiff.TableComparison{
			{
				Name:                   "customers",
				MissingColumnsInTarget: []string{"email"},
			},
		},
	}

	plan, err := NewGenerator().Generate(source, comparison)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(plan.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(plan.Operations))
	}

	expected := "ALTER TABLE `customers` ADD COLUMN `email` varchar(255) NULL"

	if plan.Operations[0].SQL != expected {
		t.Errorf(
			"unexpected SQL:\n got: %s\nwant: %s",
			plan.Operations[0].SQL,
			expected,
		)
	}

	if plan.Operations[0].Type != "ADD_COLUMN" {
		t.Errorf("expected ADD_COLUMN, got %s", plan.Operations[0].Type)
	}
}

func TestGenerateDoesNotDropTargetOnlyTables(t *testing.T) {
	source := &mysqlclient.Schema{
		Database: "source_db",
	}

	comparison := &schemadiff.Result{
		SourceDatabase:   "source_db",
		TargetDatabase:   "target_db",
		SourceOnlyTables: []string{},
		TargetOnlyTables: []string{"audit_logs"},
		CommonTables:     []schemadiff.TableComparison{},
	}

	plan, err := NewGenerator().Generate(source, comparison)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(plan.Operations) != 0 {
		t.Errorf("expected no operations, got %d", len(plan.Operations))
	}

	if len(plan.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(plan.Warnings))
	}

	if !strings.Contains(plan.Warnings[0], "audit_logs") {
		t.Errorf("expected warning about audit_logs, got %s", plan.Warnings[0])
	}
}

func TestGenerateRejectsNilInput(t *testing.T) {
	_, err := NewGenerator().Generate(nil, &schemadiff.Result{})

	if err == nil {
		t.Fatal("expected error for nil source schema")
	}
}

func TestCreateCompositeForeignKeySQL(t *testing.T) {
	fk := mysqlclient.ForeignKey{
		Name:              "fk_order_product",
		Columns:           []string{"order_id", "product_id"},
		ReferencedTable:   "order_products",
		ReferencedColumns: []string{"order_id", "product_id"},
		UpdateRule:        "CASCADE",
		DeleteRule:        "RESTRICT",
	}

	sql, err := createForeignKeySQL("order_items", fk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "ALTER TABLE `order_items` ADD CONSTRAINT `fk_order_product` " +
		"FOREIGN KEY (`order_id`, `product_id`) " +
		"REFERENCES `order_products` (`order_id`, `product_id`) " +
		"ON UPDATE CASCADE ON DELETE RESTRICT"

	if sql != expected {
		t.Errorf(
			"unexpected SQL:\n got: %s\nwant: %s",
			sql,
			expected,
		)
	}
}

func TestCreateSingleColumnForeignKeySQL(t *testing.T) {
	fk := mysqlclient.ForeignKey{
		Name:             "fk_orders_customers",
		Column:           "customer_id",
		ReferencedTable:  "customers",
		ReferencedColumn: "id",
		UpdateRule:       "NO ACTION",
		DeleteRule:       "CASCADE",
	}

	sql, err := createForeignKeySQL("orders", fk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "ALTER TABLE `orders` ADD CONSTRAINT `fk_orders_customers` " +
		"FOREIGN KEY (`customer_id`) " +
		"REFERENCES `customers` (`id`) " +
		"ON UPDATE NO ACTION ON DELETE CASCADE"

	if sql != expected {
		t.Errorf(
			"unexpected SQL:\n got: %s\nwant: %s",
			sql,
			expected,
		)
	}
}

func TestCreateForeignKeySQLRejectsMismatchedColumns(t *testing.T) {
	fk := mysqlclient.ForeignKey{
		Name:              "fk_order_product",
		Columns:           []string{"order_id", "product_id"},
		ReferencedTable:   "order_products",
		ReferencedColumns: []string{"order_id"},
		UpdateRule:        "CASCADE",
		DeleteRule:        "RESTRICT",
	}

	_, err := createForeignKeySQL("order_items", fk)

	if err == nil {
		t.Fatal("expected error for mismatched foreign key column counts")
	}
}

func TestCreateForeignKeySQLRejectsEmptyColumns(t *testing.T) {
	fk := mysqlclient.ForeignKey{
		Name:              "fk_order_product",
		Columns:           []string{},
		ReferencedTable:   "order_products",
		ReferencedColumns: []string{"order_id"},
		UpdateRule:        "CASCADE",
		DeleteRule:        "RESTRICT",
	}

	_, err := createForeignKeySQL("order_items", fk)

	if err == nil {
		t.Fatal("expected error for empty foreign key columns")
	}
}

func TestCreateIndexSQLFunctionalIndex(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_lower_email",
		Parts: []mysqlclient.IndexPart{
			{Expression: "LOWER(email)"},
		},
	}

	sql, err := createIndexSQL("customers", index)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "CREATE INDEX `idx_lower_email` ON `customers` ((LOWER(email)))"
	if sql != expected {
		t.Errorf("unexpected SQL:\n got: %s\nwant: %s", sql, expected)
	}
}

func TestCreateIndexSQLMixedIndexParts(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_tenant_email",
		Parts: []mysqlclient.IndexPart{
			{Column: "tenant_id"},
			{Expression: "LOWER(email)"},
		},
	}

	sql, err := createIndexSQL("customers", index)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "CREATE INDEX `idx_tenant_email` ON `customers` (`tenant_id`, (LOWER(email)))"
	if sql != expected {
		t.Errorf("unexpected SQL:\n got: %s\nwant: %s", sql, expected)
	}
}

func TestCreateIndexSQLUsesLegacyColumns(t *testing.T) {
	index := mysqlclient.Index{
		Name:    "idx_email",
		Columns: []string{"email"},
	}

	sql, err := createIndexSQL("customers", index)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "CREATE INDEX `idx_email` ON `customers` (`email`)"
	if sql != expected {
		t.Errorf("unexpected SQL:\n got: %s\nwant: %s", sql, expected)
	}
}

func TestIndexDefinitionFunctionalIndex(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_lower_email",
		Parts: []mysqlclient.IndexPart{
			{Expression: "LOWER(email)"},
		},
	}

	definition, err := indexDefinition(index)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "KEY `idx_lower_email` ((LOWER(email)))"
	if definition != expected {
		t.Errorf(
			"unexpected definition:\n got: %s\nwant: %s",
			definition,
			expected,
		)
	}
}

func TestCreateUniqueFunctionalIndexSQL(t *testing.T) {
	index := mysqlclient.Index{
		Name:   "idx_unique_lower_email",
		Unique: true,
		Parts: []mysqlclient.IndexPart{
			{Expression: "LOWER(email)"},
		},
	}

	sql, err := createIndexSQL("customers", index)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "CREATE UNIQUE INDEX `idx_unique_lower_email` ON `customers` ((LOWER(email)))"
	if sql != expected {
		t.Errorf("unexpected SQL:\n got: %s\nwant: %s", sql, expected)
	}
}

func TestCreateIndexSQLMultipleFunctionalParts(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_customer_expressions",
		Parts: []mysqlclient.IndexPart{
			{Expression: "LOWER(email)"},
			{Expression: "YEAR(created_at)"},
		},
	}

	sql, err := createIndexSQL("customers", index)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "CREATE INDEX `idx_customer_expressions` ON `customers` ((LOWER(email)), (YEAR(created_at)))"
	if sql != expected {
		t.Errorf("unexpected SQL:\n got: %s\nwant: %s", sql, expected)
	}
}

func TestCreateIndexSQLRejectsEmptyParts(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_invalid",
		Parts: []mysqlclient.IndexPart{
			{},
		},
	}

	_, err := createIndexSQL("customers", index)
	if err == nil {
		t.Fatal("expected error for empty index part")
	}
}

func TestCreateIndexSQLRejectsPartWithColumnAndExpression(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_invalid",
		Parts: []mysqlclient.IndexPart{
			{
				Column:     "email",
				Expression: "LOWER(email)",
			},
		},
	}

	_, err := createIndexSQL("customers", index)
	if err == nil {
		t.Fatal("expected error for index part containing both column and expression")
	}
}

func TestValidateIndexExpression(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		wantErr    bool
	}{
		{
			name:       "lowercase function",
			expression: "LOWER(email)",
		},
		{
			name:       "uppercase function",
			expression: "UPPER(email)",
		},
		{
			name:       "year function",
			expression: "YEAR(created_at)",
		},
		{
			name:       "absolute value function",
			expression: "ABS(amount)",
		},
		{
			name:       "quoted column identifier",
			expression: "LOWER(`email`)",
		},
		{
			name:       "unknown function",
			expression: "SLEEP(10)",
			wantErr:    true,
		},
		{
			name:       "file access function",
			expression: "LOAD_FILE('/etc/passwd')",
			wantErr:    true,
		},
		{
			name:       "multiple SQL statements",
			expression: "LOWER(email)); DROP TABLE customers; --",
			wantErr:    true,
		},
		{
			name:       "SQL comment",
			expression: "LOWER(email) /* comment */",
			wantErr:    true,
		},
		{
			name:       "empty expression",
			expression: "",
			wantErr:    true,
		},
		{
			name:       "plain column without function",
			expression: "email",
			wantErr:    true,
		},
		{
			name:       "nested lowercase function",
			expression: "LOWER(LOWER(email))",
		},
		{
			name:       "nested uppercase function",
			expression: "UPPER(LOWER(email))",
		},
		{
			name:       "nested function with quoted column",
			expression: "LOWER(UPPER(`email`))",
		},
		{
			name:       "unknown nested function",
			expression: "LOWER(SLEEP(10))",
			wantErr:    true,
		},
		{
			name:       "missing closing parenthesis",
			expression: "LOWER(email",
			wantErr:    true,
		},
		{
			name:       "extra closing parenthesis",
			expression: "LOWER(email))",
			wantErr:    true,
		},
		{
			name:       "multiple arguments",
			expression: "LOWER(email, name)",
			wantErr:    true,
		},
		{
			name:       "arithmetic expression",
			expression: "ABS(amount + 10)",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIndexExpression(tt.expression)

			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidateIndexExpressionColumns(t *testing.T) {
	tableColumns := []mysqlclient.Column{
		{Name: "id"},
		{Name: "email"},
		{Name: "created_at"},
		{Name: "amount"},
	}

	tests := []struct {
		name       string
		expression string
		wantErr    bool
	}{
		{
			name:       "existing column",
			expression: "LOWER(email)",
		},
		{
			name:       "existing column with different case",
			expression: "LOWER(EMAIL)",
		},
		{
			name:       "nested expression with existing column",
			expression: "LOWER(UPPER(email))",
		},
		{
			name:       "unknown column",
			expression: "LOWER(username)",
			wantErr:    true,
		},
		{
			name:       "unknown column in nested expression",
			expression: "LOWER(UPPER(username))",
			wantErr:    true,
		},
		{
			name:       "unknown column in year expression",
			expression: "YEAR(updated_at)",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIndexExpressionColumns(
				tt.expression,
				tableColumns,
			)

			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestCreateIndexSQLRejectsUnknownExpressionColumn(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_unknown_email",
		Parts: []mysqlclient.IndexPart{
			{Expression: "LOWER(username)"},
		},
	}

	tableColumns := []mysqlclient.Column{
		{Name: "id"},
		{Name: "email"},
	}

	_, err := createIndexSQL("customers", index, tableColumns)

	if err == nil {
		t.Fatal("expected error for unknown expression column")
	}
}

func TestCreateTableSQLRejectsUnknownExpressionColumn(t *testing.T) {
	table := mysqlclient.Table{
		Name:   "customers",
		Type:   "BASE TABLE",
		Engine: "InnoDB",
		Columns: []mysqlclient.Column{
			{Name: "id", ColumnType: "bigint"},
			{Name: "email", ColumnType: "varchar(255)"},
		},
		Indexes: []mysqlclient.Index{
			{
				Name: "idx_unknown_email",
				Parts: []mysqlclient.IndexPart{
					{Expression: "LOWER(username)"},
				},
			},
		},
	}

	_, err := createTableSQL(table)

	if err == nil {
		t.Fatal("expected error for unknown expression column")
	}
}

func TestCreateIndexSQLValidatesMultipleExpressionColumns(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_customer_expressions",
		Parts: []mysqlclient.IndexPart{
			{Expression: "LOWER(email)"},
			{Expression: "YEAR(created_at)"},
		},
	}

	tableColumns := []mysqlclient.Column{
		{Name: "email"},
		{Name: "created_at"},
	}

	sql, err := createIndexSQL("customers", index, tableColumns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "CREATE INDEX `idx_customer_expressions` ON `customers` ((LOWER(email)), (YEAR(created_at)))"

	if sql != expected {
		t.Errorf("unexpected SQL:\n got: %s\nwant: %s", sql, expected)
	}
}

func TestIndexPartsSQLPrefixLength(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_email",
		Parts: []mysqlclient.IndexPart{
			{Column: "email", PrefixLength: 100},
		},
	}

	got, err := indexPartsSQL(index)
	if err != nil {
		t.Fatal(err)
	}

	want := "`email`(100)"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestIndexPartsSQLDescending(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_created",
		Parts: []mysqlclient.IndexPart{
			{Column: "created_at", Order: "DESC"},
		},
	}

	got, err := indexPartsSQL(index)
	if err != nil {
		t.Fatal(err)
	}

	want := "`created_at` DESC"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCreateIndexSQLFullText(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_content",
		Type: "FULLTEXT",
		Parts: []mysqlclient.IndexPart{
			{Column: "content"},
		},
	}

	got, err := createIndexSQL("articles", index)
	if err != nil {
		t.Fatal(err)
	}

	want := "CREATE FULLTEXT INDEX `idx_content` ON `articles` (`content`)"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCreateIndexSQLSpatial(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_location",
		Type: "RTREE",
		Parts: []mysqlclient.IndexPart{
			{Column: "location"},
		},
	}

	got, err := createIndexSQL("places", index)
	if err != nil {
		t.Fatal(err)
	}

	want := "CREATE SPATIAL INDEX `idx_location` ON `places` (`location`)"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestIndexPartsSQLRejectsExpressionPrefix(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_email",
		Parts: []mysqlclient.IndexPart{
			{
				Expression:   "LOWER(email)",
				PrefixLength: 10,
			},
		},
	}

	if _, err := indexPartsSQL(index); err == nil {
		t.Fatal("expected functional expression prefix to be rejected")
	}
}

func TestIndexPartsSQLRejectsInvalidSortDirection(t *testing.T) {
	index := mysqlclient.Index{
		Name: "idx_email",
		Parts: []mysqlclient.IndexPart{
			{Column: "email", Order: "SIDEWAYS"},
		},
	}

	if _, err := indexPartsSQL(index); err == nil {
		t.Fatal("expected invalid sort direction to be rejected")
	}
}
