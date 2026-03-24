//go:build !windows

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func serviceInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install as Windows service (Windows only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("service management is only supported on Windows")
		},
	}
}

func serviceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Windows service (Windows only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("service management is only supported on Windows")
		},
	}
}

func serviceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start Windows service (Windows only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("service management is only supported on Windows")
		},
	}
}

func serviceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop Windows service (Windows only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("service management is only supported on Windows")
		},
	}
}

// runAsWindowsService is a no-op on non-Windows.
func runAsWindowsService() bool {
	return false
}
