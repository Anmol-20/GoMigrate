package migrationplan

import "testing"

func TestClassifyOperation(t *testing.T) {
	tests := []struct {
		name           string
		operationType  string
		expectedRisk   string
		expectedReview bool
	}{
		{"create table", "CREATE_TABLE", RiskMedium, false},
		{"add column", "ADD_COLUMN", RiskMedium, false},
		{"modify column", "MODIFY_COLUMN", RiskHigh, true},
		{"add index", "ADD_INDEX", RiskMedium, false},
		{"add foreign key", "ADD_FOREIGN_KEY", RiskHigh, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation := Operation{Type: tt.operationType}

			classifyOperation(&operation)

			if operation.RiskLevel != tt.expectedRisk {
				t.Errorf("expected risk %s, got %s",
					tt.expectedRisk, operation.RiskLevel)
			}

			if operation.RequiresReview != tt.expectedReview {
				t.Errorf("expected RequiresReview=%v, got %v",
					tt.expectedReview, operation.RequiresReview)
			}
		})
	}
}

func TestValidateOperationSQL(t *testing.T) {
	tests := []struct {
		name      string
		operation Operation
		wantErr   bool
	}{
		{
			name: "valid create table",
			operation: Operation{
				Type: "CREATE_TABLE",
				SQL:  "CREATE TABLE `customers` (`id` bigint NOT NULL)",
			},
		},
		{
			name: "valid add column",
			operation: Operation{
				Type: "ADD_COLUMN",
				SQL:  "ALTER TABLE `customers` ADD COLUMN `age` int",
			},
		},
		{
			name: "valid modify column",
			operation: Operation{
				Type: "MODIFY_COLUMN",
				SQL:  "ALTER TABLE `customers` MODIFY COLUMN `age` bigint",
			},
		},
		{
			name: "valid add index",
			operation: Operation{
				Type: "ADD_INDEX",
				SQL:  "CREATE INDEX `idx_email` ON `customers` (`email`)",
			},
		},
		{
			name: "valid foreign key",
			operation: Operation{
				Type: "ADD_FOREIGN_KEY",
				SQL: "ALTER TABLE `orders` ADD CONSTRAINT `fk_customer` " +
					"FOREIGN KEY (`customer_id`) REFERENCES `customers` (`id`)",
			},
		},
		{
			name: "reject drop table",
			operation: Operation{
				Type: "CREATE_TABLE",
				SQL:  "DROP TABLE `customers`",
			},
			wantErr: true,
		},
		{
			name: "reject multiple statements",
			operation: Operation{
				Type: "ADD_COLUMN",
				SQL:  "ALTER TABLE `customers` ADD COLUMN `age` int; DROP TABLE `customers`",
			},
			wantErr: true,
		},
		{
			name: "reject comments",
			operation: Operation{
				Type: "CREATE_TABLE",
				SQL:  "CREATE TABLE `customers` (`id` bigint) /* unsafe */",
			},
			wantErr: true,
		},
		{
			name: "reject mismatched operation",
			operation: Operation{
				Type: "ADD_COLUMN",
				SQL:  "CREATE TABLE `customers` (`id` bigint)",
			},
			wantErr: true,
		},
		{
			name: "reject unsupported operation",
			operation: Operation{
				Type: "DROP_TABLE",
				SQL:  "DROP TABLE `customers`",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOperationSQL(tt.operation)

			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestPlanValidate(t *testing.T) {
	plan := &Plan{
		SourceDatabase: "source_db",
		TargetDatabase: "target_db",
		Operations: []Operation{
			{
				ID:        1,
				Type:      "CREATE_TABLE",
				Table:     "customers",
				SQL:       "CREATE TABLE `customers` (`id` bigint NOT NULL)",
				RiskLevel: RiskMedium,
			},
			{
				ID:             2,
				Type:           "MODIFY_COLUMN",
				Table:          "customers",
				SQL:            "ALTER TABLE `customers` MODIFY COLUMN `id` int",
				RiskLevel:      RiskHigh,
				RequiresReview: true,
			},
		},
	}

	if err := plan.Validate(); err != nil {
		t.Fatalf("expected valid plan, got: %v", err)
	}
}

func TestPlanValidateRejectsInvalidOperationID(t *testing.T) {
	plan := &Plan{
		SourceDatabase: "source_db",
		TargetDatabase: "target_db",
		Operations: []Operation{
			{
				ID:        5,
				Type:      "CREATE_TABLE",
				Table:     "customers",
				SQL:       "CREATE TABLE `customers` (`id` bigint)",
				RiskLevel: RiskMedium,
			},
		},
	}

	if err := plan.Validate(); err == nil {
		t.Fatal("expected invalid operation ID to be rejected")
	}
}

func TestPlanValidateRejectsSameDatabase(t *testing.T) {
	plan := &Plan{
		SourceDatabase: "source_db",
		TargetDatabase: "source_db",
	}

	if err := plan.Validate(); err == nil {
		t.Fatal("expected identical source and target databases to be rejected")
	}
}
