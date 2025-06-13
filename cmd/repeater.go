package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var repeaterCmd = &cobra.Command{
	Use:   "repeater serial|ALL",
	Short: "Repeat device messages to another MQTT broker",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return fmt.Errorf("must specify serial")
		}
		iot, _ := cmd.Flags().GetBool("iot")
		ip, _ := cmd.Flags().GetString("ip")
		user, _ := cmd.Flags().GetString("user")
		pw, _ := cmd.Flags().GetString("pw")
		if ip == "" {
			return fmt.Errorf("must specify --ip")
		}
		return funcs.MQTTRepeater(args[0], iot, ip, user, pw)
	},
}

func init() {
	rootCmd.AddCommand(repeaterCmd)
	repeaterCmd.Flags().BoolP("iot", "", false, "connect through AWS IoT instead of local MQTT")
	repeaterCmd.Flags().String("ip", "", "address of MQTT broker")
	repeaterCmd.Flags().String("user", "", "username for MQTT broker")
	repeaterCmd.Flags().String("pw", "", "password for MQTT broker")
}
