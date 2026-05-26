package tracker

import (
	"testing"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/prober"
)

func okResult(target string) prober.Result {
	return prober.Result{Target: target, Status: prober.StatusOK}
}
func failResult(target string) prober.Result {
	return prober.Result{Target: target, Status: prober.StatusFail}
}

func TestTracker_InitialStateIsUnknown(t *testing.T) {
	tr := New(map[string]int{"api": 3}, nil)
	s, ok := tr.State("api")
	if !ok || s != StateUnknown {
		t.Fatalf("state=%v ok=%v, Unknown true bekleniyordu", s, ok)
	}
}

func TestTracker_FirstOK_UnknownToHealthy(t *testing.T) {
	tr := New(map[string]int{"api": 3}, nil)
	e, changed := tr.Observe(okResult("api"))
	if !changed {
		t.Fatal("ilk OK için event üretilmeliydi")
	}
	if e.OldState != StateUnknown || e.NewState != StateHealthy {
		t.Errorf("geçiş=%v→%v, Unknown→Healthy bekleniyordu", e.OldState, e.NewState)
	}
}

func TestTracker_HealthyStaysHealthyOnOK(t *testing.T) {
	tr := New(map[string]int{"api": 3}, nil)
	tr.Observe(okResult("api")) // Healthy
	_, changed := tr.Observe(okResult("api"))
	if changed {
		t.Error("Healthy → Healthy için event üretilmemeliydi")
	}
}

func TestTracker_BelowThresholdStaysHealthy(t *testing.T) {
	tr := New(map[string]int{"api": 3}, nil)
	tr.Observe(okResult("api"))

	for i := 0; i < 2; i++ {
		_, changed := tr.Observe(failResult("api"))
		if changed {
			t.Errorf("fail #%d için event üretilmemeliydi", i+1)
		}
	}
	if s, _ := tr.State("api"); s != StateHealthy {
		t.Errorf("state=%v, hala Healthy olmalıydı", s)
	}
}

func TestTracker_ThresholdReachedHealthyToUnhealthy(t *testing.T) {
	tr := New(map[string]int{"api": 3}, nil)
	tr.Observe(okResult("api")) // Healthy

	var lastEvent Event
	var lastChanged bool
	for i := 0; i < 3; i++ {
		lastEvent, lastChanged = tr.Observe(failResult("api"))
	}
	if !lastChanged {
		t.Fatal("3. fail'de event üretilmeliydi")
	}
	if lastEvent.OldState != StateHealthy || lastEvent.NewState != StateUnhealthy {
		t.Errorf("geçiş=%v→%v, Healthy→Unhealthy bekleniyordu",
			lastEvent.OldState, lastEvent.NewState)
	}
	if lastEvent.ConsecutiveFails != 3 {
		t.Errorf("ConsecutiveFails=%d, 3 bekleniyordu", lastEvent.ConsecutiveFails)
	}
}

func TestTracker_UnknownToUnhealthyDirectly(t *testing.T) {
	tr := New(map[string]int{"api": 2}, nil)
	tr.Observe(failResult("api"))
	e, changed := tr.Observe(failResult("api"))
	if !changed || e.OldState != StateUnknown || e.NewState != StateUnhealthy {
		t.Errorf("geçiş=%v→%v changed=%v, Unknown→Unhealthy bekleniyordu",
			e.OldState, e.NewState, changed)
	}
}

func TestTracker_UnhealthyStaysUnhealthyOnFail(t *testing.T) {
	tr := New(map[string]int{"api": 2}, nil)
	tr.Observe(okResult("api"))
	tr.Observe(failResult("api"))
	tr.Observe(failResult("api")) // Unhealthy

	_, changed := tr.Observe(failResult("api"))
	if changed {
		t.Error("Unhealthy → Unhealthy için event üretilmemeliydi")
	}
}

func TestTracker_RecoveryHappyPath(t *testing.T) {
	tr := New(map[string]int{"api": 2}, nil)
	tr.Observe(okResult("api"))
	tr.Observe(failResult("api"))
	tr.Observe(failResult("api")) // Unhealthy

	e1, c1 := tr.Observe(okResult("api"))
	if !c1 || e1.NewState != StateRecovering {
		t.Errorf("1. geçiş %v→%v, Unhealthy→Recovering bekleniyordu",
			e1.OldState, e1.NewState)
	}

	e2, c2 := tr.Observe(okResult("api"))
	if !c2 || e2.NewState != StateHealthy {
		t.Errorf("2. geçiş %v→%v, Recovering→Healthy bekleniyordu",
			e2.OldState, e2.NewState)
	}
}

func TestTracker_RecoveringFailGoesUnhealthyImmediately(t *testing.T) {
	tr := New(map[string]int{"api": 2}, nil)
	tr.Observe(okResult("api"))
	tr.Observe(failResult("api"))
	tr.Observe(failResult("api")) // Unhealthy
	tr.Observe(okResult("api"))   // Recovering

	e, changed := tr.Observe(failResult("api"))
	if !changed || e.OldState != StateRecovering || e.NewState != StateUnhealthy {
		t.Errorf("geçiş=%v→%v changed=%v, Recovering→Unhealthy bekleniyordu",
			e.OldState, e.NewState, changed)
	}
	if e.ConsecutiveFails != 1 {
		t.Errorf("ConsecutiveFails=%d, 1 bekleniyordu (threshold beklenmedi)",
			e.ConsecutiveFails)
	}
}

func TestTracker_TimestampFromInjectedClock(t *testing.T) {
	fixed := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := New(map[string]int{"api": 3}, func() time.Time { return fixed })

	e, _ := tr.Observe(okResult("api"))
	if !e.Timestamp.Equal(fixed) {
		t.Errorf("Timestamp=%v, %v bekleniyordu", e.Timestamp, fixed)
	}
}

func TestTracker_UnknownTargetIgnored(t *testing.T) {
	tr := New(map[string]int{"api": 3}, nil)
	_, changed := tr.Observe(okResult("ghost"))
	if changed {
		t.Error("kayıtsız target için event üretilmemeliydi")
	}
}

func TestTracker_TwoTargetsIndependent(t *testing.T) {
	tr := New(map[string]int{"a": 2, "b": 2}, nil)
	tr.Observe(okResult("a")) // a: Healthy
	tr.Observe(failResult("a"))
	tr.Observe(failResult("a")) // a: Unhealthy

	sa, _ := tr.State("a")
	sb, _ := tr.State("b")
	if sa != StateUnhealthy {
		t.Errorf("a state=%v, Unhealthy bekleniyordu", sa)
	}
	if sb != StateUnknown {
		t.Errorf("b state=%v (a'dan etkilenmemeli), Unknown bekleniyordu", sb)
	}
}
