package reactor

import (
	"context"
	"log/slog"

	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

type Reactor struct {
	logger *slog.Logger
	events <-chan tracker.Event
}

func New(logger *slog.Logger, events <-chan tracker.Event) *Reactor {
	return &Reactor{logger: logger, events: events}
}

func (r *Reactor) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-r.events:
			if !ok {
				return
			}
			r.handle(ctx, e)
		}
	}
}

func (r *Reactor) handle(ctx context.Context, e tracker.Event) {
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
