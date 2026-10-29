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
