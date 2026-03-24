package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/config"
	"github.com/hysp/hycert-agent/internal/runner"
)

// agentProgram implements kardianos/service.Interface.
type agentProgram struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (p *agentProgram) Start(s service.Service) error {
	p.done = make(chan struct{})
	go p.run()
	return nil
}

func (p *agentProgram) run() {
	defer close(p.done)

	cfg, err := config.Load(cfgFile)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return
	}

	logger := setupLogger(cfg)
	if cfg.Server.InsecureSkipVerify {
		logger.Warn("TLS verification disabled (insecure_skip_verify=true)")
	}

	client := api.NewClient(cfg.Server.URL, cfg.Server.Token, cfg.Agent.AgentID, cfg.Server.Proxy, cfg.Server.InsecureSkipVerify)
	r := runner.New(cfg, client, logger, version)

	interval := time.Duration(cfg.Agent.Interval) * time.Second
	logger.Info("service starting",
		"agent_id", cfg.Agent.AgentID,
		"hostname", cfg.Agent.Hostname,
		"interval", interval.String(),
		"server", cfg.Server.URL,
	)

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	// Run immediately on start
	r.RunOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.RunOnce(ctx)
		case <-ctx.Done():
			logger.Info("service stopped")
			return
		}
	}
}

func (p *agentProgram) Stop(s service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
	return nil
}

// newServiceConfig returns the service configuration.
func newServiceConfig() *service.Config {
	return &service.Config{
		Name:        "hycert-agent",
		DisplayName: "HyCert Deployment Agent",
		Description: "Checks and deploys certificates to this host",
		Arguments:   []string{"daemon", "--config", cfgFile},
	}
}

func serviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage system service (install/uninstall/start/stop)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install as system service (systemd on Linux, Windows Service on Windows)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svcConfig := newServiceConfig()
			if cfgFile != "" {
				svcConfig.Arguments = []string{"daemon", "--config", cfgFile}
			}
			s, err := service.New(&agentProgram{}, svcConfig)
			if err != nil {
				return err
			}
			err = s.Install()
			if err != nil {
				return err
			}
			slog.Info("service installed",
				"name", svcConfig.Name,
				"platform", service.Platform(),
			)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := service.New(&agentProgram{}, newServiceConfig())
			if err != nil {
				return err
			}
			err = s.Uninstall()
			if err != nil {
				return err
			}
			slog.Info("service uninstalled")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := service.New(&agentProgram{}, newServiceConfig())
			if err != nil {
				return err
			}
			return s.Start()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := service.New(&agentProgram{}, newServiceConfig())
			if err != nil {
				return err
			}
			return s.Stop()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := service.New(&agentProgram{}, newServiceConfig())
			if err != nil {
				return err
			}
			status, err := s.Status()
			if err != nil {
				return err
			}
			switch status {
			case service.StatusRunning:
				slog.Info("service is running")
			case service.StatusStopped:
				slog.Info("service is stopped")
			default:
				slog.Info("service status unknown")
			}
			return nil
		},
	})

	return cmd
}
