package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/config"
	"github.com/huseyinnay/gopherwatch/internal/httpapi"
	"github.com/huseyinnay/gopherwatch/internal/prober"
	"github.com/huseyinnay/gopherwatch/internal/reactor"
	"github.com/huseyinnay/gopherwatch/internal/store"
	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

type Worker struct {
	Prober           prober.Prober
	Interval         time.Duration
	FailureThreshold int

	Container string

	RestartCooldown time.Duration
}

func WorkersFromConfig(cfg *config.Config) ([]Worker, error) {
	workers := make([]Worker, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		p, err := buildProber(t)
		if err != nil {
			return nil, fmt.Errorf("target %q: %w", t.Name, err)
		}
		workers = append(workers, Worker{
			Prober:           p,
			Interval:         t.CheckInterval.Std(),
			FailureThreshold: t.FailureThreshold,
			Container:        t.Container,
			RestartCooldown:  t.RestartCooldown.Std(),
		})
	}
	return workers, nil
}

func buildProber(t config.Target) (prober.Prober, error) {
	switch t.Type {
	case config.TargetHTTP:
		return prober.NewHTTPProber(t.Name, t.URL, t.Method, t.ExpectedStatus, t.Timeout.Std()), nil
	case config.TargetTCP:
		return prober.NewTCPProber(t.Name, t.Address, t.Timeout.Std()), nil
	default:
		return nil, fmt.Errorf("bilinmeyen target type: %s", t.Type)
	}
}

type Option func(*Supervisor)

func WithRestarter(r reactor.Restarter) Option {
	return func(s *Supervisor) {
		s.restarter = r
	}
}

func WithNotifier(n reactor.Notifier) Option {
	return func(s *Supervisor) {
		s.notifier = n
	}
}

func WithDashboard(addr string) Option {
	return func(s *Supervisor) {
		s.dashboardAddr = addr
	}
}

type Supervisor struct {
	workers   []Worker
	logger    *slog.Logger
	results   chan prober.Result
	events    chan tracker.Event
	tracker   *tracker.Tracker
	restarter reactor.Restarter
	policies  map[string]reactor.RestartPolicy
	notifier  reactor.Notifier

	store         *store.Store
	dashboardAddr string
}

func New(logger *slog.Logger, workers []Worker, opts ...Option) *Supervisor {
	bufSize := len(workers) * 2
	if bufSize == 0 {
		bufSize = 1
	}

	thresholds := make(map[string]int, len(workers))
	policies := make(map[string]reactor.RestartPolicy)
	names := make([]string, 0, len(workers))
	for _, w := range workers {
		name := w.Prober.Name()
		names = append(names, name)
		thresholds[name] = w.FailureThreshold

		if w.Container != "" {
			policies[name] = reactor.RestartPolicy{
				Container: w.Container,
				Cooldown:  w.RestartCooldown,
			}
		}
	}

	s := &Supervisor{
		workers:  workers,
		logger:   logger,
		results:  make(chan prober.Result, bufSize),
		events:   make(chan tracker.Event, bufSize),
		tracker:  tracker.New(thresholds, nil),
		policies: policies,
		store:    store.New(names),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Supervisor) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	// Workers
	for _, w := range s.workers {
		wg.Add(1)
		go s.runWorker(ctx, &wg, w.Prober, w.Interval)
	}

	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for r := range s.results {
			s.logProbe(r)
			s.store.RecordProbe(r)
			event, changed := s.tracker.Observe(r)
			if !changed {
				continue
			}
			s.store.RecordEvent(event)
			select {
			case s.events <- event:
			case <-ctx.Done():

			}
		}
		close(s.events)
	}()

	react := reactor.New(s.logger, s.events, s.restarter, s.policies, reactor.WithNotifier(s.notifier))
	reactorDone := make(chan struct{})
	go func() {
		defer close(reactorDone)
		react.Run(ctx)
	}()

	var httpDone chan struct{}
	if s.dashboardAddr != "" {
		httpDone = make(chan struct{})
		srv := httpapi.New(s.store, s.logger, httpapi.WithAddr(s.dashboardAddr))
		go func() {
			defer close(httpDone)
			if err := srv.Run(ctx); err != nil {
				s.logger.Error("dashboard sunucusu durdu", "err", err)
			}
		}()
	}

	wg.Wait()
	close(s.results)
	<-collectorDone
	<-reactorDone
	if httpDone != nil {
		<-httpDone
	}
	return nil
}

func (s *Supervisor) runWorker(ctx context.Context, wg *sync.WaitGroup, p prober.Prober, interval time.Duration) {
	defer wg.Done()

	s.logger.Info("worker başladı", "target", p.Name(), "interval", interval)
	defer s.logger.Info("worker durdu", "target", p.Name())

	s.doProbe(ctx, p)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.doProbe(ctx, p)
		}
	}
}

func (s *Supervisor) doProbe(ctx context.Context, p prober.Prober) {
	r := p.Probe(ctx)
	select {
	case s.results <- r:
	case <-ctx.Done():
	}
}

func (s *Supervisor) logProbe(r prober.Result) {
	attrs := []any{
		"target", r.Target,
		"status", r.Status.String(),
		"latency_ms", r.Latency.Milliseconds(),
	}
	if r.Err != nil {
		attrs = append(attrs, "err", r.Err.Error())
	}
	s.logger.Debug("probe", attrs...)
}
