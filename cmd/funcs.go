package cmd

import (
	"fmt"
	"github.com/libdyson-wg/opendyson/cloud"
	"github.com/libdyson-wg/opendyson/devices"
	"github.com/libdyson-wg/opendyson/internal/cli"
	"github.com/libdyson-wg/opendyson/internal/config"
	"github.com/libdyson-wg/opendyson/internal/shell"
)

type functions struct {
	Login        func() error
	MQTTListen   func(serial string, iot bool) error
	MQTTHost     func(serial string, iot bool, refresh int) error
	MQTTRepeater func(serial string, iot bool, host, user, password string, refresh int) error
	GetDevices   func() ([]devices.Device, error)
}

var funcs functions

func init() {
	funcs = functions{
		Login: cli.Login(
			shell.PromptForInput,
			shell.PromptForPassword,
			cloud.BeginLogin,
			cloud.CompleteLogin,
			config.SetToken,
			cloud.SetToken,
			cloud.SetServerRegion,
		),
		GetDevices: cli.DeviceGetter(cloud.GetDevices),
		MQTTListen: cli.Listener(
			cloud.GetDevices,
			func(in string) {
				_, _ = fmt.Println(in)
			}),
		MQTTHost: cli.Host(
			cloud.GetDevices,
		),
		MQTTRepeater: cli.Repeater(
			cloud.GetDevices,
		),
	}
}
