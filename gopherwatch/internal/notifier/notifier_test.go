package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sampleRestartNotification() Notification {
	return Notification{
		Target:   "my-api",
		OldState: "HEALTHY",
		NewState: "UNHEALTHY",
		Level:    LevelCritical,
		Detail:   "restarted ✅",
	}
}

func TestNotification_Formatting(t *testing.T) {
	n := sampleRestartNotification()

	if got, want := n.Title(), "🚨 my-api UNHEALTHY"; got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
	if got, want := n.Summary(), "🚨 my-api UNHEALTHY → restarted ✅"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	if got, want := n.Body(), "HEALTHY → UNHEALTHY\nrestarted ✅"; got != want {
		t.Errorf("Body() = %q, want %q", got, want)
	}
}

func TestNotification_FormattingNoDetail(t *testing.T) {
	n := Notification{
		Target:   "redis",
		OldState: "RECOVERING",
		NewState: "HEALTHY",
		Level:    LevelInfo,
	}
	if got, want := n.Summary(), "✅ redis HEALTHY"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	if got, want := n.Body(), "RECOVERING → HEALTHY"; got != want {
		t.Errorf("Body() = %q, want %q", got, want)
	}
}

func TestLevel_EmojiAndString(t *testing.T) {
	tests := []struct {
		level Level
		emoji string
		str   string
	}{
		{LevelInfo, "✅", "info"},
		{LevelWarning, "⚠️", "warning"},
		{LevelCritical, "🚨", "critical"},
	}
	for _, tc := range tests {
		if got := tc.level.Emoji(); got != tc.emoji {
			t.Errorf("Level(%d).Emoji() = %q, want %q", tc.level, got, tc.emoji)
		}
		if got := tc.level.String(); got != tc.str {
			t.Errorf("Level(%d).String() = %q, want %q", tc.level, got, tc.str)
		}
	}
}

func capturingServer(t *testing.T, status int) (*httptest.Server, <-chan capturedRequest) {
	t.Helper()
	ch := make(chan capturedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		ch <- capturedRequest{
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

type capturedRequest struct {
	path        string
	contentType string
	body        []byte
}

func waitForRequest(t *testing.T, ch <-chan capturedRequest) capturedRequest {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("istek zamanında gelmedi")
		return capturedRequest{}
	}
}

func TestDiscordNotifier_Send(t *testing.T) {
	srv, ch := capturingServer(t, http.StatusNoContent)

	d := NewDiscordNotifier(srv.URL)
	if err := d.Send(context.Background(), sampleRestartNotification()); err != nil {
		t.Fatalf("Send hata döndü: %v", err)
	}

	req := waitForRequest(t, ch)
	if !strings.Contains(req.contentType, "application/json") {
		t.Errorf("Content-Type = %q, json bekleniyordu", req.contentType)
	}

	var payload discordPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("gövde JSON değil: %v (%s)", err, req.body)
	}
	if len(payload.Embeds) != 1 {
		t.Fatalf("1 embed bekleniyordu, %d geldi", len(payload.Embeds))
	}
	if payload.Embeds[0].Title != "🚨 my-api UNHEALTHY" {
		t.Errorf("embed title yanlış: %q", payload.Embeds[0].Title)
	}
	if payload.Embeds[0].Color != colorDiscordCritical {
		t.Errorf("embed rengi = %d, want %d", payload.Embeds[0].Color, colorDiscordCritical)
	}
}

func TestTelegramNotifier_Send(t *testing.T) {
	srv, ch := capturingServer(t, http.StatusOK)

	tn := NewTelegramNotifier("secret-token", "chat-42")
	tn.apiBase = srv.URL

	if err := tn.Send(context.Background(), sampleRestartNotification()); err != nil {
		t.Fatalf("Send hata döndü: %v", err)
	}

	req := waitForRequest(t, ch)
	if !strings.Contains(req.path, "secret-token") {
		t.Errorf("istek yolu token içermiyor: %q", req.path)
	}
	if !strings.HasSuffix(req.path, "/sendMessage") {
		t.Errorf("istek yolu /sendMessage ile bitmiyor: %q", req.path)
	}

	var payload telegramPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("gövde JSON değil: %v (%s)", err, req.body)
	}
	if payload.ChatID != "chat-42" {
		t.Errorf("chat_id = %q, want %q", payload.ChatID, "chat-42")
	}
	if payload.Text != "🚨 my-api UNHEALTHY → restarted ✅" {
		t.Errorf("text yanlış: %q", payload.Text)
	}
}

func TestSlackNotifier_Send(t *testing.T) {
	srv, ch := capturingServer(t, http.StatusOK)

	s := NewSlackNotifier(srv.URL)
	if err := s.Send(context.Background(), sampleRestartNotification()); err != nil {
		t.Fatalf("Send hata döndü: %v", err)
	}

	req := waitForRequest(t, ch)
	var payload slackPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("gövde JSON değil: %v (%s)", err, req.body)
	}
	if len(payload.Attachments) != 1 {
		t.Fatalf("1 attachment bekleniyordu, %d geldi", len(payload.Attachments))
	}
	if payload.Attachments[0].Color != colorSlackCritical {
		t.Errorf("attachment rengi = %q, want %q", payload.Attachments[0].Color, colorSlackCritical)
	}
	if !strings.Contains(payload.Attachments[0].Text, "restarted ✅") {
		t.Errorf("attachment text detay içermiyor: %q", payload.Attachments[0].Text)
	}
}

func TestNotifier_Non2xxIsError(t *testing.T) {
	srv, _ := capturingServer(t, http.StatusInternalServerError)

	d := NewDiscordNotifier(srv.URL)
	if err := d.Send(context.Background(), sampleRestartNotification()); err == nil {
		t.Fatal("5xx yanıt için hata bekleniyordu")
	}
}
