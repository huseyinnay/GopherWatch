package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/config"
	"github.com/huseyinnay/gopherwatch/internal/prober"
	"github.com/huseyinnay/gopherwatch/internal/reactor"
	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

// Worker, supervisor'a "şu prober'ı bu sıklıkta çalıştır" diye söyleyen birim.
type Worker struct {
	Prober           prober.Prober
	Interval         time.Duration
	FailureThreshold int
}

// WorkersFromConfig, config.Config'i Worker listesine çevirir.
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

type Supervisor struct {
	workers []Worker
	logger  *slog.Logger
	results chan prober.Result
	events  chan tracker.Event
	tracker *tracker.Tracker
}

func New(logger *slog.Logger, workers []Worker) *Supervisor {
	bufSize := len(workers) * 2
	if bufSize == 0 {
		bufSize = 1
	}

	thresholds := make(map[string]int, len(workers))
	for _, w := range workers {
		thresholds[w.Prober.Name()] = w.FailureThreshold
	}

	return &Supervisor{
		workers: workers,
		logger:  logger,
		results: make(chan prober.Result, bufSize),
		events:  make(chan tracker.Event, bufSize),
		tracker: tracker.New(thresholds, nil),
	}
}

// Run, workers + tracker collector + reactor üçlüsünü başlatır.
//
// Kanal akışı:
//
//	workers --> results --> [collector: log + tracker.Observe] --> events --> reactor
//
// Ölüm sırası:
//  1. ctx iptal olur -> workers ctx.Done()'a düşer ve biter.
//  2. wg.Wait() workers bitene kadar bekler.
//  3. close(results) -> collector range döngüsünden çıkar.
//  4. close(events) -> reactor range döngüsünden çıkar.
//  5. Hepsi bitince Run döner.
func (s *Supervisor) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	// Workers
	for _, w := range s.workers {
		wg.Add(1)
		go s.runWorker(ctx, &wg, w.Prober, w.Interval)
	}

	// Collector: results -> tracker -> events
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for r := range s.results {
			s.logProbe(r)
			event, changed := s.tracker.Observe(r)
			if !changed {
				continue
			}
			select {
			case s.events <- event:
			case <-ctx.Done():
				// Shutdown sırasında event drop'u kabul edilebilir;
				// reactor zaten kapanmış olabilir, deadlock'a girmeyelim.
			}
		}
		close(s.events)
	}()

	// Reactor
	react := reactor.New(s.logger, s.events)
	reactorDone := make(chan struct{})
	go func() {
		defer close(reactorDone)
		react.Run(ctx)
	}()

	wg.Wait()
	close(s.results)
	<-collectorDone
	<-reactorDone
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

// logProbe, ham probe sonucunu Debug seviyesinde basar.
// State transition'ları zaten reactor Info/Warn ile basıyor — burada
// spam yapmayalım, ama gerektiğinde log_level: debug ile detayı açabilelim.
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
