CREATE TABLE connections (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    db_type VARCHAR(30) NOT NULL,
    host VARCHAR(255) NOT NULL,
    port INT NOT NULL,
    database_name VARCHAR(100) NOT NULL,
    username VARCHAR(100) NOT NULL,
    password_ciphertext BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_connections_name (name)
);