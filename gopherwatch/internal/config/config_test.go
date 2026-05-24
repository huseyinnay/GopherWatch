package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_Valid(t *testing.T) {
	path := writeTemp(t, `
global:
  check_interval: 10s
targets:
  - name: api
    type: http
    url: http://localhost:8080/health
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("beklenmedik hata: %v", err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("1 target bekleniyordu, %d geldi", len(cfg.Targets))
	}
	// Default'ların miras alındığını doğrula
	if cfg.Targets[0].CheckInterval.Std() != 10*time.Second {
		t.Errorf("check_interval global'den miras alınmamış")
	}
	if cfg.Targets[0].Method != "GET" {
		t.Errorf("method default'u GET olmalıydı, %q geldi", cfg.Targets[0].Method)
	}
}

func TestLoad_DuplicateName(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://a
  - name: api
    type: http
    url: http://b
`)
	if _, err := Load(path); err == nil {
		t.Fatal("tekrar eden isim için hata bekleniyordu")
	}
}

func TestLoad_MissingURL(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
`)
	if _, err := Load(path); err == nil {
		t.Fatal("eksik url için hata bekleniyordu")
	}
}
