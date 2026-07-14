package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

func NewSessionToken() (string, [sha256.Size]byte, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("read random session token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(bytes)
	return token, HashSessionToken(token), nil
}

func HashSessionToken(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}
