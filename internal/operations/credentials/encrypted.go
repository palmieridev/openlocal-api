// Package credentials provides authenticated replay encryption and random access tokens.
package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/palmieridev/openlocal-api/internal/operations/application"
)

type Encrypted struct{ key string }

func New(key string) *Encrypted { return &Encrypted{key: key} }
func (e *Encrypted) aead() (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(e.key)
	if err != nil || len(key) != 32 {
		return nil, &application.Error{Status: 503, Code: "module_unavailable", Message: "request credentials are not configured"}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (e *Encrypted) Seal(plain, associated []byte) ([]byte, error) {
	a, err := e.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, plain, associated), nil
}
func (e *Encrypted) Open(encrypted, associated []byte) ([]byte, error) {
	a, err := e.aead()
	if err != nil {
		return nil, err
	}
	if len(encrypted) < a.NonceSize() {
		return nil, errors.New("invalid encrypted replay")
	}
	return a.Open(nil, encrypted[:a.NonceSize()], encrypted[a.NonceSize():], associated)
}
func (e *Encrypted) Token() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}
