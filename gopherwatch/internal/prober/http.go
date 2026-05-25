package prober

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HTTPProber struct {
	name           string
	url            string
	method         string
	expectedStatus []int
	client         *http.Client
}

func NewHTTPProber(name, url, method string, expectedStatus []int, timeout time.Duration) *HTTPProber {
	return &HTTPProber{
		name:           name,
		url:            url,
		method:         method,
		expectedStatus: expectedStatus,
		client:         &http.Client{Timeout: timeout},
	}
}

func (h *HTTPProber) Name() string { return h.name }

func (h *HTTPProber) Probe(ctx context.Context) Result {
	start := time.Now()
	r := Result{Target: h.name, Timestamp: start}

	req, err := http.NewRequestWithContext(ctx, h.method, h.url, nil)
	if err != nil {
		r.Status = StatusFail
		r.Err = fmt.Errorf("build request: %w", err)
		r.Latency = time.Since(start)
		return r
	}

	resp, err := h.client.Do(req)
	r.Latency = time.Since(start)
	if err != nil {
		r.Status = StatusFail
		r.Err = err
		return r
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	for _, code := range h.expectedStatus {
		if resp.StatusCode == code {
			r.Status = StatusOK
			return r
		}
	}
	r.Status = StatusFail
	r.Err = fmt.Errorf("beklenmedik status: %d", resp.StatusCode)
	return r
}
