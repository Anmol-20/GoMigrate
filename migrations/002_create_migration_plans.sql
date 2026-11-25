CREATE TABLE migration_plans (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    source_connection_id BIGINT NOT NULL,
    target_connection_id BIGINT NOT NULL,
    source_database VARCHAR(100) NOT NULL,
    target_database VARCHAR(100) NOT NULL,
    plan_json JSON NOT NULL,
    plan_checksum CHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_migration_plans_source_connection
        FOREIGN KEY (source_connection_id) REFERENCES connections(id),
    CONSTRAINT fk_migration_plans_target_connection
        FOREIGN KEY (target_connection_id) REFERENCES connections(id),
    INDEX idx_migration_plans_created_at (created_at),
    INDEX idx_migration_plans_connections (source_connection_id, target_connection_id)
);
