package app

import "testing"

func TestLoadConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://jchat:test@localhost:5432/jchat")
	t.Setenv("JCHAT_API_ADDR", "127.0.0.1:8081")
	t.Setenv("JCHAT_COOKIE_SECURE", "false")

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.Address != "127.0.0.1:8081" {
		t.Errorf("Address = %q, want 127.0.0.1:8081", config.Address)
	}
	if config.CookieSecure {
		t.Error("CookieSecure = true, want false")
	}
}

func TestLoadConfigRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
}
