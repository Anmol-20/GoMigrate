# Batch 4C — Read-only schema reconciliation

## New endpoint

`GET /api/v1/migration-executions/{id}/reconcile`

The endpoint loads the execution and its checksum-verified persisted plan, opens the dedicated validation database through the existing validation-database provider, verifies `SELECT DATABASE()` is exactly `gomigrate_validation`, discovers the current schema, and returns a read-only report.

## Current verification scope

- `CREATE_TABLE`: checks whether the planned table currently exists. Findings are `PRESENT` or `ABSENT`.
- `ADD_COLUMN`, `MODIFY_COLUMN`, `ADD_INDEX`, and `ADD_FOREIGN_KEY`: report `AMBIGUOUS` in this batch. Table existence is not sufficient to prove these object-level changes.
- Execution operation statuses are reported separately from schema findings. A recorded `COMPLETED` status is not treated as proof that the full schema matches the plan.

## Safety properties

- Uses the existing validation database provider, which overrides the saved target database with `gomigrate_validation`.
- Independently checks the selected database name before schema discovery.
- Does not execute migration SQL, retry operations, update execution state, or roll back changes.
- Does not modify `target_db`.

## Verification (PowerShell)

```powershell
gofmt -w cmd/api/main.go
gofmt -w internal/service/migration_execution.go
gofmt -w internal/handler/migration_execution.go
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Start the API and query a known execution ID:

```powershell
Invoke-RestMethod -Uri "http://localhost:8080/api/v1/migration-executions/1/reconcile" -Method GET
```

Use an actual execution ID from your database; `1` is only an example. The report is diagnostic, not an authorization to resume or rerun SQL.
