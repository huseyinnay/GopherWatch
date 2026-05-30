package notifier

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type fakeSink struct {
	name string
	gate chan struct{}
	err  error

	mu    sync.Mutex
	calls []Notification
}

func (f *fakeSink) Name() string { return f.name }

func (f *fakeSink) Send(ctx context.Context, n Notification) error {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, n)
	f.mu.Unlock()
	return f.err
}

func (f *fakeSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeSink) targets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Target
	}
	return out
}

func notif(target string) Notification {
	return Notification{Target: target, OldState: "HEALTHY", NewState: "UNHEALTHY", Level: LevelCritical}
}

func TestDispatcher_FanOut(t *testing.T) {
	s1 := &fakeSink{name: "a"}
	s2 := &fakeSink{name: "b"}
	d := NewDispatcher(testLogger(), []Notifier{s1, s2})

	if d.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", d.Count())
	}

	d.Dispatch(notif("api"))
	d.Close()

	if s1.count() != 1 || s2.count() != 1 {
		t.Fatalf("her iki sink de 1 bildirim almalıydı: a=%d b=%d", s1.count(), s2.count())
	}
}

func TestDispatcher_RateLimitSameTarget(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	sink := &fakeSink{name: "a"}
	d := NewDispatcher(testLogger(), []Notifier{sink},
		WithClock(clk.Now), WithRateLimit(time.Minute))

	d.Dispatch(notif("api"))
	d.Dispatch(notif("api"))
	d.Close()

	if sink.count() != 1 {
		t.Fatalf("rate limit içinde 1 bildirim bekleniyordu, %d geldi", sink.count())
	}
}

func TestDispatcher_RateLimitDifferentTargets(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	sink := &fakeSink{name: "a"}
	d := NewDispatcher(testLogger(), []Notifier{sink},
		WithClock(clk.Now), WithRateLimit(time.Minute))

	d.Dispatch(notif("api"))
	d.Dispatch(notif("redis"))
	d.Close()

	if sink.count() != 2 {
		t.Fatalf("farklı target'lar için 2 bildirim bekleniyordu, %d geldi", sink.count())
	}
}

func TestDispatcher_RateLimitExpires(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	sink := &fakeSink{name: "a"}
	d := NewDispatcher(testLogger(), []Notifier{sink},
		WithClock(clk.Now), WithRateLimit(time.Minute))

	d.Dispatch(notif("api"))
	clk.advance(2 * time.Minute)
	d.Dispatch(notif("api"))
	d.Close()

	if sink.count() != 2 {
		t.Fatalf("pencere dolunca 2 bildirim bekleniyordu, %d geldi", sink.count())
	}
}

func TestDispatcher_DispatchIsAsync(t *testing.T) {
	gate := make(chan struct{})
	sink := &fakeSink{name: "a", gate: gate}
	d := NewDispatcher(testLogger(), []Notifier{sink})

	d.Dispatch(notif("api"))

	if sink.count() != 0 {
		t.Fatalf("Dispatch bloklamamalı; gönderim henüz tamamlanmamış olmalı, count=%d", sink.count())
	}

	close(gate)
	d.Close()

	if sink.count() != 1 {
		t.Fatalf("gate açılınca 1 bildirim bekleniyordu, %d geldi", sink.count())
	}
}

func TestDispatcher_CloseWaitsForInflight(t *testing.T) {
	gate := make(chan struct{})
	sink := &fakeSink{name: "a", gate: gate}
	d := NewDispatcher(testLogger(), []Notifier{sink})

	d.Dispatch(notif("api"))

	closed := make(chan struct{})
	go func() {
		d.Close()
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("Close, uçuştaki gönderim bitmeden döndü")
	case <-time.After(100 * time.Millisecond):

	}

	close(gate)
	select {
	case <-closed:
		// başarı
	case <-time.After(time.Second):
		t.Fatal("gate açıldıktan sonra Close dönmedi")
	}
}

func TestDispatcher_SendErrorDoesNotBlockOthers(t *testing.T) {
	bad := &fakeSink{name: "bad", err: errors.New("boom")}
	good := &fakeSink{name: "good"}
	d := NewDispatcher(testLogger(), []Notifier{bad, good})

	d.Dispatch(notif("api"))
	d.Close()

	if good.count() != 1 {
		t.Fatalf("bir notifier hata verse de diğeri çağrılmalı: good=%d", good.count())
	}
}

func TestDispatcher_DispatchAfterCloseNoop(t *testing.T) {
	sink := &fakeSink{name: "a"}
	d := NewDispatcher(testLogger(), []Notifier{sink})

	d.Close()
	d.Dispatch(notif("api"))

	if sink.count() != 0 {
		t.Fatalf("Close sonrası Dispatch gönderim yapmamalı, count=%d", sink.count())
	}
}
