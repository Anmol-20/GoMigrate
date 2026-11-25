package migrationplan_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"github.com/Anmol-20/gomigrate/internal/config"
	"github.com/Anmol-20/gomigrate/internal/database"
	"github.com/Anmol-20/gomigrate/internal/migrationplan"
	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
	"github.com/Anmol-20/gomigrate/internal/repository"
	"github.com/Anmol-20/gomigrate/internal/schemadiff"
	"github.com/Anmol-20/gomigrate/internal/security"
)

const (
	sourceConnectionID     int64 = 1
	targetConnectionID     int64 = 3
	validationDatabaseName       = "gomigrate_validation"
)

func openValidationDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()

	_ = godotenv.Load("../../.env")

	ctx, cancel := context.WithTimeout(
		context.Background(),
		2*time.Minute,
	)

	cfg, err := config.Load()
	if err != nil {
		cancel()
		t.Fatalf("load configuration: %v", err)
	}

	metadataDB, err := database.NewMySQL(cfg)
	if err != nil {
		cancel()
		t.Fatalf("connect metadata database: %v", err)
	}

	connectionRepo := repository.NewMySQLConnectionRepository(metadataDB)

	encryptor, err := security.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		metadataDB.Close()
		cancel()
		t.Fatalf("initialize encryptor: %v", err)
	}

	targetConnection, err := connectionRepo.GetByID(
		ctx,
		targetConnectionID,
	)
	if err != nil {
		metadataDB.Close()
		cancel()
		t.Fatalf("retrieve saved target connection: %v", err)
	}

	if targetConnection.DatabaseName != "target_db" {
		metadataDB.Close()
		cancel()
		t.Fatalf(
			"unexpected saved target database %q; expected target_db",
			targetConnection.DatabaseName,
		)
	}

	targetPassword, err := encryptor.Decrypt(
		targetConnection.PasswordCiphertext,
	)
	if err != nil {
		metadataDB.Close()
		cancel()
		t.Fatalf("decrypt target password: %v", err)
	}

	targetDB, err := mysqlclient.Open(ctx, mysqlclient.Config{
		Host:     targetConnection.Host,
		Port:     targetConnection.Port,
		Database: validationDatabaseName,
		Username: targetConnection.Username,
		Password: targetPassword,
	})
	if err != nil {
		metadataDB.Close()
		cancel()
		t.Fatalf("connect validation database: %v", err)
	}

	t.Cleanup(func() {
		targetDB.Close()
		metadataDB.Close()
		cancel()
	})

	return ctx, targetDB
}
func TestCompositeForeignKeyMigration(t *testing.T) {
	if os.Getenv("GOMIGRATE_INTEGRATION") != "1" {
		t.Skip("set GOMIGRATE_INTEGRATION=1 to run")
	}

	ctx, targetDB := openValidationDB(t)

	// Use unique names so this test does not collide with existing tables.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	parentTable := "gm_composite_parent_" + suffix
	childTable := "gm_composite_child_" + suffix
	indexName := "idx_gm_composite_child_" + suffix
	foreignKeyName := "fk_gm_composite_child_" + suffix

	// Cleanup is limited to these uniquely named fixture tables.
	dropFixtures := func() {
		_, _ = targetDB.ExecContext(
			context.Background(),
			fmt.Sprintf("DROP TABLE IF EXISTS `%s`", childTable),
		)
		_, _ = targetDB.ExecContext(
			context.Background(),
			fmt.Sprintf("DROP TABLE IF EXISTS `%s`", parentTable),
		)
	}

	dropFixtures()
	t.Cleanup(dropFixtures)

	// 1. Create a parent table with a composite primary key.
	parentSQL := fmt.Sprintf(`
    CREATE TABLE %s (
        tenant_id BIGINT NOT NULL,
        entity_id BIGINT NOT NULL,
        PRIMARY KEY (tenant_id, entity_id)
    ) ENGINE=InnoDB
      DEFAULT CHARSET=utf8mb4
      COLLATE=utf8mb4_0900_ai_ci
`, "`"+parentTable+"`")

	if _, err := targetDB.ExecContext(ctx, parentSQL); err != nil {
		t.Fatalf("create composite parent fixture: %v", err)
	}

	// 2. Create a child table with a composite foreign key.
	// The supporting index is explicitly named so schema comparison
	// can verify it as well.
	childSQL := fmt.Sprintf(`
    CREATE TABLE %s (
        id BIGINT NOT NULL AUTO_INCREMENT,
        tenant_id BIGINT NOT NULL,
        entity_id BIGINT NOT NULL,
        PRIMARY KEY (id),
        KEY %s (tenant_id, entity_id),
        CONSTRAINT %s
            FOREIGN KEY (tenant_id, entity_id)
            REFERENCES %s (tenant_id, entity_id)
            ON UPDATE CASCADE
            ON DELETE RESTRICT
    ) ENGINE=InnoDB
      DEFAULT CHARSET=utf8mb4
      COLLATE=utf8mb4_0900_ai_ci
`,
		"`"+childTable+"`",
		"`"+indexName+"`",
		"`"+foreignKeyName+"`",
		"`"+parentTable+"`",
	)

	if _, err := targetDB.ExecContext(ctx, childSQL); err != nil {
		t.Fatalf("create composite child fixture: %v", err)
	}

	// 3. Discover the real MySQL schema.
	discoveredSchema, err := mysqlclient.DiscoverSchema(ctx, targetDB)
	if err != nil {
		t.Fatalf("discover composite fixture schema: %v", err)
	}
	discoveredSchema.Database = validationDatabaseName

	// Keep only our two fixture tables. The existing migrated tables
	// in gomigrate_validation are not part of this test.
	sourceSchema := &mysqlclient.Schema{
		Database: validationDatabaseName,
		Tables:   make([]mysqlclient.Table, 0, 2),
	}

	for _, table := range discoveredSchema.Tables {
		if table.Name == parentTable || table.Name == childTable {
			sourceSchema.Tables = append(sourceSchema.Tables, table)
		}
	}

	if len(sourceSchema.Tables) != 2 {
		t.Fatalf(
			"expected to discover 2 fixture tables; discovered %d",
			len(sourceSchema.Tables),
		)
	}

	// 4. Confirm that MySQL discovery returned the composite FK
	// with both columns in the correct order.
	var discoveredFK *mysqlclient.ForeignKey

	for _, table := range sourceSchema.Tables {
		if table.Name != childTable {
			continue
		}

		for i := range table.ForeignKeys {
			if table.ForeignKeys[i].Name == foreignKeyName {
				discoveredFK = &table.ForeignKeys[i]
				break
			}
		}
	}

	if discoveredFK == nil {
		t.Fatalf("composite foreign key %q was not discovered", foreignKeyName)
	}

	if len(discoveredFK.Columns) != 2 ||
		discoveredFK.Columns[0] != "tenant_id" ||
		discoveredFK.Columns[1] != "entity_id" {
		t.Fatalf(
			"unexpected composite FK columns: %#v",
			discoveredFK.Columns,
		)
	}

	if len(discoveredFK.ReferencedColumns) != 2 ||
		discoveredFK.ReferencedColumns[0] != "tenant_id" ||
		discoveredFK.ReferencedColumns[1] != "entity_id" {
		t.Fatalf(
			"unexpected referenced columns: %#v",
			discoveredFK.ReferencedColumns,
		)
	}

	t.Logf(
		"Discovered composite FK %s: (%s, %s) -> %s(%s, %s)",
		discoveredFK.Name,
		discoveredFK.Columns[0],
		discoveredFK.Columns[1],
		discoveredFK.ReferencedTable,
		discoveredFK.ReferencedColumns[0],
		discoveredFK.ReferencedColumns[1],
	)

	// 5. Generate a migration plan against an empty logical schema.
	emptySchema := &mysqlclient.Schema{
		Database: validationDatabaseName,
		Tables:   []mysqlclient.Table{},
	}

	comparison := schemadiff.Compare(sourceSchema, emptySchema)

	plan, err := migrationplan.NewGenerator().Generate(sourceSchema, comparison)
	if err != nil {
		t.Fatalf("generate composite FK migration plan: %v", err)
	}

	if len(plan.Operations) == 0 {
		t.Fatal("expected migration operations for composite FK fixtures")
	}

	// 6. Remove the original fixtures so the generated CREATE TABLE
	// statements can execute without colliding with existing tables.
	dropFixtures()

	// 7. Execute the generated migration plan.
	for _, operation := range plan.Operations {
		t.Logf(
			"Executing operation %d: %s on table %s",
			operation.ID,
			operation.Type,
			operation.Table,
		)
		t.Logf("SQL: %s", operation.SQL)

		if _, err := targetDB.ExecContext(ctx, operation.SQL); err != nil {
			t.Fatalf(
				"execute operation %d (%s): %v\nSQL: %s",
				operation.ID,
				operation.Type,
				err,
				operation.SQL,
			)
		}
	}

	// 8. Rediscover the recreated schema.
	migratedSchema, err := mysqlclient.DiscoverSchema(ctx, targetDB)
	if err != nil {
		t.Fatalf("discover migrated composite fixtures: %v", err)
	}
	migratedSchema.Database = validationDatabaseName

	migratedFixtures := &mysqlclient.Schema{
		Database: validationDatabaseName,
		Tables:   make([]mysqlclient.Table, 0, 2),
	}

	for _, table := range migratedSchema.Tables {
		if table.Name == parentTable || table.Name == childTable {
			migratedFixtures.Tables = append(migratedFixtures.Tables, table)
		}
	}

	if len(migratedFixtures.Tables) != 2 {
		t.Fatalf(
			"expected 2 migrated fixture tables; found %d",
			len(migratedFixtures.Tables),
		)
	}

	// 9. Verify that the recreated schema matches the discovered source.
	verificationComparison := schemadiff.Compare(
		sourceSchema,
		migratedFixtures,
	)

	verificationPlan, err := migrationplan.NewGenerator().Generate(
		sourceSchema,
		verificationComparison,
	)
	if err != nil {
		t.Fatalf("generate composite FK verification plan: %v", err)
	}

	if len(verificationPlan.Operations) != 0 ||
		len(verificationPlan.Warnings) != 0 {
		t.Fatalf(
			"composite FK verification failed: %d remaining operations, %d warnings",
			len(verificationPlan.Operations),
			len(verificationPlan.Warnings),
		)
	}

	t.Log("Composite foreign key migration and verification successful")
}

func TestGenerateRealDatabasePlan(t *testing.T) {
	if os.Getenv("GOMIGRATE_INTEGRATION") != "1" {
		t.Skip("set GOMIGRATE_INTEGRATION=1 to run")
	}

	// Load project environment variables.
	// Existing environment variables are not overwritten.
	_ = godotenv.Load("../../.env")

	ctx, cancel := context.WithTimeout(
		context.Background(),
		2*time.Minute,
	)
	defer cancel()

	// 1. Load application configuration.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}

	// 2. Connect to GoMigrate metadata database.
	metadataDB, err := database.NewMySQL(cfg)
	if err != nil {
		t.Fatalf("connect metadata database: %v", err)
	}
	defer metadataDB.Close()

	// 3. Initialize repository and encryption.
	connectionRepo := repository.NewMySQLConnectionRepository(metadataDB)

	encryptor, err := security.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		t.Fatalf("initialize encryptor: %v", err)
	}

	// 4. Retrieve saved source connection.
	sourceConnection, err := connectionRepo.GetByID(
		ctx,
		sourceConnectionID,
	)
	if err != nil {
		t.Fatalf("retrieve source connection: %v", err)
	}

	if sourceConnection.DatabaseName != "source_db" {
		t.Fatalf(
			"unexpected source database %q; expected source_db",
			sourceConnection.DatabaseName,
		)
	}

	sourcePassword, err := encryptor.Decrypt(
		sourceConnection.PasswordCiphertext,
	)
	if err != nil {
		t.Fatalf("decrypt source password: %v", err)
	}

	// 5. Connect to source database using saved credentials.
	sourceDB, err := mysqlclient.Open(ctx, mysqlclient.Config{
		Host:     sourceConnection.Host,
		Port:     sourceConnection.Port,
		Database: sourceConnection.DatabaseName,
		Username: sourceConnection.Username,
		Password: sourcePassword,
	})
	if err != nil {
		t.Fatalf("connect source database: %v", err)
	}
	defer sourceDB.Close()

	// 6. Retrieve saved target connection.
	targetConnection, err := connectionRepo.GetByID(
		ctx,
		targetConnectionID,
	)
	if err != nil {
		t.Fatalf("retrieve target connection: %v", err)
	}

	if targetConnection.DatabaseName != "target_db" {
		t.Fatalf(
			"unexpected saved target database %q; expected target_db",
			targetConnection.DatabaseName,
		)
	}

	targetPassword, err := encryptor.Decrypt(
		targetConnection.PasswordCiphertext,
	)
	if err != nil {
		t.Fatalf("decrypt target password: %v", err)
	}

	// 7. Connect using the saved target credentials,
	// but override the database name.
	//
	// This test must NEVER connect to target_db.
	targetDB, err := mysqlclient.Open(ctx, mysqlclient.Config{
		Host:     targetConnection.Host,
		Port:     targetConnection.Port,
		Database: validationDatabaseName,
		Username: targetConnection.Username,
		Password: targetPassword,
	})
	if err != nil {
		t.Fatalf("connect validation database: %v", err)
	}
	defer targetDB.Close()

	// 8. Verify that the validation database is empty.
	validationSchema, err := mysqlclient.DiscoverSchema(ctx, targetDB)
	if err != nil {
		t.Fatalf("discover validation database: %v", err)
	}

	if len(validationSchema.Tables) != 0 {
		t.Fatalf(
			"%s must be empty before migration; found %d tables. "+
				"Recreate the validation database before running again",
			validationDatabaseName,
			len(validationSchema.Tables),
		)
	}

	// 9. Discover source schema.
	sourceSchema, err := mysqlclient.DiscoverSchema(ctx, sourceDB)
	if err != nil {
		t.Fatalf("discover source schema: %v", err)
	}
	sourceSchema.Database = sourceConnection.DatabaseName

	if len(sourceSchema.Tables) == 0 {
		t.Fatal("source database contains no tables")
	}

	// 10. Compare source and empty validation database.
	comparison := schemadiff.Compare(sourceSchema, validationSchema)

	// 11. Generate migration plan.
	plan, err := migrationplan.NewGenerator().Generate(sourceSchema, comparison)
	if err != nil {
		t.Fatalf("generate migration plan: %v", err)
	}

	if len(plan.Operations) == 0 {
		t.Fatal("expected migration operations for empty validation database")
	}

	for _, warning := range plan.Warnings {
		t.Logf("Migration warning: %s", warning)
	}

	// 12. Execute generated migration operations.
	//
	// Only generated SQL is executed, and only against
	// gomigrate_validation.
	for _, operation := range plan.Operations {
		t.Logf(
			"Executing operation %d: %s on table %s",
			operation.ID,
			operation.Type,
			operation.Table,
		)

		t.Logf("SQL: %s", operation.SQL)

		if _, err := targetDB.ExecContext(ctx, operation.SQL); err != nil {
			t.Fatalf(
				"execute operation %d (%s) failed: %v\nSQL: %s",
				operation.ID,
				operation.Type,
				err,
				operation.SQL,
			)
		}
	}

	// 13. Discover schema after migration.
	migratedSchema, err := mysqlclient.DiscoverSchema(ctx, targetDB)
	if err != nil {
		t.Fatalf("discover migrated schema: %v", err)
	}
	migratedSchema.Database = validationDatabaseName

	// 14. Compare migrated schema against source.
	verificationComparison := schemadiff.Compare(
		sourceSchema,
		migratedSchema,
	)

	verificationPlan, err := migrationplan.NewGenerator().Generate(
		sourceSchema,
		verificationComparison,
	)
	if err != nil {
		t.Fatalf("generate verification plan: %v", err)
	}

	if len(verificationPlan.Operations) != 0 ||
		len(verificationPlan.Warnings) != 0 {
		t.Fatalf(
			"schema verification failed: %d remaining operations, %d warnings",
			len(verificationPlan.Operations),
			len(verificationPlan.Warnings),
		)
	}

	// 15. Explicitly verify engine, charset and collation.
	if err := verifyTableProperties(sourceSchema, migratedSchema); err != nil {
		t.Fatal(err)
	}

	t.Logf(
		"Migration successful: %d tables migrated and verified in %s",
		len(migratedSchema.Tables),
		validationDatabaseName,
	)
}

// verifyTableProperties checks table metadata that is not yet
// included in schemadiff.Compare.
func verifyTableProperties(
	source *mysqlclient.Schema,
	target *mysqlclient.Schema,
) error {
	targetTables := make(map[string]mysqlclient.Table)

	for _, table := range target.Tables {
		targetTables[strings.ToLower(table.Name)] = table
	}

	for _, sourceTable := range source.Tables {
		targetTable, exists := targetTables[strings.ToLower(sourceTable.Name)]
		if !exists {
			return fmt.Errorf(
				"table %q missing from migrated schema",
				sourceTable.Name,
			)
		}

		if !strings.EqualFold(sourceTable.Engine, targetTable.Engine) {
			return fmt.Errorf(
				"engine mismatch for table %s: source=%s target=%s",
				sourceTable.Name,
				sourceTable.Engine,
				targetTable.Engine,
			)
		}

		if !strings.EqualFold(sourceTable.Charset, targetTable.Charset) {
			return fmt.Errorf(
				"charset mismatch for table %s: source=%s target=%s",
				sourceTable.Name,
				sourceTable.Charset,
				targetTable.Charset,
			)
		}

		if !strings.EqualFold(sourceTable.Collation, targetTable.Collation) {
			return fmt.Errorf(
				"collation mismatch for table %s: source=%s target=%s",
				sourceTable.Name,
				sourceTable.Collation,
				targetTable.Collation,
			)
		}
	}

	return nil
}
