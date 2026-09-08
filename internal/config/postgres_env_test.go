package config

import (
	"strings"
	"testing"
)

func TestPostgresURLFromEnv(t *testing.T) {
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_USER", "openusenetserver")
	t.Setenv("POSTGRES_PASSWORD", "p@ss:word/x")
	t.Setenv("POSTGRES_DB", "openusenet")
	got := postgresURLFromEnv()
	if !strings.Contains(got, "openusenetserver") {
		t.Fatalf("user: %s", got)
	}
	if !strings.Contains(got, "postgres:5432") {
		t.Fatalf("host: %s", got)
	}
	if !strings.Contains(got, "p%40ss%3Aword%2Fx") {
		t.Fatalf("password should be url-encoded: %s", got)
	}
}

func TestPostgresURLFromEnvRequiresHost(t *testing.T) {
	t.Setenv("POSTGRES_HOST", "")
	t.Setenv("POSTGRES_USER", "openusenet")
	t.Setenv("POSTGRES_PASSWORD", "foo")
	t.Setenv("POSTGRES_DB", "openusenet")
	if postgresURLFromEnv() != "" {
		t.Fatal("empty POSTGRES_HOST should not build a URL")
	}
}
