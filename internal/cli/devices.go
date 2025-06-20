// internal/cli/devices.go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/libdyson-wg/opendyson/cloud"
	"github.com/libdyson-wg/opendyson/devices"
	"github.com/libdyson-wg/opendyson/internal/config"
	"github.com/libdyson-wg/opendyson/internal/shell"
)

func DeviceGetter(getDevices func() ([]devices.Device, error)) func() ([]devices.Device, error) {
	return func() ([]devices.Device, error) {
		if Verbose {
			tok, _ := config.GetToken()
			fmt.Printf("[cloud] endpoint: %s\n", cloud.CurrentServer())
			fmt.Printf("[cloud] token: %s\n", tok)
		}

		ds, err := getDevices()
		if err != nil {
			return nil, err
		}

		wg := sync.WaitGroup{}
		for _, d := range ds {
			if cd, ok := d.(devices.ConnectedDevice); ok {
				wg.Add(1)
				go func(cd devices.ConnectedDevice) {
					if err := cd.ResolveLocalAddress(); err != nil {
						fmt.Println(err)
					}
					wg.Done()
				}(cd)
			}
		}
		wg.Wait()
		return ds, nil
	}
}

func Listener(
	getDevices func() ([]devices.Device, error),
	printLine func(in string),
) func(serial string, iot bool) error {
	return func(serial string, iot bool) error {
		ds, err := getDevices()
		if err != nil {
			return err
		}

		subscribed := make(map[string]struct{})
		cancels := make(map[string]context.CancelFunc)
		var subscribe func(id string, cd devices.ConnectedDevice, force bool) error
		subscribe = func(id string, cd devices.ConnectedDevice, force bool) error {
			if c, ok := cancels[id]; ok {
				if force {
					c()
					delete(cancels, id)
					delete(subscribed, id)
				} else {
					return nil
				}
			}
			if iot {
				cd.SetMode(devices.ModeIoT)
			}
			for name, topic := range map[string]string{
				"Status:   ": cd.StatusTopic(),
				"Fault:    ": cd.FaultTopic(),
				"Command:  ": cd.CommandTopic(),
			} {
				n, t := name, topic
				printLine(fmt.Sprintf("[%s] Subscribing to %s", id, t))
				if err := cd.SubscribeRaw(t, func(bytes []byte) {
					printLine(fmt.Sprintf("[%s] %s%s", id, n, string(bytes)))
				}); err != nil {
					return err
				}
			}

			if iot {
				ctx, cancel := context.WithCancel(context.Background())
				cancels[id] = cancel
				go func(ctx context.Context, id string, cd devices.ConnectedDevice) {
					ticker := time.NewTicker(23 * time.Hour)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							info, err := cloud.GetDeviceIoT(id)
							if err != nil {
								fmt.Println("iot refresh:", err)
								continue
							}
							if u, ok := cd.(interface{ UpdateIoT(devices.IoT) }); ok {
								u.UpdateIoT(info)
							}
							cd.SetMode(devices.ModeIoT)
							if err := subscribe(id, cd, true); err != nil {
								fmt.Println(err)
							}
						case <-ctx.Done():
							return
						}
					}
				}(ctx, id, cd)
			}
			subscribed[id] = struct{}{}
			return nil
		}

		if strings.EqualFold(serial, "ALL") {
			found := false
			for _, d := range ds {
				cd, ok := d.(devices.ConnectedDevice)
				if !ok {
					continue
				}
				found = true
				if err := subscribe(d.GetSerial(), cd, false); err != nil {
					return err
				}
			}
			if !found {
				return fmt.Errorf("no connected devices found")
			}
		} else {
			var d devices.Device
			for i := range ds {
				if ds[i].GetSerial() == serial {
					d = ds[i]
				}
			}
			if d == nil {
				return fmt.Errorf("device with serial %s not found", serial)
			}
			cd, ok := d.(devices.ConnectedDevice)
			if !ok {
				return fmt.Errorf("device %s is not connected", serial)
			}
			if err := subscribe(d.GetSerial(), cd, false); err != nil {
				return err
			}
		}

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
		shell.ListenForCtrlX(sig)
		go func() {
			<-sig
			if Verbose {
				fmt.Println("[listener] shutting down")
			}
			os.Exit(0)
		}()

		select {}
	}
}
