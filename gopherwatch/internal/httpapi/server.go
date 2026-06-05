package httpapi

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/huseyinnay/gopherwatch/internal/store"
)

//go:embed dashboard.html
var dashboardHTML []byte

// Source, sunucunun durum ve event verisini aldığı kaynak. *store.Store bunu karşılar.
type Source interface {
	Snapshot() []store.Status
	EventsSince(t time.Time) []store.Event
}

// Server, kontrol API'sini ve dashboard'u sunan HTTP sunucusu.
type Server struct {
	src             Source
	logger          *slog.Logger
	addr            string
	now             func() time.Time
	shutdownTimeout time.Duration
	defaultWindow   time.Duration // /events için "since" verilmezse kullanılan pencere
	authToken       string
	srv             *http.Server
}

// Option, Server'ı yapılandıran fonksiyonel seçenek.
type Option func(*Server)

// WithAddr, sunucunun dinleyeceği adresi belirler (ör. "localhost:8090").
func WithAddr(addr string) Option {
	return func(s *Server) {
		if addr != "" {
			s.addr = addr
		}
	}
}

// WithClock, zaman kaynağını enjekte eder .
func WithClock(now func() time.Time) Option {
	return func(s *Server) {
		if now != nil {
			s.now = now
		}
	}
}

// WithShutdownTimeout, shutdown için tanınan süreyi belirler.
func WithShutdownTimeout(d time.Duration) Option {
	return func(s *Server) {
		if d > 0 {
			s.shutdownTimeout = d
		}
	}
}

// WithAuthToken, API ve dashboard için token tabanlı doğrulamayı etkinleştirir.
func WithAuthToken(token string) Option {
	return func(s *Server) {
		s.authToken = token
	}
}

// New, verilen kaynak ve logger ile bir Server kurar.
func New(src Source, logger *slog.Logger, opts ...Option) *Server {
	s := &Server{
		src:             src,
		logger:          logger,
		addr:            "localhost:8090",
		now:             time.Now,
		shutdownTimeout: 5 * time.Second,
		defaultWindow:   15 * time.Minute,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.srv = &http.Server{
		Handler: s.routes(),
		// Slowloris türü saldırılara karşı basit bir koruma.
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Run, sunucuyu başlatır ve context iptal edilene kadar çalıştırır.
// Port bağlanamazsa (ör. adres meşgul) hata SENKRON döner — net.Listen'i
// goroutine'den önce çağırdığımız için. Böylece çağıran taraf başlatma
// hatasını hemen görür.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("dashboard portu dinlenemedi (%s): %w", s.addr, err)
	}
	s.logger.Info("dashboard sunucusu dinliyor", "addr", ln.Addr().String())

	// Tamponlu kanal: Shutdown sonrası Serve, ErrServerClosed'ı buraya yazar;
	// kimse okumasa bile goroutine sızmaz.
	errc := make(chan error, 1)
	go func() {
		errc <- s.srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("dashboard graceful shutdown: %w", err)
		}
		s.logger.Info("dashboard sunucusu temiz kapandı")
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("dashboard sunucusu: %w", err)
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/healthz", s.handleHealthz)

	if s.authToken != "" {
		return s.authMiddleware(mux)
	}
	return mux
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if token != s.authToken {
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Unauthorized. Lütfen ?token=... ile erişin.\n"))
				return
			}
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// "/" her şeyi yakalar; kök dışındaki yollara 404 döner.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}

type statusResponse struct {
	GeneratedAt string      `json:"generated_at"`
	Targets     []statusDTO `json:"targets"`
}

type statusDTO struct {
	Name             string  `json:"name"`
	State            string  `json:"state"`
	LastChange       string  `json:"last_change,omitempty"`
	LastCheck        string  `json:"last_check,omitempty"`
	LatencyMS        float64 `json:"latency_ms"`
	Error            string  `json:"error,omitempty"`
	ConsecutiveFails int     `json:"consecutive_fails"`
	ConsecutiveOK    int     `json:"consecutive_ok"`
	TotalChecks      int64   `json:"total_checks"`
	TotalFailures    int64   `json:"total_failures"`
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	snap := s.src.Snapshot()
	targets := make([]statusDTO, 0, len(snap))
	for _, st := range snap {
		targets = append(targets, statusDTO{
			Name:             st.Name,
			State:            st.State,
			LastChange:       formatTime(st.LastChange),
			LastCheck:        formatTime(st.LastCheck),
			LatencyMS:        float64(st.LastLatency) / float64(time.Millisecond),
			Error:            st.LastError,
			ConsecutiveFails: st.ConsecutiveFails,
			ConsecutiveOK:    st.ConsecutiveOK,
			TotalChecks:      st.TotalChecks,
			TotalFailures:    st.TotalFailures,
		})
	}
	writeJSON(w, statusResponse{
		GeneratedAt: formatTime(s.now()),
		Targets:     targets,
	})
}

type eventsResponse struct {
	Window      string     `json:"window"`
	GeneratedAt string     `json:"generated_at"`
	Events      []eventDTO `json:"events"`
}

type eventDTO struct {
	Target    string `json:"target"`
	OldState  string `json:"old_state"`
	NewState  string `json:"new_state"`
	Timestamp string `json:"timestamp"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	window := s.defaultWindow
	if q := r.URL.Query().Get("since"); q != "" {
		d, err := time.ParseDuration(q)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("geçersiz 'since' parametresi: %q (ör. 5m, 1h)", q))
			return
		}
		window = d
	}

	cutoff := s.now().Add(-window)
	raw := s.src.EventsSince(cutoff)

	events := make([]eventDTO, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		e := raw[i]
		events = append(events, eventDTO{
			Target:    e.Target,
			OldState:  e.OldState,
			NewState:  e.NewState,
			Timestamp: formatTime(e.Timestamp),
		})
	}

	writeJSON(w, eventsResponse{
		Window:      window.String(),
		GeneratedAt: formatTime(s.now()),
		Events:      events,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	snap := s.src.Snapshot()
	var b strings.Builder

	writeMetricHeader(&b, "gopherwatch_target_up", "Hedef sağlıklı mı (1) değil mi (0).", "gauge")
	for _, st := range snap {
		fmt.Fprintf(&b, "gopherwatch_target_up{target=\"%s\"} %d\n", escapeLabel(st.Name), boolToInt(st.State == "HEALTHY"))
	}

	writeMetricHeader(&b, "gopherwatch_target_checks_total", "Toplam probe sayısı.", "counter")
	for _, st := range snap {
		fmt.Fprintf(&b, "gopherwatch_target_checks_total{target=\"%s\"} %d\n", escapeLabel(st.Name), st.TotalChecks)
	}

	writeMetricHeader(&b, "gopherwatch_target_failures_total", "Toplam başarısız probe sayısı.", "counter")
	for _, st := range snap {
		fmt.Fprintf(&b, "gopherwatch_target_failures_total{target=\"%s\"} %d\n", escapeLabel(st.Name), st.TotalFailures)
	}

	writeMetricHeader(&b, "gopherwatch_target_consecutive_fails", "Ardışık başarısız probe sayısı.", "gauge")
	for _, st := range snap {
		fmt.Fprintf(&b, "gopherwatch_target_consecutive_fails{target=\"%s\"} %d\n", escapeLabel(st.Name), st.ConsecutiveFails)
	}

	writeMetricHeader(&b, "gopherwatch_target_last_latency_seconds", "Son probe gecikmesi (saniye).", "gauge")
	for _, st := range snap {
		fmt.Fprintf(&b, "gopherwatch_target_last_latency_seconds{target=\"%s\"} %g\n", escapeLabel(st.Name), st.LastLatency.Seconds())
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// formatTime, sıfır zamanı boş string'e çevirir; aksi halde RFC3339 verir.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func writeMetricHeader(b *strings.Builder, name, help, typ string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s %s\n", name, typ)
}

func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
