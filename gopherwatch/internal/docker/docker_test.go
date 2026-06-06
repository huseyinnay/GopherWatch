package docker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

type fakeEngine struct {
	inspectErr error
	restartErr error
	running    bool

	inspectCalls int32
	restartCalls int32

	inFlight    int32
	maxInFlight int32

	gate chan struct{}
}

func (f *fakeEngine) ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error) {
	atomic.AddInt32(&f.inspectCalls, 1)
	if f.inspectErr != nil {
		return container.InspectResponse{}, f.inspectErr
	}
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:    "deadbeefcafe0123456789",
			Name:  "/" + id,
			State: &container.State{Running: f.running},
		},
	}, nil
}

func (f *fakeEngine) ContainerRestart(ctx context.Context, id string, options container.StopOptions) error {

	n := atomic.AddInt32(&f.inFlight, 1)
	for {
		mx := atomic.LoadInt32(&f.maxInFlight)
		if n <= mx || atomic.CompareAndSwapInt32(&f.maxInFlight, mx, n) {
			break
		}
	}
	defer atomic.AddInt32(&f.inFlight, -1)

	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	atomic.AddInt32(&f.restartCalls, 1)
	return f.restartErr
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

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForInFlight(t *testing.T, f *fakeEngine, want int32, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if atomic.LoadInt32(&f.inFlight) == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("inFlight %d bekleniyordu, şu an %d", want, atomic.LoadInt32(&f.inFlight))
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func TestManager_RestartInspectsThenRestarts(t *testing.T) {
	fake := &fakeEngine{running: true}
	m := newManager(fake, testLogger())

	ok, err := m.Restart(context.Background(), "web", 0)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if !ok {
		t.Fatal("restart yapılmalıydı (true beklendi)")
	}
	if got := atomic.LoadInt32(&fake.inspectCalls); got != 1 {
		t.Errorf("inspectCalls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&fake.restartCalls); got != 1 {
		t.Errorf("restartCalls = %d, want 1", got)
	}
}

func TestManager_CooldownSkips(t *testing.T) {
	fake := &fakeEngine{running: true}
	clk := &fakeClock{t: time.Unix(1000, 0)}
	m := newManager(fake, testLogger(), WithClock(clk.Now))

	if ok, err := m.Restart(context.Background(), "web", time.Minute); err != nil || !ok {
		t.Fatalf("ilk restart: ok=%v err=%v", ok, err)
	}
	ok, err := m.Restart(context.Background(), "web", time.Minute)
	if err != nil {
		t.Fatalf("ikinci restart beklenmeyen hata: %v", err)
	}
	if ok {
		t.Fatal("cooldown aktifken restart atlanmalıydı (false beklendi)")
	}
	if got := atomic.LoadInt32(&fake.restartCalls); got != 1 {
		t.Errorf("restartCalls = %d, want 1 (ikinci çağrı atlanmalıydı)", got)
	}
}

func TestManager_CooldownExpires(t *testing.T) {
	fake := &fakeEngine{running: true}
	clk := &fakeClock{t: time.Unix(1000, 0)}
	m := newManager(fake, testLogger(), WithClock(clk.Now))

	if ok, err := m.Restart(context.Background(), "web", time.Minute); err != nil || !ok {
		t.Fatalf("ilk restart: ok=%v err=%v", ok, err)
	}
	clk.advance(2 * time.Minute) // cooldown'ı geç

	ok, err := m.Restart(context.Background(), "web", time.Minute)
	if err != nil {
		t.Fatalf("ikinci restart beklenmeyen hata: %v", err)
	}
	if !ok {
		t.Fatal("cooldown dolunca restart yapılmalıydı (true beklendi)")
	}
	if got := atomic.LoadInt32(&fake.restartCalls); got != 2 {
		t.Errorf("restartCalls = %d, want 2", got)
	}
}

func TestManager_InspectErrorPropagates(t *testing.T) {
	errBoom := errors.New("inspect patladı")
	fake := &fakeEngine{inspectErr: errBoom}
	m := newManager(fake, testLogger())

	ok, err := m.Restart(context.Background(), "web", 0)
	if ok {
		t.Fatal("inspect başarısızken restart raporlanmamalı (false beklendi)")
	}
	if !errors.Is(err, errBoom) {
		t.Fatalf("hata sarmalanmadı: %v", err)
	}
	if got := atomic.LoadInt32(&fake.restartCalls); got != 0 {
		t.Errorf("restartCalls = %d, want 0 (inspect başarısızsa restart denenmemeli)", got)
	}
}

func TestManager_RestartErrorPropagates(t *testing.T) {
	errBoom := errors.New("restart patladı")
	fake := &fakeEngine{running: true, restartErr: errBoom}
	m := newManager(fake, testLogger())

	ok, err := m.Restart(context.Background(), "web", 0)
	if ok {
		t.Fatal("restart başarısızken false beklenir")
	}
	if !errors.Is(err, errBoom) {
		t.Fatalf("hata sarmalanmadı: %v", err)
	}
	if got := atomic.LoadInt32(&fake.inspectCalls); got != 1 {
		t.Errorf("inspectCalls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&fake.restartCalls); got != 1 {
		t.Errorf("restartCalls = %d, want 1", got)
	}
}

func TestManager_PerContainerMutexSerializes(t *testing.T) {
	fake := &fakeEngine{running: true, gate: make(chan struct{})}
	m := newManager(fake, testLogger())

	var wg sync.WaitGroup
	start := func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Restart(context.Background(), "web", 0)
		}()
	}

	start()

	waitForInFlight(t, fake, 1, time.Second)

	start()

	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&fake.maxInFlight); got != 1 {
		t.Fatalf("maxInFlight = %d, want 1 (mutex serileştirmeli)", got)
	}

	close(fake.gate)
	wg.Wait()

	if got := atomic.LoadInt32(&fake.restartCalls); got != 2 {
		t.Errorf("restartCalls = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&fake.maxInFlight); got != 1 {
		t.Errorf("maxInFlight = %d, want 1", got)
	}
}

func TestManager_DifferentContainersConcurrent(t *testing.T) {
	fake := &fakeEngine{running: true, gate: make(chan struct{})}
	m := newManager(fake, testLogger())

	var wg sync.WaitGroup
	for _, name := range []string{"web", "db"} {
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			_, _ = m.Restart(context.Background(), c, 0)
		}(name)
	}

	waitForInFlight(t, fake, 2, time.Second)
	if got := atomic.LoadInt32(&fake.maxInFlight); got != 2 {
		t.Fatalf("maxInFlight = %d, want 2 (farklı konteynerler paralel olmalı)", got)
	}

	close(fake.gate)
	wg.Wait()

	if got := atomic.LoadInt32(&fake.restartCalls); got != 2 {
		t.Errorf("restartCalls = %d, want 2", got)
	}
}
