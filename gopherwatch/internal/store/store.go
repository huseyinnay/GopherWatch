package store

import (
	"sync"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/prober"
	"github.com/huseyinnay/gopherwatch/internal/tracker"
)

type Status struct {
	Name             string
	State            string // tracker.State.String(): HEALTHY/UNHEALTHY/RECOVERING/UNKNOWN
	LastChange       time.Time
	LastCheck        time.Time
	LastLatency      time.Duration
	LastError        string
	ConsecutiveFails int
	ConsecutiveOK    int
	TotalChecks      int64
	TotalFailures    int64
}

// Event, bir state geçişinin kaydı. tracker.Event'in dışarıya açılan,
// string'leştirilmiş halidir .
type Event struct {
	Target    string
	OldState  string
	NewState  string
	Timestamp time.Time
}

// Store, target durumlarını ve event geçmişini tutan thread-safe depo.
type Store struct {
	mu        sync.RWMutex
	targets   map[string]*Status
	order     []string // hedeflerin stabil sıralaması (config'deki sıra)
	events    []Event  // kronolojik ring buffer; cap = maxEvents
	maxEvents int
}

// Option, Store'u yapılandıran fonksiyonel seçenek.
type Option func(*Store)

// WithMaxEvents, event ring buffer'ının kapasitesini belirler.
// Pozitif olmayan değerler yok sayılır.
func WithMaxEvents(n int) Option {
	return func(s *Store) {
		if n > 0 {
			s.maxEvents = n
		}
	}
}

// New, verilen target adlarıyla bir Store kurar. Tüm target'lar başlangıçta
// UNKNOWN durumundadır; böylece dashboard, ilk probe atılmadan önce bile
// hedeflerin tamamını gösterebilir. Tekrar eden adlar sessizce atlanır.
func New(names []string, opts ...Option) *Store {
	s := &Store{
		targets:   make(map[string]*Status, len(names)),
		order:     make([]string, 0, len(names)),
		maxEvents: 1000,
	}
	for _, name := range names {
		if _, dup := s.targets[name]; dup {
			continue
		}
		s.targets[name] = &Status{
			Name:  name,
			State: tracker.StateUnknown.String(),
		}
		s.order = append(s.order, name)
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RecordProbe, tek bir probe sonucunu işler: canlı istatistikleri (son
// kontrol zamanı, gecikme, ardışık sayaçlar, toplamlar) günceller.
// Bilinmeyen target'lar sessizce yok sayılır.
// Not: ardışık sayaçlar burada doğrudan probe sonuçlarından türetilir;
// tracker da aynı reset mantığını uyguladığı için iki taraf doğal olarak
// senkron kalır. Böylece store, tracker'ın iç sayaçlarını dışarı açmaya
// gerek kalmadan kendi kendine yeter.
func (s *Store) RecordProbe(r prober.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.targets[r.Target]
	if !ok {
		return
	}

	st.TotalChecks++
	st.LastCheck = r.Timestamp
	st.LastLatency = r.Latency

	switch r.Status {
	case prober.StatusOK:
		st.ConsecutiveOK++
		st.ConsecutiveFails = 0
		st.LastError = ""
	case prober.StatusFail:
		st.ConsecutiveFails++
		st.ConsecutiveOK = 0
		st.TotalFailures++
		if r.Err != nil {
			st.LastError = r.Err.Error()
		}
	}
}

// RecordEvent, bir state geçişini işler: target'ın güncel durumunu ve son
// değişim zamanını günceller, geçişi ring buffer'a ekler. Bilinmeyen
// target'lar sessizce yok sayılır.
func (s *Store) RecordEvent(e tracker.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st, ok := s.targets[e.Target]; ok {
		st.State = e.NewState.String()
		st.LastChange = e.Timestamp
	}

	s.events = append(s.events, Event{
		Target:    e.Target,
		OldState:  e.OldState.String(),
		NewState:  e.NewState.String(),
		Timestamp: e.Timestamp,
	})

	if len(s.events) > s.maxEvents {
		trimmed := make([]Event, s.maxEvents)
		copy(trimmed, s.events[len(s.events)-s.maxEvents:])
		s.events = trimmed
	}
}

func (s *Store) Snapshot() []Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Status, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, *s.targets[name])
	}
	return out
}

func (s *Store) EventsSince(since time.Time) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Event, 0, len(s.events))
	for _, e := range s.events {
		if !e.Timestamp.Before(since) {
			out = append(out, e)
		}
	}
	return out
}
