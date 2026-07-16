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
	MemberModels    []string
	AdminModels     []string
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
	memberModels, err := modelList(environmentOr("JCHAT_MEMBER_MODELS", environmentOr("JCHAT_ALLOWED_MODELS", defaultModel)))
	if err != nil {
		return Config{}, fmt.Errorf("JCHAT_MEMBER_MODELS: %w", err)
	}
	adminModels, err := optionalModelList(os.Getenv("JCHAT_ADMIN_MODELS"))
	if err != nil {
		return Config{}, fmt.Errorf("JCHAT_ADMIN_MODELS: %w", err)
	}

	config := Config{
		Address:         environmentOr("JCHAT_API_ADDR", ":8080"),
		DatabaseURL:     databaseURL,
		CookieSecure:    cookieSecure,
		SessionDuration: 7 * 24 * time.Hour,
		GatewayURL:      environmentOr("JCHAT_GATEWAY_URL", ""),
		GatewayAccessID: environmentOr("JCHAT_GATEWAY_ACCESS_ID", ""),
		GatewaySecret:   environmentOr("JCHAT_GATEWAY_ACCESS_SECRET", ""),
		MemberModels:    memberModels,
		AdminModels:     adminModels,
	}
	if err := config.validateGatewayCredentials(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func modelList(value string) ([]string, error) {
	models := make([]string, 0)
	seen := make(map[string]struct{})
	for _, candidate := range strings.Split(value, ",") {
		model := strings.TrimSpace(candidate)
		if model == "" {
			return nil, errors.New("model identifiers cannot be empty")
		}
		if _, exists := seen[model]; exists {
			return nil, fmt.Errorf("model %q is configured more than once", model)
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models, nil
}

func optionalModelList(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return modelList(value)
}

func (c Config) validateGatewayCredentials() error {
	configured := 0
	for _, value := range []string{c.GatewayURL, c.GatewayAccessID, c.GatewaySecret} {
		if value != "" {
			configured++
		}
	}
	if configured != 0 && configured != 3 {
		return errors.New("JCHAT_GATEWAY_URL, JCHAT_GATEWAY_ACCESS_ID, and JCHAT_GATEWAY_ACCESS_SECRET must be configured together")
	}
	return nil
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
