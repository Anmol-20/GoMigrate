package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/Anmol-20/gomigrate/internal/connection"
	"github.com/Anmol-20/gomigrate/internal/security"
)

var ErrInvalidInput = errors.New("invalid input")

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

type ConnectionService struct {
	repo      ConnectionRepository
	encryptor *security.Encryptor
}

func NewConnectionService(
	repo ConnectionRepository,
	encryptor *security.Encryptor,
) *ConnectionService {

	return &ConnectionService{
		repo:      repo,
		encryptor: encryptor,
	}
}

type CreateConnectionRequest struct {
	Name     string `json:"name"`
	DBType   string `json:"db_type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ConnectionResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	DBType    string `json:"db_type"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Database  string `json:"database"`
	Username  string `json:"username"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func validateConnection(req CreateConnectionRequest) error {

	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}

	if strings.ToLower(strings.TrimSpace(req.DBType)) != "mysql" {
		return fmt.Errorf("%w: only mysql is supported", ErrInvalidInput)
	}

	if strings.TrimSpace(req.Host) == "" {
		return fmt.Errorf("%w: host is required", ErrInvalidInput)
	}

	if net.ParseIP(req.Host) == nil && strings.ContainsAny(req.Host, " /\\:@") {
		return fmt.Errorf("%w: invalid host", ErrInvalidInput)
	}

	if req.Port < 1 || req.Port > 65535 {
		return fmt.Errorf("%w: invalid port", ErrInvalidInput)
	}

	if strings.TrimSpace(req.Database) == "" {
		return fmt.Errorf("%w: database is required", ErrInvalidInput)
	}

	if strings.TrimSpace(req.Username) == "" {
		return fmt.Errorf("%w: username is required", ErrInvalidInput)
	}

	if req.Password == "" {
		return fmt.Errorf("%w: password is required", ErrInvalidInput)
	}

	return nil
}

func toResponse(
	conn *connection.Connection,
) ConnectionResponse {

	return ConnectionResponse{
		ID:        conn.ID,
		Name:      conn.Name,
		DBType:    conn.DBType,
		Host:      conn.Host,
		Port:      conn.Port,
		Database:  conn.DatabaseName,
		Username:  conn.Username,
		Status:    "REGISTERED",
		CreatedAt: conn.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func (s *ConnectionService) Create(
	ctx context.Context,
	req CreateConnectionRequest,
) (*ConnectionResponse, error) {

	req.Name = strings.TrimSpace(req.Name)
	req.DBType = strings.ToLower(strings.TrimSpace(req.DBType))
	req.Host = strings.TrimSpace(req.Host)
	req.Database = strings.TrimSpace(req.Database)
	req.Username = strings.TrimSpace(req.Username)

	if err := validateConnection(req); err != nil {
		return nil, err
	}

	encryptedPassword, err := s.encryptor.Encrypt(req.Password)
	if err != nil {
		return nil, fmt.Errorf("encrypt password: %w", err)
	}

	conn := &connection.Connection{
		Name:               req.Name,
		DBType:             req.DBType,
		Host:               req.Host,
		Port:               req.Port,
		DatabaseName:       req.Database,
		Username:           req.Username,
		PasswordCiphertext: encryptedPassword,
	}

	if err := s.repo.Create(ctx, conn); err != nil {
		return nil, fmt.Errorf("create connection: %w", err)
	}

	response := toResponse(conn)
	return &response, nil
}

func (s *ConnectionService) GetByID(
	ctx context.Context,
	id int64,
) (*ConnectionResponse, error) {

	if id <= 0 {
		return nil, fmt.Errorf("%w: invalid ID", ErrInvalidInput)
	}

	conn, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	response := toResponse(conn)
	return &response, nil
}

func (s *ConnectionService) List(
	ctx context.Context,
) ([]ConnectionResponse, error) {

	connections, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	responses := make([]ConnectionResponse, 0, len(connections))

	for i := range connections {
		responses = append(
			responses,
			toResponse(&connections[i]),
		)
	}

	return responses, nil
}

func (s *ConnectionService) Delete(
	ctx context.Context,
	id int64,
) error {

	if id <= 0 {
		return fmt.Errorf("%w: invalid ID", ErrInvalidInput)
	}

	return s.repo.Delete(ctx, id)
}

// Ensure the MySQL repository error is accessible to callers.
