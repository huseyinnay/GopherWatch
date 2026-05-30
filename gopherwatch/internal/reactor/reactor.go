package reactor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/notifier"
	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

type Restarter interface {
	Restart(ctx context.Context, container string, cooldown time.Duration) (bool, error)
}

type Notifier interface {
	Dispatch(n notifier.Notification)
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
	notifier  Notifier
}

type Option func(*Reactor)

func WithNotifier(n Notifier) Option {
	return func(r *Reactor) {
		if n != nil {
			r.notifier = n
		}
	}
}

func New(logger *slog.Logger, events <-chan tracker.Event, restarter Restarter, policies map[string]RestartPolicy, opts ...Option) *Reactor {
	r := &Reactor{
		logger:    logger,
		events:    events,
		restarter: restarter,
		policies:  policies,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
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
			r.handle(ctx, &wg, e)
		}
	}
}

func (r *Reactor) handle(ctx context.Context, wg *sync.WaitGroup, e tracker.Event) {
	r.logTransition(ctx, e)

	level, notify := classify(e)

	if e.NewState == tracker.StateUnhealthy && r.restarter != nil {
		policy, ok := r.policies[e.Target]
		if ok && policy.Container != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				detail := r.triggerRestart(ctx, e.Target, policy)
				if notify {
					r.dispatch(e, level, detail)
				}
			}()
			return
		}
		r.logger.Debug("restart atlanıyor: target için konteyner tanımlı değil", "target", e.Target)
	}

	if notify {
		r.dispatch(e, level, "")
	}
}

func classify(e tracker.Event) (notifier.Level, bool) {
	switch {
	case e.NewState == tracker.StateUnhealthy:
		return notifier.LevelCritical, true
	case e.NewState == tracker.StateHealthy && e.OldState == tracker.StateRecovering:
		return notifier.LevelInfo, true
	default:
		return notifier.LevelInfo, false
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

func (r *Reactor) triggerRestart(ctx context.Context, target string, p RestartPolicy) string {
	restarted, err := r.restarter.Restart(ctx, p.Container, p.Cooldown)
	switch {
	case err != nil:
		r.logger.Error("restart başarısız",
			"target", target, "container", p.Container, "err", err)
		return "restart failed ❌"
	case !restarted:
		r.logger.Info("restart atlandı (cooldown aktif)",
			"target", target, "container", p.Container)
		return "restart skipped (cooldown) ⏳"
	default:
		r.logger.Info("konteyner restart edildi",
			"target", target, "container", p.Container)
		return "restarted ✅"
	}
}

func (r *Reactor) dispatch(e tracker.Event, level notifier.Level, detail string) {
	if r.notifier == nil {
		return
	}
	r.notifier.Dispatch(notifier.Notification{
		Target:    e.Target,
		OldState:  e.OldState.String(),
		NewState:  e.NewState.String(),
		Level:     level,
		Detail:    detail,
		Timestamp: e.Timestamp,
	})
}
