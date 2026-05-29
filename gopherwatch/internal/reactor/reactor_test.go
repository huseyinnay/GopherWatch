package reactor

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type restarterFunc func(ctx context.Context, container string, cooldown time.Duration) (bool, error)

func (f restarterFunc) Restart(ctx context.Context, container string, cooldown time.Duration) (bool, error) {
	return f(ctx, container, cooldown)
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func unhealthyEvent(target string) tracker.Event {
	return tracker.Event{
		Target:           target,
		OldState:         tracker.StateHealthy,
		NewState:         tracker.StateUnhealthy,
		ConsecutiveFails: 3,
		Timestamp:        time.Now(),
	}
}

func TestReactor_LogsTransition(t *testing.T) {
	buf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	events := make(chan tracker.Event, 1)

	r := New(logger, events, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()

	events <- tracker.Event{
		Target:           "api",
		OldState:         tracker.StateHealthy,
		NewState:         tracker.StateUnhealthy,
		ConsecutiveFails: 3,
		Timestamp:        time.Now(),
	}

	deadline := time.After(500 * time.Millisecond)
	for {
		if strings.Contains(buf.String(), "UNHEALTHY") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("event log'a düşmedi: %q", buf.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	<-done

	out := buf.String()
	if !strings.Contains(out, "target=api") {
		t.Errorf("log target içermiyor: %q", out)
	}
	if !strings.Contains(out, "HEALTHY") || !strings.Contains(out, "UNHEALTHY") {
		t.Errorf("log eski/yeni state içermiyor: %q", out)
	}
}

func TestReactor_StopsWhenChannelClosed(t *testing.T) {
	events := make(chan tracker.Event)
	r := New(silentLogger(), events, nil, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(context.Background())
	}()

	close(events)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("reactor, kanal kapanınca çıkmadı")
	}
}

func TestReactor_StopsOnContextCancel(t *testing.T) {
	events := make(chan tracker.Event)
	r := New(silentLogger(), events, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("reactor, ctx cancel sonrası çıkmadı")
	}
}

func TestReactor_RestartsOnUnhealthy(t *testing.T) {
	type call struct {
		container string
		cooldown  time.Duration
	}
	calls := make(chan call, 1)
	fake := restarterFunc(func(ctx context.Context, container string, cooldown time.Duration) (bool, error) {
		calls <- call{container, cooldown}
		return true, nil
	})

	events := make(chan tracker.Event, 1)
	policies := map[string]RestartPolicy{
		"api": {Container: "api-container", Cooldown: 30 * time.Second},
	}
	r := New(silentLogger(), events, fake, policies)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()

	events <- unhealthyEvent("api")

	select {
	case c := <-calls:
		if c.container != "api-container" {
			t.Errorf("yanlış konteyner: got %q, want %q", c.container, "api-container")
		}
		if c.cooldown != 30*time.Second {
			t.Errorf("yanlış cooldown: got %v, want %v", c.cooldown, 30*time.Second)
		}
	case <-time.After(time.Second):
		t.Fatal("UNHEALTHY geçişinde restart çağrılmadı")
	}

	cancel()
	<-done
}

func TestReactor_NoRestartOnNonUnhealthy(t *testing.T) {
	tests := []struct {
		name     string
		newState tracker.State
	}{
		{"recovering", tracker.StateRecovering},
		{"healthy", tracker.StateHealthy},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := make(chan struct{}, 1)
			fake := restarterFunc(func(ctx context.Context, container string, cooldown time.Duration) (bool, error) {
				called <- struct{}{}
				return true, nil
			})
			events := make(chan tracker.Event, 1)
			policies := map[string]RestartPolicy{
				"api": {Container: "api-container", Cooldown: 30 * time.Second},
			}
			r := New(silentLogger(), events, fake, policies)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				r.Run(ctx)
			}()

			events <- tracker.Event{
				Target:    "api",
				OldState:  tracker.StateUnhealthy,
				NewState:  tc.newState,
				Timestamp: time.Now(),
			}

			select {
			case <-called:
				t.Fatal("UNHEALTHY olmayan geçişte restart çağrıldı")
			case <-time.After(100 * time.Millisecond):

			}
			cancel()
			<-done
		})
	}
}

func TestReactor_NoRestartWhenNoContainer(t *testing.T) {
	called := make(chan struct{}, 1)
	fake := restarterFunc(func(ctx context.Context, container string, cooldown time.Duration) (bool, error) {
		called <- struct{}{}
		return true, nil
	})
	events := make(chan tracker.Event, 1)

	r := New(silentLogger(), events, fake, map[string]RestartPolicy{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()

	events <- unhealthyEvent("api")

	select {
	case <-called:
		t.Fatal("konteyner tanımlı olmadan restart çağrıldı")
	case <-time.After(100 * time.Millisecond):

	}
	cancel()
	<-done
}

func TestReactor_WaitsForInflightRestart(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool

	fake := restarterFunc(func(ctx context.Context, container string, cooldown time.Duration) (bool, error) {
		close(started)
		<-release
		finished.Store(true)
		return true, nil
	})

	events := make(chan tracker.Event, 1)
	policies := map[string]RestartPolicy{
		"api": {Container: "api-container", Cooldown: 0},
	}
	r := New(silentLogger(), events, fake, policies)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		r.Run(ctx)
	}()

	events <- unhealthyEvent("api")
	<-started

	cancel()

	select {
	case <-runDone:
		t.Fatal("Run, in-flight restart bitmeden döndü")
	case <-time.After(100 * time.Millisecond):

	}
	if finished.Load() {
		t.Fatal("restart beklenenden erken bitti")
	}

	close(release)
	select {
	case <-runDone:
		// başarı
	case <-time.After(time.Second):
		t.Fatal("restart bittikten sonra Run dönmedi")
	}
	if !finished.Load() {
		t.Fatal("restart tamamlanmadı")
	}
}
