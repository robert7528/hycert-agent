package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/config"
	"github.com/hysp/hycert-agent/internal/runner"
)

func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run continuously with periodic polling (for systemd/service)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}

			logger := setupLogger(cfg)
			if cfg.Server.InsecureSkipVerify {
				logger.Warn("TLS verification disabled (insecure_skip_verify=true)")
			}

			client := api.NewClient(cfg.Server.URL, cfg.Server.Token, cfg.Agent.AgentID, cfg.Server.Proxy, cfg.Server.InsecureSkipVerify)
			r := runner.New(cfg, client, logger, version)

			interval := time.Duration(cfg.Agent.Interval) * time.Second
			logger.Info("daemon starting",
				"agent_id", cfg.Agent.AgentID,
				"hostname", cfg.Agent.Hostname,
				"interval", interval.String(),
				"server", cfg.Server.URL,
			)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Graceful shutdown
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			// Run immediately on start (includes registration)
			r.RunOnce(ctx)

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					r.RunOnce(ctx)
				case sig := <-sigCh:
					logger.Info("received signal, shutting down", "signal", sig.String())
					cancel()
					return nil
				}
			}
		},
	}
}
