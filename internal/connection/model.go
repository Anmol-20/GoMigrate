package connection

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("connection not found")

type Connection struct {
	ID                 int64
	Name               string
	DBType             string
	Host               string
	Port               int
	DatabaseName       string
	Username           string
	PasswordCiphertext []byte
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
