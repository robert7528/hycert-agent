package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
)

var cfgFile string

func main() {
	// If running as Windows service, handle it directly
	if runAsWindowsService() {
		return
	}

	root := &cobra.Command{
		Use:   "hycert-agent",
		Short: "HyCert deployment agent — checks and deploys certificates to target hosts",
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: /etc/hycert/agent.yaml)")

	root.AddCommand(
		runCmd(),
		daemonCmd(),
		serviceCmd(),
		versionCmd(),
	)

	if err := root.Execute(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
