package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/huseyinnay/gopherwatch/internal/config"
)

func main() {
	configPath := flag.String("config", "configs/gopherwatch.yaml", "config dosyası yolu")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config hatası: %v\n", err)
		os.Exit(1)
	}

	logger.Info("config yüklendi",
		"target_sayısı", len(cfg.Targets),
		"log_level", cfg.Global.LogLevel,
	)

	for _, t := range cfg.Targets {
		logger.Info("target",
			"name", t.Name,
			"type", t.Type,
			"interval", t.CheckInterval.Std(),
			"container", t.Container,
		)
	}
}
