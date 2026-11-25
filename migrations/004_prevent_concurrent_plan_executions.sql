-- Prevent more than one active (PENDING or RUNNING) execution per migration plan.
-- MySQL permits multiple NULL values in a UNIQUE index, so terminal executions
-- do not conflict with one another.
ALTER TABLE migration_executions
    ADD COLUMN active_plan_id BIGINT
        GENERATED ALWAYS AS (
            CASE
                WHEN status IN ('PENDING', 'RUNNING') THEN plan_id
                ELSE NULL
            END
        ) STORED,
    ADD UNIQUE INDEX uq_migration_executions_active_plan (active_plan_id);
