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

	logger.Info("gopherwatch başlıyor", "target_sayısı", len(workers))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sup := supervisor.New(logger, workers)
	if err := sup.Run(ctx); err != nil {
		logger.Error("supervisor hatası", "err", err)
		os.Exit(1)
	}

	logger.Info("temiz çıkış")
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
