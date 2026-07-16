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

func TestLoadConfigRequiresAllGatewayValues(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://jchat:test@localhost:5432/jchat")
	t.Setenv("JCHAT_GATEWAY_URL", "https://ai.example.com")
	t.Setenv("JCHAT_GATEWAY_ACCESS_ID", "client-id")
	t.Setenv("JCHAT_GATEWAY_ACCESS_SECRET", "")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want error for partial gateway configuration")
	}
}

func TestLoadConfigAcceptsCompleteGatewayValues(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://jchat:test@localhost:5432/jchat")
	t.Setenv("JCHAT_GATEWAY_URL", "https://ai.example.com")
	t.Setenv("JCHAT_GATEWAY_ACCESS_ID", "client-id")
	t.Setenv("JCHAT_GATEWAY_ACCESS_SECRET", "client-secret")

	if _, err := LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
}

func TestLoadConfigSeparatesMemberAndAdminModels(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://jchat:test@localhost:5432/jchat")
	t.Setenv("JCHAT_MEMBER_MODELS", "qwen2.5:1.5b-instruct")
	t.Setenv("JCHAT_ADMIN_MODELS", "qwen3:4b-instruct")

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(config.MemberModels) != 1 || config.MemberModels[0] != "qwen2.5:1.5b-instruct" {
		t.Fatalf("MemberModels = %#v", config.MemberModels)
	}
	if len(config.AdminModels) != 1 || config.AdminModels[0] != "qwen3:4b-instruct" {
		t.Fatalf("AdminModels = %#v", config.AdminModels)
	}
}

func TestModelListRejectsDuplicateModels(t *testing.T) {
	_, err := modelList("qwen2.5:1.5b-instruct,qwen2.5:1.5b-instruct")
	if err == nil {
		t.Fatal("modelList() error = nil, want duplicate model error")
	}
}
