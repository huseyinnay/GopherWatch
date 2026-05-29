package reactor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

type Restarter interface {
	Restart(ctx context.Context, container string, cooldown time.Duration) (bool, error)
}

type RestartPolicy struct {
	Container string
	Cooldown  time.Duration
}

type Reactor struct {
	logger    *slog.Logger
	events    <-chan tracker.Event
	restarter Restarter
	policies  map[string]RestartPolicy
}

func New(logger *slog.Logger, events <-chan tracker.Event, restarter Restarter, policies map[string]RestartPolicy) *Reactor {
	return &Reactor{
		logger:    logger,
		events:    events,
		restarter: restarter,
		policies:  policies,
	}
}

func (r *Reactor) Run(ctx context.Context) {

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-r.events:
			if !ok {
				return
			}
			r.logTransition(ctx, e)
			r.maybeRestart(ctx, &wg, e)
		}
	}
}

func (r *Reactor) logTransition(ctx context.Context, e tracker.Event) {
	level := slog.LevelInfo
	if e.NewState == tracker.StateUnhealthy {
		level = slog.LevelWarn
	}
	r.logger.Log(ctx, level, "state transition",
		"target", e.Target,
		"from", e.OldState.String(),
		"to", e.NewState.String(),
		"consecutive_fails", e.ConsecutiveFails,
		"consecutive_ok", e.ConsecutiveOK,
	)
}

func (r *Reactor) maybeRestart(ctx context.Context, wg *sync.WaitGroup, e tracker.Event) {
	if e.NewState != tracker.StateUnhealthy {
		return
	}
	if r.restarter == nil {
		return
	}
	policy, ok := r.policies[e.Target]
	if !ok || policy.Container == "" {
		r.logger.Debug("restart atlanıyor: target için konteyner tanımlı değil", "target", e.Target)
		return
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		r.triggerRestart(ctx, e.Target, policy)
	}()
}

func (r *Reactor) triggerRestart(ctx context.Context, target string, p RestartPolicy) {
	restarted, err := r.restarter.Restart(ctx, p.Container, p.Cooldown)
	switch {
	case err != nil:
		r.logger.Error("restart başarısız",
			"target", target, "container", p.Container, "err", err)
	case !restarted:
		r.logger.Info("restart atlandı (cooldown aktif)",
			"target", target, "container", p.Container)
	default:
		r.logger.Info("konteyner restart edildi",
			"target", target, "container", p.Container)
	}
}
