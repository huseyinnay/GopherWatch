package notifier

import (
	"context"
	"net/http"
	"time"
)

const (
	colorDiscordCritical = 0xE74C3C
	colorDiscordWarning  = 0xF1C40F
	colorDiscordInfo     = 0x2ECC71
)

// DiscordNotifier, bir Discord webhook URL'sine embed içeren JSON gönderir.
type DiscordNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewDiscordNotifier, verilen webhook URL'si için bir notifier kurar.
func NewDiscordNotifier(webhookURL string) *DiscordNotifier {
	return &DiscordNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *DiscordNotifier) Name() string { return "discord" }

// discordEmbed / discordPayload, Discord webhook API'sinin beklediği body.
type discordEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       int    `json:"color"`
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

func (d *DiscordNotifier) Send(ctx context.Context, n Notification) error {
	payload := discordPayload{
		Embeds: []discordEmbed{{
			Title:       n.Title(),
			Description: n.Body(),
			Color:       discordColor(n.Level),
		}},
	}
	return postJSON(ctx, d.client, d.webhookURL, payload)
}

func discordColor(l Level) int {
	switch l {
	case LevelCritical:
		return colorDiscordCritical
	case LevelWarning:
		return colorDiscordWarning
	default:
		return colorDiscordInfo
	}
}
