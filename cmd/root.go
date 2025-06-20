package cmd

import (
	"os"

	"github.com/libdyson-wg/opendyson/cloud"
	"github.com/libdyson-wg/opendyson/devices"
	"github.com/libdyson-wg/opendyson/internal/cli"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "opendyson",
	Short: "A community tool for connecting to and debugging Wi-Fi connected Dyson devices",

	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var verbose bool

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		cli.Verbose = verbose
		devices.Verbose = verbose
		cloud.Verbose = verbose
	}
}
