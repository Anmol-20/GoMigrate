package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Anmol-20/gomigrate/internal/connection"
)

type ConnectionRepository interface {
	Create(
		ctx context.Context,
		conn *connection.Connection,
	) error

	GetByID(
		ctx context.Context,
		id int64,
	) (*connection.Connection, error)

	List(
		ctx context.Context,
	) ([]connection.Connection, error)

	Delete(
		ctx context.Context,
		id int64,
	) error
}

type MySQLConnectionRepository struct {
	db *sql.DB
}

func NewMySQLConnectionRepository(
	db *sql.DB,
) *MySQLConnectionRepository {

	return &MySQLConnectionRepository{
		db: db,
	}
}

func (r *MySQLConnectionRepository) Create(
	ctx context.Context,
	conn *connection.Connection,
) error {

	query := `
        INSERT INTO connections (
            name,
            db_type,
            host,
            port,
            database_name,
            username,
            password_ciphertext
        )
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `

	result, err := r.db.ExecContext(
		ctx,
		query,
		conn.Name,
		conn.DBType,
		conn.Host,
		conn.Port,
		conn.DatabaseName,
		conn.Username,
		conn.PasswordCiphertext,
	)

	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	conn.ID = id

	conn.ID = id

	err = r.db.QueryRowContext(
		ctx,
		`SELECT created_at, updated_at
     FROM connections WHERE id = ?`,
		id,
	).Scan(
		&conn.CreatedAt,
		&conn.UpdatedAt,
	)

	if err != nil {
		return err
	}

	return nil
}

func (r *MySQLConnectionRepository) GetByID(
	ctx context.Context,
	id int64,
) (*connection.Connection, error) {

	query := `
        SELECT
            id,
            name,
            db_type,
            host,
            port,
            database_name,
            username,
            password_ciphertext,
            created_at,
            updated_at
        FROM connections
        WHERE id = ?
    `

	var conn connection.Connection

	err := r.db.QueryRowContext(
		ctx,
		query,
		id,
	).Scan(
		&conn.ID,
		&conn.Name,
		&conn.DBType,
		&conn.Host,
		&conn.Port,
		&conn.DatabaseName,
		&conn.Username,
		&conn.PasswordCiphertext,
		&conn.CreatedAt,
		&conn.UpdatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, connection.ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	return &conn, nil
}

func (r *MySQLConnectionRepository) List(
	ctx context.Context,
) ([]connection.Connection, error) {

	query := `
        SELECT
            id,
            name,
            db_type,
            host,
            port,
            database_name,
            username,
            password_ciphertext,
            created_at,
            updated_at
        FROM connections
        ORDER BY id DESC
    `

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	connections := make([]connection.Connection, 0)

	for rows.Next() {

		var conn connection.Connection

		err := rows.Scan(
			&conn.ID,
			&conn.Name,
			&conn.DBType,
			&conn.Host,
			&conn.Port,
			&conn.DatabaseName,
			&conn.Username,
			&conn.PasswordCiphertext,
			&conn.CreatedAt,
			&conn.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		connections = append(connections, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return connections, nil
}

func (r *MySQLConnectionRepository) Delete(
	ctx context.Context,
	id int64,
) error {

	query := `DELETE FROM connections WHERE id = ?`

	result, err := r.db.ExecContext(
		ctx,
		query,
		id,
	)

	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected == 0 {
		return connection.ErrNotFound
	}

	return nil
}
