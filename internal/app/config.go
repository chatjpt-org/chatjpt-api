package app

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address         string
	DatabaseURL     string
	CookieSecure    bool
	SessionDuration time.Duration
	GatewayURL      string
	GatewayAccessID string
	GatewaySecret   string
}

func LoadConfig() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	cookieSecure, err := environmentBool("JCHAT_COOKIE_SECURE", true)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Address:         environmentOr("JCHAT_API_ADDR", ":8080"),
		DatabaseURL:     databaseURL,
		CookieSecure:    cookieSecure,
		SessionDuration: 7 * 24 * time.Hour,
		GatewayURL:      environmentOr("JCHAT_GATEWAY_URL", ""),
		GatewayAccessID: environmentOr("JCHAT_GATEWAY_ACCESS_ID", ""),
		GatewaySecret:   environmentOr("JCHAT_GATEWAY_ACCESS_SECRET", ""),
	}, nil
}

func environmentOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func environmentBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return parsed, nil
}
