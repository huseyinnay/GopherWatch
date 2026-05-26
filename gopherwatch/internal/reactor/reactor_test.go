package reactor

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
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

func TestReactor_LogsTransition(t *testing.T) {
	buf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	events := make(chan tracker.Event, 1)

	r := New(logger, events)
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := make(chan tracker.Event)
	r := New(logger, events)

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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := make(chan tracker.Event)
	r := New(logger, events)

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
