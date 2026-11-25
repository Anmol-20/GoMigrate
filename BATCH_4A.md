# Batch 4A — Validation database coordination

The migration execution service now serializes all plan executions against the dedicated `gomigrate_validation` database using a MySQL advisory lock.

- Lock name: `gomigrate:validation:gomigrate_validation`
- Lock acquisition: `GET_LOCK(lock_name, 0)` (non-blocking)
- Lock release: `RELEASE_LOCK(lock_name)` on the same pinned `sql.Conn`
- All plan DDL runs through that pinned connection while the lock is held.
- The executor verifies `SELECT DATABASE()` on the pinned execution connection before executing operations.
- If the lock is unavailable, the execution is marked failed and no plan operation is executed.

No additional metadata schema migration is required for Batch 4A.

## Verification

Run from the project root:

```powershell
gofmt -w internal/service/migration_execution.go
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

This lock coordinates GoMigrate executions that use this lock name. It does not prevent a DBA or another application from modifying the validation schema outside GoMigrate.
