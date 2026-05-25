package prober

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// httptest.NewServer, gerçek bir HTTP sunucusunu rastgele bir loopback
// portunda başlatır. URL'sini srv.URL'den alır. Test bitince Close
// kanalları kapatır, goroutine'leri toparlar.

func TestHTTPProber_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewHTTPProber("t", srv.URL, "GET", []int{200}, time.Second)
	r := p.Probe(context.Background())

	if r.Status != StatusOK {
		t.Errorf("status=%v, OK bekleniyordu; err=%v", r.Status, r.Err)
	}
	if r.Err != nil {
		t.Errorf("err=%v, nil bekleniyordu", r.Err)
	}
	if r.Latency <= 0 {
		t.Errorf("latency=%v, >0 bekleniyordu", r.Latency)
	}
}

func TestHTTPProber_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewHTTPProber("t", srv.URL, "GET", []int{200}, time.Second)
	r := p.Probe(context.Background())

	if r.Status != StatusFail {
		t.Errorf("status=%v, FAIL bekleniyordu", r.Status)
	}
	if r.Err == nil {
		t.Error("err nil olmamalıydı")
	}
}

func TestHTTPProber_MultipleExpectedStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted) // 202
	}))
	defer srv.Close()

	p := NewHTTPProber("t", srv.URL, "GET", []int{200, 201, 202}, time.Second)
	r := p.Probe(context.Background())

	if r.Status != StatusOK {
		t.Errorf("202, expectedStatus listesinde olmalıydı; got=%v err=%v", r.Status, r.Err)
	}
}

func TestHTTPProber_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewHTTPProber("t", srv.URL, "GET", []int{200}, 50*time.Millisecond)
	r := p.Probe(context.Background())

	if r.Status != StatusFail {
		t.Errorf("status=%v, FAIL bekleniyordu", r.Status)
	}
	// Latency timeout'a yakın olmalı, kesinlikle 500ms değil.
	if r.Latency > 300*time.Millisecond {
		t.Errorf("latency=%v, ~50ms timeout bekleniyordu", r.Latency)
	}
}

func TestHTTPProber_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	p := NewHTTPProber("t", srv.URL, "GET", []int{200}, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	r := p.Probe(ctx)
	if r.Status != StatusFail {
		t.Errorf("status=%v, cancel sonrası FAIL bekleniyordu", r.Status)
	}
	if r.Latency > 500*time.Millisecond {
		t.Errorf("latency=%v, hızlı cancel bekleniyordu", r.Latency)
	}
}

func TestHTTPProber_NetworkError(t *testing.T) {
	p := NewHTTPProber("t", "http://127.0.0.1:1", "GET", []int{200}, time.Second)
	r := p.Probe(context.Background())

	if r.Status != StatusFail {
		t.Errorf("status=%v, FAIL bekleniyordu", r.Status)
	}
	if r.Err == nil {
		t.Error("ulaşılamayan host için err nil olmamalıydı")
	}
}
