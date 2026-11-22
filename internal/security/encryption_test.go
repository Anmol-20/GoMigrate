package security

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {

	key := make([]byte, 32)

	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	encodedKey := base64.StdEncoding.EncodeToString(key)

	encryptor, err := NewEncryptor(encodedKey)
	if err != nil {
		t.Fatal(err)
	}

	original := "my-secret-database-password"

	encrypted, err := encryptor.Encrypt(original)
	if err != nil {
		t.Fatal(err)
	}

	if string(encrypted) == original {
		t.Fatal("encrypted data should differ from plaintext")
	}

	decrypted, err := encryptor.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}

	if decrypted != original {
		t.Fatalf(
			"expected %q, got %q",
			original,
			decrypted,
		)
	}
}

func TestDecryptWithTamperedCiphertext(t *testing.T) {

	key := make([]byte, 32)

	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	encryptor, err := NewEncryptor(
		base64.StdEncoding.EncodeToString(key),
	)
	if err != nil {
		t.Fatal(err)
	}

	encrypted, err := encryptor.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}

	encrypted[len(encrypted)-1] ^= 1

	_, err = encryptor.Decrypt(encrypted)

	if err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}
