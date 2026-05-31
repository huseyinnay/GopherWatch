package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/prober"
	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

func ok(target string, latency time.Duration, ts time.Time) prober.Result {
	return prober.Result{Target: target, Status: prober.StatusOK, Latency: latency, Timestamp: ts}
}

func fail(target string, err error, ts time.Time) prober.Result {
	return prober.Result{Target: target, Status: prober.StatusFail, Err: err, Timestamp: ts}
}

func find(snap []Status, name string) (Status, bool) {
	for _, s := range snap {
		if s.Name == name {
			return s, true
		}
	}
	return Status{}, false
}

func evt(target string, from, to tracker.State, ts time.Time) tracker.Event {
	return tracker.Event{Target: target, OldState: from, NewState: to, Timestamp: ts}
}

func TestNew_InitialStateAndOrder(t *testing.T) {
	s := New([]string{"api", "db", "cache"})
	snap := s.Snapshot()

	if len(snap) != 3 {
		t.Fatalf("snapshot uzunluğu=%d, 3 bekleniyordu", len(snap))
	}
	wantOrder := []string{"api", "db", "cache"}
	for i, want := range wantOrder {
		if snap[i].Name != want {
			t.Errorf("sıra[%d]=%q, %q bekleniyordu", i, snap[i].Name, want)
		}
		if snap[i].State != "UNKNOWN" {
			t.Errorf("%q başlangıç state=%q, UNKNOWN bekleniyordu", snap[i].Name, snap[i].State)
		}
	}
}

func TestNew_DuplicateNamesIgnored(t *testing.T) {
	s := New([]string{"api", "api", "db"})
	if got := len(s.Snapshot()); got != 2 {
		t.Errorf("tekrar eden ad sonrası hedef sayısı=%d, 2 bekleniyordu", got)
	}
}

func TestRecordProbe_OKUpdatesStats(t *testing.T) {
	s := New([]string{"api"})
	now := time.Now()
	s.RecordProbe(ok("api", 12*time.Millisecond, now))

	st, _ := find(s.Snapshot(), "api")
	if st.TotalChecks != 1 {
		t.Errorf("TotalChecks=%d, 1 bekleniyordu", st.TotalChecks)
	}
	if st.TotalFailures != 0 {
		t.Errorf("TotalFailures=%d, 0 bekleniyordu", st.TotalFailures)
	}
	if st.ConsecutiveOK != 1 {
		t.Errorf("ConsecutiveOK=%d, 1 bekleniyordu", st.ConsecutiveOK)
	}
	if st.LastLatency != 12*time.Millisecond {
		t.Errorf("LastLatency=%v, 12ms bekleniyordu", st.LastLatency)
	}
	if !st.LastCheck.Equal(now) {
		t.Errorf("LastCheck=%v, %v bekleniyordu", st.LastCheck, now)
	}
}

func TestRecordProbe_FailSetsErrorAndCounters(t *testing.T) {
	s := New([]string{"api"})
	s.RecordProbe(fail("api", errors.New("connection refused"), time.Now()))

	st, _ := find(s.Snapshot(), "api")
	if st.TotalFailures != 1 {
		t.Errorf("TotalFailures=%d, 1 bekleniyordu", st.TotalFailures)
	}
	if st.ConsecutiveFails != 1 {
		t.Errorf("ConsecutiveFails=%d, 1 bekleniyordu", st.ConsecutiveFails)
	}
	if st.LastError != "connection refused" {
		t.Errorf("LastError=%q, 'connection refused' bekleniyordu", st.LastError)
	}
}

func TestRecordProbe_OKClearsPreviousFailure(t *testing.T) {
	s := New([]string{"api"})
	s.RecordProbe(fail("api", errors.New("timeout"), time.Now()))
	s.RecordProbe(fail("api", errors.New("timeout"), time.Now()))
	s.RecordProbe(ok("api", 5*time.Millisecond, time.Now()))

	st, _ := find(s.Snapshot(), "api")
	if st.ConsecutiveFails != 0 {
		t.Errorf("OK sonrası ConsecutiveFails=%d, 0 bekleniyordu", st.ConsecutiveFails)
	}
	if st.ConsecutiveOK != 1 {
		t.Errorf("ConsecutiveOK=%d, 1 bekleniyordu", st.ConsecutiveOK)
	}
	if st.LastError != "" {
		t.Errorf("OK sonrası LastError=%q, boş bekleniyordu", st.LastError)
	}
	if st.TotalChecks != 3 {
		t.Errorf("TotalChecks=%d, 3 bekleniyordu", st.TotalChecks)
	}
	if st.TotalFailures != 2 {
		t.Errorf("TotalFailures=%d, 2 bekleniyordu", st.TotalFailures)
	}
}

func TestRecordProbe_UnknownTargetIgnored(t *testing.T) {
	s := New([]string{"api"})
	s.RecordProbe(ok("ghost", time.Second, time.Now())) // panik olmamalı
	if got := len(s.Snapshot()); got != 1 {
		t.Errorf("bilinmeyen target eklenmiş; hedef sayısı=%d, 1 bekleniyordu", got)
	}
}

func TestRecordEvent_UpdatesStateAndHistory(t *testing.T) {
	s := New([]string{"api"})
	now := time.Now()
	s.RecordEvent(evt("api", tracker.StateUnknown, tracker.StateHealthy, now))

	st, _ := find(s.Snapshot(), "api")
	if st.State != "HEALTHY" {
		t.Errorf("state=%q, HEALTHY bekleniyordu", st.State)
	}
	if !st.LastChange.Equal(now) {
		t.Errorf("LastChange=%v, %v bekleniyordu", st.LastChange, now)
	}

	evts := s.EventsSince(now.Add(-time.Minute))
	if len(evts) != 1 {
		t.Fatalf("event sayısı=%d, 1 bekleniyordu", len(evts))
	}
	if evts[0].OldState != "UNKNOWN" || evts[0].NewState != "HEALTHY" {
		t.Errorf("event geçişi=%s->%s, UNKNOWN->HEALTHY bekleniyordu", evts[0].OldState, evts[0].NewState)
	}
}

func TestEventsSince_FiltersByTime(t *testing.T) {
	s := New([]string{"api"})
	base := time.Now()
	s.RecordEvent(evt("api", tracker.StateUnknown, tracker.StateHealthy, base.Add(-10*time.Minute)))
	s.RecordEvent(evt("api", tracker.StateHealthy, tracker.StateUnhealthy, base.Add(-2*time.Minute)))
	s.RecordEvent(evt("api", tracker.StateUnhealthy, tracker.StateRecovering, base.Add(-30*time.Second)))

	got := s.EventsSince(base.Add(-5 * time.Minute))
	if len(got) != 2 {
		t.Fatalf("5dk penceresinde event=%d, 2 bekleniyordu", len(got))
	}

	if !got[0].Timestamp.Before(got[1].Timestamp) {
		t.Errorf("EventsSince kronolojik sırada değil")
	}
}

func TestRecordEvent_RingBufferCap(t *testing.T) {
	s := New([]string{"api"}, WithMaxEvents(3))
	base := time.Now()
	for i := 0; i < 10; i++ {
		s.RecordEvent(evt("api", tracker.StateHealthy, tracker.StateUnhealthy, base.Add(time.Duration(i)*time.Second)))
	}

	got := s.EventsSince(base.Add(-time.Hour))
	if len(got) != 3 {
		t.Fatalf("ring buffer boyutu=%d, 3 (cap) bekleniyordu", len(got))
	}

	if !got[0].Timestamp.Equal(base.Add(7 * time.Second)) {
		t.Errorf("en eski kalan event yanlış: %v", got[0].Timestamp)
	}
}

func TestSnapshot_ReturnsCopies(t *testing.T) {
	s := New([]string{"api"})
	s.RecordProbe(ok("api", time.Millisecond, time.Now()))

	snap := s.Snapshot()
	snap[0].State = "TAMPERED"
	snap[0].TotalChecks = 9999

	fresh, _ := find(s.Snapshot(), "api")
	if fresh.State == "TAMPERED" || fresh.TotalChecks == 9999 {
		t.Error("Snapshot iç state'e referans döndürüyor — kopya olmalı")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := New([]string{"a", "b", "c"})
	var wg sync.WaitGroup
	stop := time.Now().Add(150 * time.Millisecond)

	for _, name := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			for time.Now().Before(stop) {
				s.RecordProbe(ok(n, time.Millisecond, time.Now()))
				s.RecordEvent(evt(n, tracker.StateHealthy, tracker.StateUnhealthy, time.Now()))
			}
		}(name)
	}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				_ = s.Snapshot()
				_ = s.EventsSince(time.Now().Add(-time.Minute))
			}
		}()
	}
	wg.Wait()
}
