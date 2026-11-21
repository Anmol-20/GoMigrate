package config

import (
	"fmt"
	"os"
)

type Config struct {
	ServerPort string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
}

func Load() (Config, error) {

	cfg := Config{
		ServerPort: getEnv("SERVER_PORT", "8080"),

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBUser:     getEnv("DB_USER", ""),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBName:     getEnv("DB_NAME", "gomigrate"),
	}

	if cfg.DBUser == "" {
		return Config{}, fmt.Errorf("DB_USER is required")
	}

	if cfg.DBPassword == "" {
		return Config{}, fmt.Errorf("DB_PASSWORD is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {

	value, exists := os.LookupEnv(key)

	if !exists || value == "" {
		return fallback
	}

	return value
}
