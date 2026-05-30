package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Level int

const (
	LevelInfo Level = iota
	LevelWarning
	LevelCritical
)

func (l Level) Emoji() string {
	switch l {
	case LevelCritical:
		return "🚨"
	case LevelWarning:
		return "⚠️"
	default:
		return "✅"
	}
}

func (l Level) String() string {
	switch l {
	case LevelCritical:
		return "critical"
	case LevelWarning:
		return "warning"
	default:
		return "info"
	}
}

type Notification struct {
	Target    string    // hangi target (örn. "my-api")
	OldState  string    // önceki durum, tracker.State.String() çıktısı (örn. "HEALTHY")
	NewState  string    // yeni durum (örn. "UNHEALTHY")
	Level     Level     // önem derecesi
	Detail    string    // opsiyonel ek bilgi (örn. "restarted");
	Timestamp time.Time // olayın zamanı
}

func (n Notification) Title() string {
	return fmt.Sprintf("%s %s %s", n.Level.Emoji(), n.Target, n.NewState)
}

func (n Notification) Summary() string {
	s := n.Title()
	if n.Detail != "" {
		s += " → " + n.Detail
	}
	return s
}

func (n Notification) Body() string {
	b := fmt.Sprintf("%s → %s", n.OldState, n.NewState)
	if n.Detail != "" {
		b += "\n" + n.Detail
	}
	return b
}

type Notifier interface {
	Name() string
	Send(ctx context.Context, n Notification) error
}

func postJSON(ctx context.Context, client *http.Client, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("payload marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("beklenmedik durum kodu: %d", resp.StatusCode)
	}
	return nil
}
