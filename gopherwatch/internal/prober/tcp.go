package prober

import (
	"context"
	"net"
	"time"
)

type TCPProber struct {
	name    string
	address string
	timeout time.Duration
}

func NewTCPProber(name, address string, timeout time.Duration) *TCPProber {
	return &TCPProber{name: name, address: address, timeout: timeout}
}

func (t *TCPProber) Name() string { return t.name }

func (t *TCPProber) Probe(ctx context.Context) Result {
	start := time.Now()
	r := Result{Target: t.name, Timestamp: start}

	dialCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", t.address)
	r.Latency = time.Since(start)
	if err != nil {
		r.Status = StatusFail
		r.Err = err
		return r
	}
	conn.Close()

	r.Status = StatusOK
	return r
}
