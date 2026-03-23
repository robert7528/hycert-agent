package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/config"
	"github.com/hysp/hycert-agent/internal/runner"
)

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run a single check-and-deploy cycle (for cron)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}

			logger := setupLogger(cfg)
			if cfg.Server.InsecureSkipVerify {
				logger.Warn("TLS verification disabled (insecure_skip_verify=true)")
			}

			client := api.NewClient(cfg.Server.URL, cfg.Server.Token, cfg.Server.InsecureSkipVerify)
			r := runner.New(cfg, client, logger)
			r.RunOnce(context.Background())
			return nil
		},
	}
}

func setupLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	if cfg.Log.File != "" {
		// Ensure log directory exists
		if dir := filepath.Dir(cfg.Log.File); dir != "" {
			os.MkdirAll(dir, 0755)
		}

		lj := &lumberjack.Logger{
			Filename:   cfg.Log.File,
			MaxSize:    cfg.Log.MaxSize,
			MaxBackups: cfg.Log.MaxBackups,
			MaxAge:     cfg.Log.MaxAge,
			Compress:   cfg.Log.Compress,
		}

		// Write to both file and stdout
		w := io.MultiWriter(lj, os.Stdout)
		return slog.New(slog.NewTextHandler(w, opts))
	}

	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
