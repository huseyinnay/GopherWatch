package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/huseyinnay/gopherwatch/internal/config"
	"github.com/huseyinnay/gopherwatch/internal/docker"
	"github.com/huseyinnay/gopherwatch/internal/notifier"
	"github.com/huseyinnay/gopherwatch/internal/supervisor"
)

func main() {
	configPath := flag.String("config", "configs/gopherwatch.yaml", "config dosyası yolu")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config hatası: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(cfg.Global.LogLevel),
	}))

	workers, err := supervisor.WorkersFromConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker hatası: %v\n", err)
		os.Exit(1)
	}

	var opts []supervisor.Option
	if anyContainerConfigured(workers) {
		mgr, err := docker.New(logger)
		if err != nil {
			logger.Warn("docker client kurulamadı; otomatik restart devre dışı", "err", err)
		} else {
			defer mgr.Close()
			opts = append(opts, supervisor.WithRestarter(mgr))
			logger.Info("docker otomatik restart etkin")
		}
	}

	if d, ok := buildNotifier(cfg, logger); ok {
		defer d.Close()
		opts = append(opts, supervisor.WithNotifier(d))
		logger.Info("bildirim sistemi etkin", "kanal_sayısı", d.Count())
	}

	if cfg.HTTP.Enabled {
		opts = append(opts, supervisor.WithDashboard(cfg.HTTP.Addr))
		logger.Info("dashboard etkin", "addr", cfg.HTTP.Addr)
	}

	logger.Info("gopherwatch başlıyor", "target_sayısı", len(workers))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sup := supervisor.New(logger, workers, opts...)
	if err := sup.Run(ctx); err != nil {
		logger.Error("supervisor hatası", "err", err)
		os.Exit(1)
	}

	logger.Info("temiz çıkış")
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
