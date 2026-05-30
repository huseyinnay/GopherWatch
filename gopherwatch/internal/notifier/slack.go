package notifier

import (
	"context"
	"net/http"
	"time"
)

const (
	colorSlackCritical = "danger"
	colorSlackWarning  = "warning"
	colorSlackInfo     = "good"
)

type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *SlackNotifier) Name() string { return "slack" }

type slackAttachment struct {
	Color string `json:"color"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type slackPayload struct {
	Attachments []slackAttachment `json:"attachments"`
}

func (s *SlackNotifier) Send(ctx context.Context, n Notification) error {
	payload := slackPayload{
		Attachments: []slackAttachment{{
			Color: slackColor(n.Level),
			Title: n.Title(),
			Text:  n.Body(),
		}},
	}
	return postJSON(ctx, s.client, s.webhookURL, payload)
}

func slackColor(l Level) string {
	switch l {
	case LevelCritical:
		return colorSlackCritical
	case LevelWarning:
		return colorSlackWarning
	default:
		return colorSlackInfo
	}
}
