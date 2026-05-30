package notifier

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type Dispatcher struct {
	notifiers []Notifier
	logger    *slog.Logger

	rateLimit   time.Duration
	now         func() time.Time
	sendTimeout time.Duration

	mu       sync.Mutex
	lastSent map[string]time.Time
	closed   bool
	wg       sync.WaitGroup
}

type Option func(*Dispatcher)

func WithRateLimit(d time.Duration) Option {
	return func(disp *Dispatcher) {
		if d > 0 {
			disp.rateLimit = d
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(disp *Dispatcher) {
		if now != nil {
			disp.now = now
		}
	}
}

func WithSendTimeout(t time.Duration) Option {
	return func(disp *Dispatcher) {
		if t > 0 {
			disp.sendTimeout = t
		}
	}
}

func NewDispatcher(logger *slog.Logger, notifiers []Notifier, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		notifiers:   notifiers,
		logger:      logger,
		rateLimit:   time.Minute,
		now:         time.Now,
		sendTimeout: 10 * time.Second,
		lastSent:    make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

func (d *Dispatcher) Count() int { return len(d.notifiers) }

func (d *Dispatcher) Dispatch(n Notification) {
	if len(d.notifiers) == 0 {
		return
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	now := d.now()
	if last, ok := d.lastSent[n.Target]; ok && now.Sub(last) < d.rateLimit {
		d.mu.Unlock()
		d.logger.Debug("bildirim atlandı: rate limit",
			"target", n.Target, "since_last", now.Sub(last).Round(time.Second))
		return
	}
	d.lastSent[n.Target] = now
	d.wg.Add(len(d.notifiers))
	d.mu.Unlock()

	for _, nf := range d.notifiers {
		go d.send(nf, n)
	}
}

func (d *Dispatcher) send(nf Notifier, n Notification) {
	defer d.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), d.sendTimeout)
	defer cancel()

	if err := nf.Send(ctx, n); err != nil {
		d.logger.Warn("bildirim gönderilemedi",
			"notifier", nf.Name(), "target", n.Target, "err", err)
	}
}

func (d *Dispatcher) Close() error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()

	d.wg.Wait()
	return nil
}
