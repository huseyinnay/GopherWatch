package notifier

import (
	"context"
	"net/http"
	"time"
)

type TelegramNotifier struct {
	token   string
	chatID  string
	client  *http.Client
	apiBase string
}

func NewTelegramNotifier(token, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		token:   token,
		chatID:  chatID,
		client:  &http.Client{Timeout: 10 * time.Second},
		apiBase: "https://api.telegram.org",
	}
}

func (t *TelegramNotifier) Name() string { return "telegram" }

type telegramPayload struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

func (t *TelegramNotifier) Send(ctx context.Context, n Notification) error {
	url := t.apiBase + "/bot" + t.token + "/sendMessage"
	payload := telegramPayload{
		ChatID: t.chatID,
		Text:   n.Summary(),
	}
	return postJSON(ctx, t.client, url, payload)
}
