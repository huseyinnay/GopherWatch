package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/store"
)

var fixedNow = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

type fakeSource struct {
	statuses []store.Status
	events   []store.Event
}

func (f *fakeSource) Snapshot() []store.Status { return f.statuses }

func (f *fakeSource) EventsSince(t time.Time) []store.Event {
	out := make([]store.Event, 0, len(f.events))
	for _, e := range f.events {
		if !e.Timestamp.Before(t) {
			out = append(out, e)
		}
	}
	return out
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T, src Source) *httptest.Server {
	t.Helper()
	s := New(src, silentLogger(), WithClock(func() time.Time { return fixedNow }))
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return ts
}

func TestHandleStatus_JSON(t *testing.T) {
	src := &fakeSource{statuses: []store.Status{
		{Name: "api", State: "HEALTHY", LastLatency: 12 * time.Millisecond, TotalChecks: 10, TotalFailures: 1, ConsecutiveOK: 9},
		{Name: "db", State: "UNHEALTHY", LastError: "connection refused", ConsecutiveFails: 3, TotalChecks: 8, TotalFailures: 5},
	}}
	ts := newTestServer(t, src)

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type=%q, json bekleniyordu", ct)
	}

	var got statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("JSON decode: %v", err)
	}
	if len(got.Targets) != 2 {
		t.Fatalf("target sayısı=%d, 2 bekleniyordu", len(got.Targets))
	}
	if got.Targets[0].Name != "api" || got.Targets[0].State != "HEALTHY" {
		t.Errorf("ilk target=%+v", got.Targets[0])
	}
	if got.Targets[0].LatencyMS != 12 {
		t.Errorf("LatencyMS=%v, 12 bekleniyordu", got.Targets[0].LatencyMS)
	}
	if got.Targets[1].Error != "connection refused" {
		t.Errorf("ikinci target error=%q", got.Targets[1].Error)
	}
}

func TestHandleStatus_EmptyIsArrayNotNull(t *testing.T) {
	ts := newTestServer(t, &fakeSource{})
	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"targets": []`) {
		t.Errorf("boş hedef listesi [] olmalı, null değil:\n%s", body)
	}
}

func TestHandleDashboard_ServesHTML(t *testing.T) {
	ts := newTestServer(t, &fakeSource{})
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, 200 bekleniyordu", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type=%q, html bekleniyordu", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "gopherwatch") {
		t.Error("dashboard HTML 'gopherwatch' içermiyor — embed çalışmamış olabilir")
	}
}

func TestUnknownPath_404(t *testing.T) {
	ts := newTestServer(t, &fakeSource{})
	resp, err := http.Get(ts.URL + "/bilinmeyen")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d, 404 bekleniyordu", resp.StatusCode)
	}
}

func TestHandleEvents_WindowAndOrder(t *testing.T) {
	src := &fakeSource{events: []store.Event{
		{Target: "api", OldState: "UNKNOWN", NewState: "HEALTHY", Timestamp: fixedNow.Add(-10 * time.Minute)},
		{Target: "api", OldState: "HEALTHY", NewState: "UNHEALTHY", Timestamp: fixedNow.Add(-2 * time.Minute)},
		{Target: "api", OldState: "UNHEALTHY", NewState: "RECOVERING", Timestamp: fixedNow.Add(-30 * time.Second)},
	}}
	ts := newTestServer(t, src)

	resp, err := http.Get(ts.URL + "/events?since=5m")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got eventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Window != "5m0s" {
		t.Errorf("window=%q, '5m0s' bekleniyordu", got.Window)
	}
	if len(got.Events) != 2 {
		t.Fatalf("event sayısı=%d, 2 bekleniyordu (10dk öncesi pencere dışında)", len(got.Events))
	}
	// En yeni önce: ilk event -30s (RECOVERING) olmalı.
	if got.Events[0].NewState != "RECOVERING" {
		t.Errorf("ilk event=%s, en yeni (RECOVERING) bekleniyordu", got.Events[0].NewState)
	}
}

func TestHandleEvents_InvalidSince(t *testing.T) {
	ts := newTestServer(t, &fakeSource{})
	resp, err := http.Get(ts.URL + "/events?since=muz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d, 400 bekleniyordu", resp.StatusCode)
	}
}

func TestHandleMetrics_Format(t *testing.T) {
	src := &fakeSource{statuses: []store.Status{
		{Name: "api", State: "HEALTHY", TotalChecks: 5, TotalFailures: 1, LastLatency: 12 * time.Millisecond},
		{Name: "db", State: "UNHEALTHY", ConsecutiveFails: 3},
	}}
	ts := newTestServer(t, src)

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "version=0.0.4") {
		t.Errorf("Content-Type=%q, Prometheus formatı bekleniyordu", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	wants := []string{
		"# TYPE gopherwatch_target_up gauge",
		`gopherwatch_target_up{target="api"} 1`,
		`gopherwatch_target_up{target="db"} 0`,
		`gopherwatch_target_checks_total{target="api"} 5`,
		`gopherwatch_target_failures_total{target="api"} 1`,
		`gopherwatch_target_consecutive_fails{target="db"} 3`,
	}
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("metrics çıktısında bulunamadı: %q\n--- çıktı ---\n%s", w, text)
		}
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	s := New(&fakeSource{}, silentLogger(), WithAddr("127.0.0.1:0"))

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx) }()

	time.Sleep(40 * time.Millisecond)
	cancel()

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("graceful shutdown hata döndürdü: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run, context iptalinden sonra kapanmadı — sızıntı/asılma")
	}
}

func TestRun_BindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	s := New(&fakeSource{}, silentLogger(), WithAddr(ln.Addr().String()))
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("meşgul adreste Run nil döndürdü; bind hatası bekleniyordu")
	}
}
