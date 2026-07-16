package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(store.Close)

	if err := ApplyMigrations(ctx, store); err != nil {
		t.Fatalf("ApplyMigrations() error = %v", err)
	}
	if err := ApplyMigrations(ctx, store); err != nil {
		t.Fatalf("ApplyMigrations() a second time error = %v", err)
	}

	firstUsername := "integration_first"
	secondUsername := "integration_second"
	if err := store.CreateUser(ctx, firstUsername, "first-password-hash"); err != nil {
		t.Fatalf("CreateUser(first) error = %v", err)
	}
	if err := store.CreateUser(ctx, secondUsername, "second-password-hash"); err != nil {
		t.Fatalf("CreateUser(second) error = %v", err)
	}
	firstUser, passwordHash, err := store.FindUserByUsername(ctx, firstUsername)
	if err != nil {
		t.Fatalf("FindUserByUsername(first) error = %v", err)
	}
	if passwordHash != "first-password-hash" {
		t.Errorf("password hash = %q, want first-password-hash", passwordHash)
	}
	secondUser, _, err := store.FindUserByUsername(ctx, secondUsername)
	if err != nil {
		t.Fatalf("FindUserByUsername(second) error = %v", err)
	}

	conversation, err := store.CreateConversation(ctx, firstUser.ID, "Integration test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if _, err := store.FindConversation(ctx, secondUser.ID, conversation.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("FindConversation(other user) error = %v, want not found", err)
	}

	if _, err := store.CreateMessage(ctx, secondUser.ID, conversation.ID, "user", "not allowed", ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CreateMessage(other user) error = %v, want not found", err)
	}
	if _, err := store.CreateMessage(ctx, firstUser.ID, conversation.ID, "user", "hello", ""); err != nil {
		t.Fatalf("CreateMessage(user) error = %v", err)
	}
	if _, err := store.CreateMessage(ctx, firstUser.ID, conversation.ID, "assistant", "hello back", "qwen2.5:1.5b-instruct"); err != nil {
		t.Fatalf("CreateMessage(assistant) error = %v", err)
	}
	messages, err := store.ListMessages(ctx, firstUser.ID, conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].Content != "hello" || messages[1].Content != "hello back" {
		t.Fatalf("messages = %#v", messages)
	}

	tokenHash := sha256.Sum256([]byte("integration-session"))
	if err := store.CreateSession(ctx, firstUser.ID, tokenHash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	sessionUser, err := store.FindUserBySession(ctx, tokenHash)
	if err != nil {
		t.Fatalf("FindUserBySession() error = %v", err)
	}
	if sessionUser.ID != firstUser.ID {
		t.Errorf("session user ID = %q, want %q", sessionUser.ID, firstUser.ID)
	}
	if err := store.DeleteSession(ctx, tokenHash); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := store.FindUserBySession(ctx, tokenHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("FindUserBySession(deleted) error = %v, want not found", err)
	}

	expiredHash := sha256.Sum256([]byte("integration-expired-session"))
	if err := store.CreateSession(ctx, firstUser.ID, expiredHash, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("CreateSession(expired) error = %v", err)
	}
	if _, err := store.FindUserBySession(ctx, expiredHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("FindUserBySession(expired) error = %v, want not found", err)
	}
}
