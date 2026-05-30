package reactor

import (
	"context"
	"testing"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/notifier"
	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

type captureNotifier struct {
	ch chan notifier.Notification
}

func newCaptureNotifier() *captureNotifier {
	return &captureNotifier{ch: make(chan notifier.Notification, 4)}
}

func (c *captureNotifier) Dispatch(n notifier.Notification) { c.ch <- n }

func runReactor(t *testing.T, restarter Restarter, policies map[string]RestartPolicy, cap *captureNotifier, e tracker.Event) {
	t.Helper()
	events := make(chan tracker.Event, 1)
	r := New(silentLogger(), events, restarter, policies, WithNotifier(cap))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()
	events <- e

	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func expectNotification(t *testing.T, cap *captureNotifier) notifier.Notification {
	t.Helper()
	select {
	case n := <-cap.ch:
		return n
	case <-time.After(time.Second):
		t.Fatal("bildirim bekleniyordu ama gelmedi")
		return notifier.Notification{}
	}
}

func expectNoNotification(t *testing.T, cap *captureNotifier) {
	t.Helper()
	select {
	case n := <-cap.ch:
		t.Fatalf("bildirim gelmemeliydi ama geldi: %+v", n)
	case <-time.After(150 * time.Millisecond):
		// beklenen
	}
}

func TestReactor_NotifiesOnUnhealthy(t *testing.T) {
	cap := newCaptureNotifier()
	runReactor(t, nil, nil, cap, unhealthyEvent("api"))

	n := expectNotification(t, cap)
	if n.Target != "api" || n.NewState != "UNHEALTHY" {
		t.Errorf("beklenmedik bildirim: %+v", n)
	}
	if n.Level != notifier.LevelCritical {
		t.Errorf("level = %v, critical bekleniyordu", n.Level)
	}
	if n.Detail != "" {
		t.Errorf("detay boş olmalıydı (restart yok), geldi: %q", n.Detail)
	}
}

func TestReactor_NotifiesWithRestartDetail(t *testing.T) {
	cap := newCaptureNotifier()
	fake := restarterFunc(func(ctx context.Context, container string, cooldown time.Duration) (bool, error) {
		return true, nil
	})
	policies := map[string]RestartPolicy{
		"api": {Container: "api-container", Cooldown: 30 * time.Second},
	}
	runReactor(t, fake, policies, cap, unhealthyEvent("api"))

	n := expectNotification(t, cap)
	if n.Detail != "restarted ✅" {
		t.Errorf("detay = %q, want %q", n.Detail, "restarted ✅")
	}
	if got := n.Summary(); got != "🚨 api UNHEALTHY → restarted ✅" {
		t.Errorf("Summary() = %q", got)
	}
}

func TestReactor_NotifiesOnRecovery(t *testing.T) {
	cap := newCaptureNotifier()
	e := tracker.Event{
		Target:    "api",
		OldState:  tracker.StateRecovering,
		NewState:  tracker.StateHealthy,
		Timestamp: time.Now(),
	}
	runReactor(t, nil, nil, cap, e)

	n := expectNotification(t, cap)
	if n.NewState != "HEALTHY" || n.Level != notifier.LevelInfo {
		t.Errorf("toparlanma bildirimi yanlış: %+v", n)
	}
}

func TestReactor_NoNotifyOnStartup(t *testing.T) {
	cap := newCaptureNotifier()
	e := tracker.Event{
		Target:    "api",
		OldState:  tracker.StateUnknown,
		NewState:  tracker.StateHealthy,
		Timestamp: time.Now(),
	}
	runReactor(t, nil, nil, cap, e)
	expectNoNotification(t, cap)
}

func TestReactor_NoNotifyOnRecovering(t *testing.T) {
	cap := newCaptureNotifier()
	e := tracker.Event{
		Target:    "api",
		OldState:  tracker.StateUnhealthy,
		NewState:  tracker.StateRecovering,
		Timestamp: time.Now(),
	}
	runReactor(t, nil, nil, cap, e)
	expectNoNotification(t, cap)
}
