package prober

import (
	"context"
	"time"
)

// Status, bir probe denemesinin sonucunu temsil eder.
type Status int

const (
	StatusUnknown Status = iota
	StatusOK
	StatusFail
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// Result, tek bir probe çağrısının çıktısı.
type Result struct {
	Target    string
	Status    Status
	Latency   time.Duration
	Err       error
	Timestamp time.Time
}

// Prober, bir hedefi kontrol edebilen her şeyin interface'i.
// Bu seviyede HTTP/TCP ayrımı yok; supervisor sadece Prober'a konuşur.
type Prober interface {
	Name() string
	Probe(ctx context.Context) Result
}
