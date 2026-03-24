package main

import (
	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run continuously with periodic polling (for systemd/service or console)",
		RunE: func(cmd *cobra.Command, args []string) error {
			prg := &agentProgram{}
			svcConfig := newServiceConfig()

			s, err := service.New(prg, svcConfig)
			if err != nil {
				return err
			}

			// Run as service if launched by SCM, otherwise run interactively
			return s.Run()
		},
	}
}
