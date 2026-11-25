# Batch 3: Active execution uniqueness

This batch adds database-level protection against concurrent executions of the same migration plan.

## Migration

Apply `migrations/004_prevent_concurrent_plan_executions.sql` to the GoMigrate metadata database (`gomigrate`), after migrations 001-003.

It adds `active_plan_id`, a stored generated column that contains `plan_id` only while an execution is `PENDING` or `RUNNING`, and a unique index on that generated column. MySQL unique indexes permit multiple NULL values, so terminal executions do not prevent later executions of a plan.

## Behavior

- Concurrent create attempts for the same plan are serialized/enforced by MySQL's unique index.
- A `PENDING` or `RUNNING` record prevents another active execution for that plan.
- A crashed execution left in `RUNNING` intentionally blocks new execution attempts until reconciliation is implemented.
- Terminal statuses (`COMPLETED`, `FAILED`, `CANCELLED`) no longer occupy the active-plan key.

This is per-plan protection, not a global lock on the validation database. Plans that target the same validation database but are different plan IDs may still run concurrently; global validation DB coordination is a separate follow-up.
