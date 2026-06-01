package config

import "testing"

func TestLoad_HTTPValidEnabled(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://localhost/health
http:
  enabled: true
  addr: localhost:8090
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("beklenmedik hata: %v", err)
	}
	if !cfg.HTTP.Enabled {
		t.Error("http etkin olmalıydı")
	}
	if cfg.HTTP.Addr != "localhost:8090" {
		t.Errorf("addr=%q", cfg.HTTP.Addr)
	}
}

func TestLoad_HTTPEnabledDefaultAddr(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://localhost/health
http:
  enabled: true
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("beklenmedik hata: %v", err)
	}
	if cfg.HTTP.Addr != "localhost:8090" {
		t.Errorf("default addr uygulanmadı: %q", cfg.HTTP.Addr)
	}
}

func TestLoad_HTTPEnabledInvalidAddr(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://localhost/health
http:
  enabled: true
  addr: "portsuz-adres"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("geçersiz addr için hata bekleniyordu")
	}
}

func TestLoad_NoHTTPBlock(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://localhost/health
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("beklenmedik hata: %v", err)
	}
	if cfg.HTTP.Enabled {
		t.Error("http bloğu yokken kapalı olmalıydı")
	}
}
