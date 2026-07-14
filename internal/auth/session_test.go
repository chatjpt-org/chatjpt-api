package auth

import "testing"

func TestNewSessionToken(t *testing.T) {
	token, hash, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("NewSessionToken() returned an empty token")
	}
	if got := HashSessionToken(token); got != hash {
		t.Fatal("HashSessionToken() does not match the returned token hash")
	}
}
