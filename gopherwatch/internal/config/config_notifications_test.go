package config

import (
	"testing"
	"time"
)

func TestLoad_NotificationsValid(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://localhost/health
notifications:
  rate_limit: 60s
  discord:
    enabled: true
    webhook_url: https://discord.com/api/webhooks/1/abc
  telegram:
    enabled: false
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("beklenmedik hata: %v", err)
	}
	if cfg.Notifications.RateLimit.Std() != 60*time.Second {
		t.Errorf("rate_limit parse edilmedi: %v", cfg.Notifications.RateLimit.Std())
	}
	if cfg.Notifications.Discord == nil || !cfg.Notifications.Discord.Enabled {
		t.Fatal("discord etkin olmalıydı")
	}
	if cfg.Notifications.Discord.WebhookURL == "" {
		t.Error("discord webhook_url boş kaldı")
	}
	if cfg.Notifications.Telegram == nil || cfg.Notifications.Telegram.Enabled {
		t.Error("telegram tanımlı ve kapalı olmalıydı")
	}
	if cfg.Notifications.Slack != nil {
		t.Error("slack tanımlı değildi, nil olmalıydı")
	}
}

func TestLoad_DiscordEnabledNoWebhook(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://localhost/health
notifications:
  discord:
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("webhook_url'sü olmayan etkin discord için hata bekleniyordu")
	}
}

func TestLoad_TelegramEnabledMissingChatID(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://localhost/health
notifications:
  telegram:
    enabled: true
    bot_token: "123:abc"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("chat_id'si olmayan etkin telegram için hata bekleniyordu")
	}
}

func TestLoad_DisabledChannelAllowsEmptyFields(t *testing.T) {
	path := writeTemp(t, `
targets:
  - name: api
    type: http
    url: http://localhost/health
notifications:
  discord:
    enabled: false
  slack:
    enabled: false
`)
	if _, err := Load(path); err != nil {
		t.Fatalf("kapalı kanalların boş alanları hata vermemeli: %v", err)
	}
}

func TestLoad_NoNotificationsBlock(t *testing.T) {
	// Bildirim bloğu hiç olmayan config geçerli olmalı (geriye uyumluluk).
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
	if cfg.Notifications.Discord != nil || cfg.Notifications.Telegram != nil || cfg.Notifications.Slack != nil {
		t.Error("hiçbir kanal tanımlı olmamalıydı")
	}
}
