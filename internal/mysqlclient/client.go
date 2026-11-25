package mysqlclient

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

type Config struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
}

func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	driverConfig := mysql.Config{
		User:   cfg.Username,
		Passwd: cfg.Password,
		Net:    "tcp",
		Addr: net.JoinHostPort(
			cfg.Host,
			strconv.Itoa(cfg.Port),
		),
		DBName:               cfg.Database,
		Timeout:              5 * time.Second,
		ReadTimeout:          10 * time.Second,
		WriteTimeout:         10 * time.Second,
		ParseTime:            true,
		AllowNativePasswords: true,
	}

	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open mysql connection: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(2 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("mysql ping failed: %w", err)
	}

	return db, nil
}
