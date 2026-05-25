package supervisor

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/prober"
)

type fakeProber struct {
	name   string
	calls  atomic.Int64
	result prober.Result
}

func (f *fakeProber) Name() string { return f.name }

func (f *fakeProber) Probe(_ context.Context) prober.Result {
	f.calls.Add(1)
	r := f.result
	r.Target = f.name
	r.Timestamp = time.Now()
	return r
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSupervisor_WorkerTicks(t *testing.T) {
	fake := &fakeProber{result: prober.Result{Status: prober.StatusOK}}
	sup := New(silentLogger(), []Worker{
		{Prober: fake, Interval: 20 * time.Millisecond},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sup.Run(ctx)

	calls := fake.calls.Load()
	if calls < 3 || calls > 8 {
		t.Errorf("calls=%d, ~5-6 bekleniyordu", calls)
	}
}

func TestSupervisor_MultipleWorkersInParallel(t *testing.T) {
	a := &fakeProber{name: "a", result: prober.Result{Status: prober.StatusOK}}
	b := &fakeProber{name: "b", result: prober.Result{Status: prober.StatusOK}}

	sup := New(silentLogger(), []Worker{
		{Prober: a, Interval: 20 * time.Millisecond},
		{Prober: b, Interval: 20 * time.Millisecond},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sup.Run(ctx)

	ac, bc := a.calls.Load(), b.calls.Load()
	if ac < 3 || bc < 3 {
		t.Errorf("a=%d b=%d, ikisinin de >=3 olması bekleniyordu", ac, bc)
	}
	if ac > 2*bc || bc > 2*ac {
		t.Errorf("a=%d b=%d, dengesiz — paralel çalışmıyor olabilir", ac, bc)
	}
}

func TestSupervisor_CancelStopsWorkersPromptly(t *testing.T) {
	fake := &fakeProber{result: prober.Result{Status: prober.StatusOK}}
	sup := New(silentLogger(), []Worker{
		{Prober: fake, Interval: 10 * time.Second},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sup.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run, cancel sonrası 500ms içinde dönmedi — sızıntı var")
	}

	if fake.calls.Load() < 1 {
		t.Error("ilk anlık probe atılmamış")
	}
}

func TestSupervisor_EmptyWorkers(t *testing.T) {

	sup := New(silentLogger(), nil)

	done := make(chan struct{})
	go func() {
		sup.Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("boş worker listesiyle Run takıldı")
	}
}
