package migrationplan

import "testing"

func TestValidateOperationSQLAllowsForeignKeyDeleteActions(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "cascade",
			sql: "ALTER TABLE `orders` " +
				"ADD CONSTRAINT `fk_orders_customers` " +
				"FOREIGN KEY (`customer_id`) " +
				"REFERENCES `customers` (`id`) " +
				"ON DELETE CASCADE",
		},
		{
			name: "set null",
			sql: "ALTER TABLE `orders` " +
				"ADD CONSTRAINT `fk_orders_customers` " +
				"FOREIGN KEY (`customer_id`) " +
				"REFERENCES `customers` (`id`) " +
				"ON DELETE SET NULL",
		},
		{
			name: "restrict",
			sql: "ALTER TABLE `orders` " +
				"ADD CONSTRAINT `fk_orders_customers` " +
				"FOREIGN KEY (`customer_id`) " +
				"REFERENCES `customers` (`id`) " +
				"ON DELETE RESTRICT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation := Operation{
				Type: "ADD_FOREIGN_KEY",
				SQL:  tt.sql,
			}

			if err := validateOperationSQL(operation); err != nil {
				t.Fatalf("expected SQL to be allowed, got error: %v", err)
			}
		})
	}
}

func TestValidateOperationSQLRejectsDestructiveStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "delete",
			sql:  "DELETE FROM `customers`",
		},
		{
			name: "drop",
			sql:  "DROP TABLE `customers`",
		},
		{
			name: "truncate",
			sql:  "TRUNCATE TABLE `customers`",
		},
		{
			name: "replace",
			sql:  "REPLACE INTO `customers` VALUES (1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation := Operation{
				Type: "CREATE_TABLE",
				SQL:  tt.sql,
			}

			if err := validateOperationSQL(operation); err == nil {
				t.Fatal("expected destructive SQL to be rejected")
			}
		})
	}
}
