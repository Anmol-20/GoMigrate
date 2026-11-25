CREATE TABLE migration_executions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    plan_id BIGINT NOT NULL,
    status ENUM('PENDING', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED') NOT NULL DEFAULT 'PENDING',
    started_at TIMESTAMP NULL,
    completed_at TIMESTAMP NULL,
    error_message TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_migration_executions_plan
        FOREIGN KEY (plan_id) REFERENCES migration_plans(id),
    INDEX idx_migration_executions_plan (plan_id),
    INDEX idx_migration_executions_status (status)
);

CREATE TABLE migration_execution_operations (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    execution_id BIGINT NOT NULL,
    operation_id INT NOT NULL,
    operation_type VARCHAR(40) NOT NULL,
    table_name VARCHAR(100) NOT NULL,
    status ENUM('PENDING', 'RUNNING', 'COMPLETED', 'FAILED', 'SKIPPED') NOT NULL DEFAULT 'PENDING',
    started_at TIMESTAMP NULL,
    completed_at TIMESTAMP NULL,
    error_message TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_execution_operations_execution
        FOREIGN KEY (execution_id) REFERENCES migration_executions(id)
        ON DELETE CASCADE,
    CONSTRAINT uq_execution_operation UNIQUE (execution_id, operation_id),
    INDEX idx_execution_operations_status (execution_id, status)
);
