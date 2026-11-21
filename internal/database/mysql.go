package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/Anmol-20/gomigrate/internal/config"
)

func NewMySQL(cfg config.Config) (*sql.DB, error) {

	mysqlConfig := mysql.NewConfig()

	mysqlConfig.User = cfg.DBUser
	mysqlConfig.Passwd = cfg.DBPassword
	mysqlConfig.Net = "tcp"
	mysqlConfig.Addr = fmt.Sprintf(
		"%s:%s",
		cfg.DBHost,
		cfg.DBPort,
	)
	mysqlConfig.DBName = cfg.DBName
	mysqlConfig.ParseTime = true
	mysqlConfig.Loc = time.UTC

	dsn := mysqlConfig.FormatDSN()

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to open database: %w", err,
		)
	}

	// Configure connection pool.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify actual connectivity.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()

		return nil, fmt.Errorf(
			"failed to connect to MySQL: %w", err,
		)
	}

	return db, nil
}
