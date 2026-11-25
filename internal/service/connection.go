package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/Anmol-20/gomigrate/internal/connection"
	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
	"github.com/Anmol-20/gomigrate/internal/security"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrConnectionFailed = errors.New("mysql connection failed")
var ErrSchemaDiscoveryFailed = errors.New("schema discovery failed")

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

func (s *ConnectionService) TestConnection(
	ctx context.Context,
	id int64,
) error {

	if id <= 0 {
		return fmt.Errorf("%w: invalid ID", ErrInvalidInput)
	}

	// Step 1: Retrieve saved connection details.
	conn, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Step 2: Decrypt the stored password.
	password, err := s.encryptor.Decrypt(
		conn.PasswordCiphertext,
	)
	if err != nil {
		return fmt.Errorf("decrypt connection password: %w", err)
	}

	// Step 3: Establish an external MySQL connection.
	db, err := mysqlclient.Open(ctx, mysqlclient.Config{
		Host:     conn.Host,
		Port:     conn.Port,
		Database: conn.DatabaseName,
		Username: conn.Username,
		Password: password,
	})

	if err != nil {
		return fmt.Errorf("%w: %w", ErrConnectionFailed, err)
	}

	// Step 4: Close the temporary connection.
	defer db.Close()

	return nil
}

func (s *ConnectionService) DiscoverSchema(
	ctx context.Context,
	id int64,
) (*mysqlclient.Schema, error) {

	if id <= 0 {
		return nil, fmt.Errorf("%w: invalid ID", ErrInvalidInput)
	}

	// Step 1: Retrieve connection configuration.
	conn, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Step 2: Decrypt saved password.
	password, err := s.encryptor.Decrypt(
		conn.PasswordCiphertext,
	)
	if err != nil {
		return nil, fmt.Errorf("decrypt connection password: %w", err)
	}

	// Step 3: Connect to external MySQL.
	db, err := mysqlclient.Open(ctx, mysqlclient.Config{
		Host:     conn.Host,
		Port:     conn.Port,
		Database: conn.DatabaseName,
		Username: conn.Username,
		Password: password,
	})

	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnectionFailed, err)
	}
	defer db.Close()

	// Step 4: Discover database schema.
	schema, err := mysqlclient.DiscoverSchema(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSchemaDiscoveryFailed, err)
	}

	// Step 5: Include the database name in the response.
	schema.Database = conn.DatabaseName

	return schema, nil
}

// ExecutionDatabaseName is the only database the migration executor may modify.
const ExecutionDatabaseName = "gomigrate_execution_test"

// OpenValidationDatabase reuses the saved target server credentials but overrides
// the selected schema with the dedicated execution test database.
// The caller must close the returned handle.
func (s *ConnectionService) OpenValidationDatabase(
	ctx context.Context,
	targetConnectionID int64,
) (*sql.DB, error) {
	if targetConnectionID <= 0 {
		return nil, fmt.Errorf("%w: invalid target connection ID", ErrInvalidInput)
	}

	conn, err := s.repo.GetByID(ctx, targetConnectionID)
	if err != nil {
		return nil, err
	}

	password, err := s.encryptor.Decrypt(conn.PasswordCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt saved target password: %w", err)
	}

	db, err := mysqlclient.Open(ctx, mysqlclient.Config{
		Host:     conn.Host,
		Port:     conn.Port,
		Database: ExecutionDatabaseName,
		Username: conn.Username,
		Password: password,
	})
	if err != nil {
		return nil, fmt.Errorf("open execution test database: %w", err)
	}

	var selected string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&selected); err != nil {
		db.Close()
		return nil, fmt.Errorf("verify selected execution database: %w", err)
	}

	if selected != ExecutionDatabaseName {
		db.Close()
		return nil, fmt.Errorf(
			"refusing to execute: selected database %q is not %q",
			selected,
			ExecutionDatabaseName,
		)
	}

	return db, nil
}
