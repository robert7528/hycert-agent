package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

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
		f, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Error("failed to open log file, falling back to stdout", "error", err)
			return slog.New(slog.NewTextHandler(os.Stdout, opts))
		}
		// Write to both stdout and file
		return slog.New(slog.NewTextHandler(f, opts))
	}

	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
