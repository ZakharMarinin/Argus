package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"argus-frontend/internal/config"
)

// write кладёт конфиг во временный файл и отдаёт путь.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(body), 0o600)
	if err != nil {
		t.Fatalf("не записался конфиг: %v", err)
	}

	return path
}

// shared - тот самый общий конфиг из ТЗ §8: в нём есть секции обоих бинарников.
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

// TestForeignEnvVarsAreNotRequired - конфиг общий для collector и web
// (ТЗ §8). Дашборд не должен требовать переменные, которыми не пользуется:
// заставлять экспортировать ARGUS_TG_TOKEN ради запуска web неправильно.
func TestForeignEnvVarsAreNotRequired(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "http://backend.local:8081")
	t.Setenv("ARGUS_TG_TOKEN", "")

	cfg, err := config.Load(write(t, shared))
	if err != nil {
		t.Fatalf("конфиг не загрузился: %v", err)
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

// TestMissingBaseURLNamesTheVariable: если пусто именно то, что нужно web,
// ошибка должна называть переменную, а не отсылать читать конфиг.
func TestMissingBaseURLNamesTheVariable(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "")

	_, err := config.Load(write(t, shared))
	if err == nil {
		t.Fatal("конфиг без base_url загрузился")
	}
	if !strings.Contains(err.Error(), "ARGUS_BACKEND_URL") {
		t.Errorf("ошибка не называет переменную: %v", err)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "http://from-env:8081")

	cfg, err := config.Load(write(t, "backend:\n  base_url: http://from-file:8081\n"))
	if err != nil {
		t.Fatalf("конфиг не загрузился: %v", err)
	}

	if cfg.Backend.BaseURL != "http://from-env:8081" {
		t.Errorf("base_url = %q, хотели значение из окружения", cfg.Backend.BaseURL)
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
		t.Errorf("ошибка %v, ожидали подсказку про ARGUS_BACKEND_URL", err)
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := config.Load(write(t, "backend:\n  base_url: http://localhost:8081\n"))
	if err != nil {
		t.Fatalf("конфиг не загрузился: %v", err)
	}
	if cfg.Web.ListenAddr != ":8080" {
		t.Errorf("listen_addr = %q, ожидали :8080", cfg.Web.ListenAddr)
	}
	if cfg.Web.RetentionDays != 14 {
		t.Errorf("retention_days = %d, ожидали 14", cfg.Web.RetentionDays)
	}
	if cfg.Retention().Hours() != 14*24 {
		t.Errorf("Retention() = %v", cfg.Retention())
	}
	if cfg.Backend.TimeoutSeconds != 5 {
		t.Errorf("timeout_seconds = %d, ожидали 5", cfg.Backend.TimeoutSeconds)
	}
	if cfg.Timeout().Seconds() != 5 {
		t.Errorf("Timeout() = %v", cfg.Timeout())
	}
}

func TestInvalidRetention(t *testing.T) {
	t.Setenv("ARGUS_BACKEND_URL", "http://localhost:8081")

	_, err := config.Load(write(t, "backend:\n  base_url: \"${ARGUS_BACKEND_URL}\"\nweb:\n  retention_days: -1\n"))
	if err == nil || !strings.Contains(err.Error(), "retention_days") {
		t.Errorf("ошибка %v, ожидали жалобу на retention_days", err)
	}
}

func TestBrokenYAML(t *testing.T) {
	_, err := config.Load(write(t, "backend:\n  base_url: [не строка\n"))
	if err == nil {
		t.Fatal("битый YAML загрузился")
	}
}

func TestMissingFile(t *testing.T) {
	_, err := config.Load("нет-такого-файла.yaml")
	if err == nil {
		t.Fatal("отсутствующий файл загрузился")
	}
}
