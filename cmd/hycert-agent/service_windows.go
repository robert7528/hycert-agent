//go:build windows

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/config"
	"github.com/hysp/hycert-agent/internal/runner"
)

const serviceName = "hycert-agent"
const serviceDisplayName = "HyCert Deployment Agent"
const serviceDescription = "Checks and deploys certificates to this host"

// agentService implements svc.Handler for Windows Service Control Manager.
type agentService struct{}

func (s *agentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	// Load config
	configPath := cfgFile
	if configPath == "" {
		exePath, _ := os.Executable()
		configPath = filepath.Join(filepath.Dir(exePath), "agent.yaml")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return true, 1
	}

	logger := setupLogger(cfg)
	if cfg.Server.InsecureSkipVerify {
		logger.Warn("TLS verification disabled")
	}

	client := api.NewClient(cfg.Server.URL, cfg.Server.Token, cfg.Agent.AgentID, cfg.Server.Proxy, cfg.Server.InsecureSkipVerify)
	rn := runner.New(cfg, client, logger, version)

	interval := time.Duration(cfg.Agent.Interval) * time.Second
	logger.Info("service starting",
		"agent_id", cfg.Agent.AgentID,
		"hostname", cfg.Agent.Hostname,
		"interval", interval.String(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	// Run immediately
	rn.RunOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rn.RunOnce(ctx)
		case c := <-r:
			switch c.Cmd {
			case svc.Stop, svc.Shutdown:
				logger.Info("service stopping")
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				return false, 0
			case svc.Interrogate:
				changes <- c.CurrentStatus
			}
		}
	}
}

func serviceInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install as Windows service",
		RunE: func(cmd *cobra.Command, args []string) error {
			exePath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("get executable path: %w", err)
			}

			m, err := mgr.Connect()
			if err != nil {
				return fmt.Errorf("connect to service manager: %w", err)
			}
			defer m.Disconnect()

			// Check if already exists
			s, err := m.OpenService(serviceName)
			if err == nil {
				s.Close()
				return fmt.Errorf("service %s already exists (use 'service uninstall' first)", serviceName)
			}

			// Build config path argument
			configPath := cfgFile
			if configPath == "" {
				configPath = filepath.Join(filepath.Dir(exePath), "agent.yaml")
			}

			s, err = m.CreateService(serviceName, exePath, mgr.Config{
				DisplayName: serviceDisplayName,
				Description: serviceDescription,
				StartType:   mgr.StartAutomatic,
			}, "daemon", "--config", configPath)
			if err != nil {
				return fmt.Errorf("create service: %w", err)
			}
			defer s.Close()

			fmt.Printf("Service %s installed successfully\n", serviceName)
			fmt.Printf("  Binary: %s\n", exePath)
			fmt.Printf("  Config: %s\n", configPath)
			fmt.Printf("  Start:  hycert-agent service start\n")
			return nil
		},
	}
}

func serviceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Windows service",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mgr.Connect()
			if err != nil {
				return fmt.Errorf("connect to service manager: %w", err)
			}
			defer m.Disconnect()

			s, err := m.OpenService(serviceName)
			if err != nil {
				return fmt.Errorf("service %s not found", serviceName)
			}
			defer s.Close()

			err = s.Delete()
			if err != nil {
				return fmt.Errorf("delete service: %w", err)
			}

			fmt.Printf("Service %s uninstalled\n", serviceName)
			return nil
		},
	}
}

func serviceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start Windows service",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mgr.Connect()
			if err != nil {
				return fmt.Errorf("connect to service manager: %w", err)
			}
			defer m.Disconnect()

			s, err := m.OpenService(serviceName)
			if err != nil {
				return fmt.Errorf("service %s not found", serviceName)
			}
			defer s.Close()

			err = s.Start()
			if err != nil {
				return fmt.Errorf("start service: %w", err)
			}

			fmt.Printf("Service %s started\n", serviceName)
			return nil
		},
	}
}

func serviceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop Windows service",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mgr.Connect()
			if err != nil {
				return fmt.Errorf("connect to service manager: %w", err)
			}
			defer m.Disconnect()

			s, err := m.OpenService(serviceName)
			if err != nil {
				return fmt.Errorf("service %s not found", serviceName)
			}
			defer s.Close()

			_, err = s.Control(svc.Stop)
			if err != nil {
				return fmt.Errorf("stop service: %w", err)
			}

			fmt.Printf("Service %s stopped\n", serviceName)
			return nil
		},
	}
}

// runAsWindowsService checks if running as a Windows service and handles it.
func runAsWindowsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false
	}

	// Running as Windows service — start the service handler
	err = svc.Run(serviceName, &agentService{})
	if err != nil {
		slog.Error("service failed", "error", err)
		os.Exit(1)
	}
	os.Exit(0)
	return true
}
