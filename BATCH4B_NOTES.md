# Batch 4B: Stale execution diagnostics (read-only)

Adds `GET /api/v1/migration-executions/stale?older_than_minutes=30`.

The endpoint lists executions still in RUNNING whose `updated_at` is older than the requested threshold (1..10080 minutes; default 30), including operation records. It is diagnostic-only: it does not change statuses, infer DDL outcomes, or retry SQL.

An operation marked RUNNING remains ambiguous after interruption. Reconcile its actual schema manually before any recovery action. This batch intentionally does not implement recovery mutation.

No metadata schema migration is required.
