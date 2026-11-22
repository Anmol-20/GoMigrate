package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

type Encryptor struct {
	aead cipher.AEAD
}

func NewEncryptor(encodedKey string) (*Encryptor, error) {

	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid encryption key: %w", err,
		)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf(
			"encryption key must be 32 bytes",
		)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to initialize AES: %w", err,
		)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to initialize GCM: %w", err,
		)
	}

	return &Encryptor{aead: aead}, nil
}

func (e *Encryptor) Encrypt(plaintext string) ([]byte, error) {

	nonce := make([]byte, e.aead.NonceSize())

	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := e.aead.Seal(
		nonce,
		nonce,
		[]byte(plaintext),
		nil,
	)

	return ciphertext, nil
}

func (e *Encryptor) Decrypt(ciphertext []byte) (string, error) {

	nonceSize := e.aead.NonceSize()

	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("invalid ciphertext")
	}

	nonce := ciphertext[:nonceSize]
	encryptedData := ciphertext[nonceSize:]

	plaintext, err := e.aead.Open(
		nil,
		nonce,
		encryptedData,
		nil,
	)

	if err != nil {
		return "", fmt.Errorf(
			"failed to decrypt password: %w", err,
		)
	}

	return string(plaintext), nil
}
