package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "postgres://example")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" {
		t.Fatalf("default port = %q, want 8080", cfg.Port)
	}
}

func TestLoadInvalidPort(t *testing.T) {
	t.Setenv("PORT", "not-a-port")
	t.Setenv("DATABASE_URL", "postgres://example")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestLoadRequiresDatabase(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected database configuration error")
	}
}

func TestProductionRequiresHTTPS(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("ALLOW_COD", "true")
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_PUBLIC_URL", "http://example.com")
	t.Setenv("WEB_ORIGIN", "https://app.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("insecure API URL accepted in production")
	}
	t.Setenv("API_PUBLIC_URL", "https://api.example.com")
	t.Setenv("WEB_ORIGIN", "http://app.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("insecure web origin accepted in production")
	}
	t.Setenv("WEB_ORIGIN", "https://app.example.com")
	result, err := Load()
	if err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
	if !result.SecureCookies {
		t.Fatal("production session cookies not marked secure")
	}
}
func TestRejectOriginFragmentsAndCredentials(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("APP_ENV", "development")
	t.Setenv("API_PUBLIC_URL", "https://api.example.com#fragment")
	t.Setenv("WEB_ORIGIN", "https://app.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("URL fragment accepted")
	}
	t.Setenv("API_PUBLIC_URL", "https://user:password@api.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("URL credentials accepted")
	}
}
func TestRejectUnknownEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("APP_ENV", "prod")
	if _, err := Load(); err == nil {
		t.Fatal("invalid environment accepted")
	}
}

func TestProductionRejectsDevelopmentPassword(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://restaurant:restaurant_dev@db:5432/restaurant?sslmode=disable")
	t.Setenv("API_PUBLIC_URL", "https://restaurant.example")
	t.Setenv("WEB_ORIGIN", "https://restaurant.example")
	t.Setenv("ALLOW_COD", "true")
	if _, err := Load(); err == nil {
		t.Fatal("default local password accepted in production")
	}
}
