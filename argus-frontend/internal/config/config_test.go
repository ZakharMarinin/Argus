package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"argus-frontend/internal/config"
)

// write puts a config into a temporary file and returns the path.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(body), 0o600)
	if err != nil {
		t.Fatalf("config was not written: %v", err)
	}

	return path
}

// shared is the shared config from spec §8: it has sections for both binaries.
const shared = `
backend:
  base_url: "${ARGUS_BACKEND_URL}"

web:
  listen_addr: ":9000"
  retention_days: 30

telegram:
  bot_token: "${ARGUS_TG_TOKEN}"
  chat_id: 123456789

probes:
  - name: my-app
    url: https://myapp.local/health
`

// TestForeignEnvVarsAreNotRequired: the config is shared by collector and web
// (spec §8). The dashboard must not require variables it doesn't use:
// forcing people to export ARGUS_TG_TOKEN just to run web is wrong.
func TestForeignEnvVarsAreNotRequired(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "http://backend.local:8081")
	t.Setenv("ARGUS_TG_TOKEN", "")

	cfg, err := config.Load(write(t, shared))
	if err != nil {
		t.Fatalf("config did not load: %v", err)
	}
	if cfg.Backend.BaseURL != "http://backend.local:8081" {
		t.Errorf("backend.base_url = %q", cfg.Backend.BaseURL)
	}
	if cfg.Web.ListenAddr != ":9000" {
		t.Errorf("listen_addr = %q", cfg.Web.ListenAddr)
	}
	if cfg.Web.RetentionDays != 30 {
		t.Errorf("retention_days = %d", cfg.Web.RetentionDays)
	}
}

// TestMissingBaseURLNamesTheVariable: if exactly what web needs is empty,
// the error must name the variable rather than send you to read the config.
func TestMissingBaseURLNamesTheVariable(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "")

	_, err := config.Load(write(t, shared))
	if err == nil {
		t.Fatal("config without base_url loaded")
	}
	if !strings.Contains(err.Error(), "ARGUS_BACKEND_URL") {
		t.Errorf("error doesn't name the variable: %v", err)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "http://from-env:8081")

	cfg, err := config.Load(write(t, "backend:\n  base_url: http://from-file:8081\n"))
	if err != nil {
		t.Fatalf("config did not load: %v", err)
	}

	if cfg.Backend.BaseURL != "http://from-env:8081" {
		t.Errorf("base_url = %q, wanted the value from the environment", cfg.Backend.BaseURL)
	}
}

func TestUnexpandedPlaceholderIsRejected(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "")

	err := os.Unsetenv("ARGUS_BACKEND_URL")
	if err != nil {
		t.Fatal(err)
	}

	_, err = config.Load(write(t, shared))
	if err == nil || !strings.Contains(err.Error(), "ARGUS_BACKEND_URL") {
		t.Errorf("error %v, expected a hint about ARGUS_BACKEND_URL", err)
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := config.Load(write(t, "backend:\n  base_url: http://localhost:8081\n"))
	if err != nil {
		t.Fatalf("config did not load: %v", err)
	}
	if cfg.Web.ListenAddr != ":8080" {
		t.Errorf("listen_addr = %q, expected :8080", cfg.Web.ListenAddr)
	}
	if cfg.Web.RetentionDays != 14 {
		t.Errorf("retention_days = %d, expected 14", cfg.Web.RetentionDays)
	}
	if cfg.Retention().Hours() != 14*24 {
		t.Errorf("Retention() = %v", cfg.Retention())
	}
	if cfg.Backend.TimeoutSeconds != 5 {
		t.Errorf("timeout_seconds = %d, expected 5", cfg.Backend.TimeoutSeconds)
	}
	if cfg.Timeout().Seconds() != 5 {
		t.Errorf("Timeout() = %v", cfg.Timeout())
	}
}

func TestInvalidRetention(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "http://localhost:8081")

	_, err := config.Load(write(t, "backend:\n  base_url: \"${ARGUS_BACKEND_URL}\"\nweb:\n  retention_days: -1\n"))
	if err == nil || !strings.Contains(err.Error(), "retention_days") {
		t.Errorf("error %v, expected a complaint about retention_days", err)
	}
}

func TestBrokenYAML(t *testing.T) {
	_, err := config.Load(write(t, "backend:\n  base_url: [not a string\n"))
	if err == nil {
		t.Fatal("broken YAML loaded")
	}
}

func TestMissingFile(t *testing.T) {
	_, err := config.Load("no-such-file.yaml")
	if err == nil {
		t.Fatal("missing file loaded")
	}
}
