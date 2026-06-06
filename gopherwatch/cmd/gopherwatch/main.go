package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/huseyinnay/gopherwatch/internal/config"
	"github.com/huseyinnay/gopherwatch/internal/docker"
	"github.com/huseyinnay/gopherwatch/internal/notifier"
	"github.com/huseyinnay/gopherwatch/internal/store"
	"github.com/huseyinnay/gopherwatch/internal/supervisor"
)

var Version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		runStart(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "reload":
		runReload()
	case "version":
		fmt.Printf("GopherWatch version %s\n", Version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`GopherWatch - Uptime & Health Monitor

Usage:
  gopherwatch <command> [arguments]

Commands:
  start    Start the daemon
  status   Show current status (queries local daemon via API)
  reload   Reload configuration of the running daemon via SIGHUP
  version  Show version
`)
}

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	configPath := fs.String("config", "configs/gopherwatch.yaml", "config dosyası yolu")
	pidFile := fs.String("pidfile", "/tmp/gopherwatch.pid", "pid dosyası yolu")
	fs.Parse(args)

	if err := os.WriteFile(*pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "pid file yazılamadı: %v\n", err)
	}
	defer os.Remove(*pidFile)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)

	for {
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config yüklenemedi: %v\n", err)
			os.Exit(1)
		}

		runDaemon(ctx, sighup, cfg)

		select {
		case <-ctx.Done():
			return
		default:
			// continue loop for reload
		}
	}
}

func runDaemon(ctx context.Context, sighup chan os.Signal, cfg *config.Config) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(cfg.Global.LogLevel),
	}))

	var closers []func()
	defer func() {
		for _, c := range closers {
			c()
		}
	}()

	workers, err := supervisor.WorkersFromConfig(cfg)
	if err != nil {
		logger.Error("worker hatası", "err", err)
		os.Exit(1)
	}

	var opts []supervisor.Option
	if anyContainerConfigured(workers) {
		mgr, err := docker.New(logger)
		if err != nil {
			logger.Warn("docker client kurulamadı; otomatik restart devre dışı", "err", err)
		} else {
			closers = append(closers, func() { mgr.Close() })
			opts = append(opts, supervisor.WithRestarter(mgr))
			logger.Info("docker otomatik restart etkin")
		}
	}

	if d, ok := buildNotifier(cfg, logger); ok {
		closers = append(closers, func() { d.Close() })
		opts = append(opts, supervisor.WithNotifier(d))
		logger.Info("bildirim sistemi etkin", "kanal_sayısı", d.Count())
	}

	if cfg.HTTP.Enabled {
		opts = append(opts, supervisor.WithDashboard(cfg.HTTP.Addr, cfg.HTTP.AuthToken))
		logger.Info("dashboard etkin", "addr", cfg.HTTP.Addr)
	}

	if cfg.Global.EventLogFile != "" {
		opts = append(opts, supervisor.WithStoreOptions(store.WithEventLog(cfg.Global.EventLogFile)))
		logger.Info("persistent event log etkin", "file", cfg.Global.EventLogFile)
	}

	sup := supervisor.New(logger, workers, opts...)

	supCtx, supCancel := context.WithCancel(ctx)
	defer supCancel()

	errc := make(chan error, 1)
	go func() {
		errc <- sup.Run(supCtx)
	}()

	select {
	case <-ctx.Done():
		logger.Info("kapanma sinyali alındı...")
		supCancel()
		<-errc
	case <-sighup:
		logger.Info("SIGHUP alındı, konfigürasyon yeniden yükleniyor...")
		supCancel()
		<-errc
	case err := <-errc:
		if err != nil {
			logger.Error("supervisor hatası", "err", err)
		}
	}
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := fs.String("config", "configs/gopherwatch.yaml", "config dosyası")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config yüklenemedi: %v\n", err)
		os.Exit(1)
	}
	if !cfg.HTTP.Enabled {
		fmt.Fprintf(os.Stderr, "Status komutu için HTTP server (dashboard) aktif olmalıdır.\n")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://%s/status", cfg.HTTP.Addr)
	if cfg.HTTP.AuthToken != "" {
		url += "?token=" + cfg.HTTP.AuthToken
	}

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "API bağlantı hatası: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "HTTP hata kodu: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Response okunamadı: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(body))
}

func runReload() {
	pidFile := "/tmp/gopherwatch.pid"
	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "PID dosyası okunamadı (daemon çalışıyor mu?): %v\n", err)
		os.Exit(1)
	}
	pidStr := strings.TrimSpace(string(data))
	var pid int
	fmt.Sscanf(pidStr, "%d", &pid)

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Process bulunamadı: %v\n", err)
		os.Exit(1)
	}

	if err := proc.Signal(syscall.SIGHUP); err != nil {
		fmt.Fprintf(os.Stderr, "Sinyal gönderilemedi: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Reload sinyali başarıyla gönderildi.")
}

func anyContainerConfigured(workers []supervisor.Worker) bool {
	for _, w := range workers {
		if w.Container != "" {
			return true
		}
	}
	return false
}

func buildNotifier(cfg *config.Config, logger *slog.Logger) (*notifier.Dispatcher, bool) {
	nc := cfg.Notifications

	var notifiers []notifier.Notifier
	if d := nc.Discord; d != nil && d.Enabled {
		notifiers = append(notifiers, notifier.NewDiscordNotifier(d.WebhookURL))
	}
	if t := nc.Telegram; t != nil && t.Enabled {
		notifiers = append(notifiers, notifier.NewTelegramNotifier(t.BotToken, t.ChatID))
	}
	if s := nc.Slack; s != nil && s.Enabled {
		notifiers = append(notifiers, notifier.NewSlackNotifier(s.WebhookURL))
	}

	if len(notifiers) == 0 {
		return nil, false
	}

	var opts []notifier.Option
	if nc.RateLimit > 0 {
		opts = append(opts, notifier.WithRateLimit(nc.RateLimit.Std()))
	}
	return notifier.NewDispatcher(logger, notifiers, opts...), true
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
