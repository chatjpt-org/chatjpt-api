package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"

	"github.com/chatjpt-org/chatjpt-api/internal/app"
	"github.com/chatjpt-org/chatjpt-api/internal/auth"
	"github.com/chatjpt-org/chatjpt-api/internal/store"
	"golang.org/x/term"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := app.LoadConfig()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	pool, err := store.Open(context.Background(), config.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "serve":
		server, err := app.NewServer(config, pool, logger)
		if err != nil {
			logger.Error("create server", "error", err)
			os.Exit(1)
		}
		if err := server.Serve(); err != nil {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	case "migrate":
		if err := store.ApplyMigrations(context.Background(), pool); err != nil {
			logger.Error("apply migrations", "error", err)
			os.Exit(1)
		}
		logger.Info("migrations applied")
	case "create-user":
		if err := createUser(context.Background(), pool); err != nil {
			logger.Error("create user", "error", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: chatjpt-api [serve|migrate|create-user]")
		os.Exit(2)
	}
}

func createUser(ctx context.Context, pool *store.Store) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read username: %w", err)
	}
	username = strings.TrimSpace(username)
	if err := auth.ValidateUsername(username); err != nil {
		return err
	}

	password, err := readPassword("Password: ")
	if err != nil {
		return err
	}
	confirmation, err := readPassword("Confirm password: ")
	if err != nil {
		return err
	}
	if password != confirmation {
		return errors.New("password confirmation does not match")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := pool.CreateUser(ctx, username, hash); err != nil {
		return err
	}

	fmt.Printf("User %q created.\n", username)
	return nil
}

func readPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if len(password) < 12 {
		return "", errors.New("password must have at least 12 characters")
	}
	return string(password), nil
}
