package prober

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPProber_OK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Accept döngüsü: gelen bağlantıları kabul edip kapat.
	// Listener Close edilince Accept hata döner, goroutine biter.
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	p := NewTCPProber("t", ln.Addr().String(), time.Second)
	r := p.Probe(context.Background())

	if r.Status != StatusOK {
		t.Errorf("status=%v, OK bekleniyordu; err=%v", r.Status, r.Err)
	}
}

func TestTCPProber_ConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	p := NewTCPProber("t", addr, time.Second)
	r := p.Probe(context.Background())

	if r.Status != StatusFail {
		t.Errorf("status=%v, FAIL bekleniyordu", r.Status)
	}
	if r.Err == nil {
		t.Error("err nil olmamalıydı")
	}
}

func TestTCPProber_FailsOnUnroutable(t *testing.T) {
	p := NewTCPProber("t", "192.0.2.1:80", 200*time.Millisecond)
	start := time.Now()
	r := p.Probe(context.Background())
	elapsed := time.Since(start)

	if r.Status != StatusFail {
		t.Errorf("status=%v, FAIL bekleniyordu", r.Status)
	}
	if elapsed > 2*time.Second {
		t.Errorf("elapsed=%v, çok uzun sürdü — timeout sızıyor olabilir", elapsed)
	}
}
