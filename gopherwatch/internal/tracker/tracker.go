package tracker

import (
	"time"

	"github.com/huseyinnay/gopherwatch/internal/prober"
)

type State int

const (
	StateUnknown State = iota
	StateHealthy
	StateUnhealthy
	StateRecovering
)

func (s State) String() string {
	switch s {
	case StateHealthy:
		return "HEALTHY"
	case StateUnhealthy:
		return "UNHEALTHY"
	case StateRecovering:
		return "RECOVERING"
	default:
		return "UNKNOWN"
	}
}

type Event struct {
	Target           string
	OldState         State
	NewState         State
	ConsecutiveFails int
	ConsecutiveOK    int
	Timestamp        time.Time
}

type targetState struct {
	state            State
	consecutiveFails int
	consecutiveOK    int
	failureThreshold int
}

type Tracker struct {
	states map[string]*targetState
	now    func() time.Time
}

func New(thresholds map[string]int, now func() time.Time) *Tracker {
	if now == nil {
		now = time.Now
	}
	t := &Tracker{
		states: make(map[string]*targetState, len(thresholds)),
		now:    now,
	}
	for name, ft := range thresholds {
		if ft <= 0 {
			ft = 1
		}
		t.states[name] = &targetState{
			state:            StateUnknown,
			failureThreshold: ft,
		}
	}
	return t
}

func (t *Tracker) Observe(r prober.Result) (Event, bool) {
	s, ok := t.states[r.Target]
	if !ok {
		return Event{}, false
	}

	oldState := s.state

	switch r.Status {
	case prober.StatusOK:
		s.consecutiveFails = 0
		s.consecutiveOK++
		switch s.state {
		case StateUnknown:
			s.state = StateHealthy
		case StateUnhealthy:
			s.state = StateRecovering
		case StateRecovering:
			s.state = StateHealthy
		}

	case prober.StatusFail:
		s.consecutiveOK = 0
		s.consecutiveFails++
		switch s.state {
		case StateUnknown, StateHealthy:
			if s.consecutiveFails >= s.failureThreshold {
				s.state = StateUnhealthy
			}
		case StateRecovering:
			s.state = StateUnhealthy
		}
	}

	if s.state == oldState {
		return Event{}, false
	}
	return Event{
		Target:           r.Target,
		OldState:         oldState,
		NewState:         s.state,
		ConsecutiveFails: s.consecutiveFails,
		ConsecutiveOK:    s.consecutiveOK,
		Timestamp:        t.now(),
	}, true
}

func (t *Tracker) State(target string) (State, bool) {
	s, ok := t.states[target]
	if !ok {
		return StateUnknown, false
	}
	return s.state, true
}
