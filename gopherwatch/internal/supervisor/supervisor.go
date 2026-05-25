package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/config"
	"github.com/huseyinnay/gopherwatch/internal/prober"
)

// Worker, supervisor'a "şu prober'ı bu sıklıkta çalıştır" diye söyleyen birim.
type Worker struct {
	Prober   prober.Prober
	Interval time.Duration
}

// WorkersFromConfig, config.Config'i Worker listesine çevirir.
// "Üretim" yolu; testte fake worker'ları doğrudan New'e veriyorum.
func WorkersFromConfig(cfg *config.Config) ([]Worker, error) {
	workers := make([]Worker, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		p, err := buildProber(t)
		if err != nil {
			return nil, fmt.Errorf("target %q: %w", t.Name, err)
		}
		workers = append(workers, Worker{
			Prober:   p,
			Interval: t.CheckInterval.Std(),
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
}

func New(logger *slog.Logger, workers []Worker) *Supervisor {
	bufSize := len(workers) * 2
	if bufSize == 0 {
		bufSize = 1
	}
	return &Supervisor{
		workers: workers,
		logger:  logger,
		results: make(chan prober.Result, bufSize),
	}
}

func (s *Supervisor) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	for _, w := range s.workers {
		wg.Add(1)
		go s.runWorker(ctx, &wg, w.Prober, w.Interval)
	}

	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for r := range s.results {
			s.logResult(r)
		}
	}()

	wg.Wait()
	close(s.results)
	<-collectorDone
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

func (s *Supervisor) logResult(r prober.Result) {
	attrs := []any{
		"target", r.Target,
		"status", r.Status.String(),
		"latency_ms", r.Latency.Milliseconds(),
	}
	if r.Err != nil {
		attrs = append(attrs, "err", r.Err.Error())
		s.logger.Warn("probe", attrs...)
		return
	}
	s.logger.Info("probe", attrs...)
}
