package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/libdyson-wg/opendyson/cloud"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/libdyson-wg/opendyson/devices"
)

func Repeater(
	getDevices func() ([]devices.Device, error),
) func(serial string, iot bool, host, user, password string) error {
	return func(serial string, iot bool, host, user, password string) error {
		opts := paho.NewClientOptions()
		if strings.Contains(host, "://") {
			opts.AddBroker(host)
		} else {
			opts.AddBroker(fmt.Sprintf("tcp://%s:1883", host))
		}
		opts.SetClientID("opendyson-repeater")
		if user != "" {
			opts.SetUsername(user)
			opts.SetPassword(password)
		}
		client := paho.NewClient(opts)
		t := client.Connect()
		if !t.WaitTimeout(5 * time.Second) {
			return fmt.Errorf("mqtt connect %s timeout", host)
		}
		if t.Error() != nil {
			return fmt.Errorf("unable to connect: %w", t.Error())
		}

		ds, err := getDevices()
		if err != nil {
			return err
		}

		subscribed := make(map[string]struct{})
		commandTargets := make(map[string]devices.ConnectedDevice)
		mu := sync.RWMutex{}

		var subscribe func(id string, cd devices.ConnectedDevice, force bool) error
		subscribe = func(id string, cd devices.ConnectedDevice, force bool) error {
			if _, ok := subscribed[id]; ok && !force {
				return nil
			}
			if iot {
				cd.SetMode(devices.ModeIoT)
			}
			for _, topic := range []string{cd.StatusTopic(), cd.FaultTopic(), cd.CommandTopic()} {
				t := topic
				if err := cd.SubscribeRaw(t, func(b []byte) {
					fmt.Printf("Incoming message %s on topic %s\n", string(b), t)
					client.Publish(t, 0, false, b)
				}); err != nil {
					return err
				}
			}

			mu.Lock()
			commandTargets[cd.CommandTopic()] = cd
			mu.Unlock()

			if token := client.Subscribe(cd.CommandTopic(), 0, func(c paho.Client, msg paho.Message) {
				fmt.Printf("Forwarding %s from host to %s\n", string(msg.Payload()), cd.CommandTopic())
				if err := cd.SendRaw(cd.CommandTopic(), msg.Payload()); err != nil {
					fmt.Println(err)
				}
			}); !token.WaitTimeout(5 * time.Second) {
				return fmt.Errorf("subscribe timeout")
			} else if token.Error() != nil {
				return token.Error()
			}

			if iot {
				go func(id string, cd devices.ConnectedDevice) {
					ticker := time.NewTicker(30 * time.Second)
					refresh := time.NewTicker(23 * time.Hour)
					defer ticker.Stop()
					defer refresh.Stop()
					for {
						select {
						case <-ticker.C:
							ts := time.Now().UTC().Format(time.RFC3339)
							msgs := []string{
								fmt.Sprintf(`{"mode-reason":"RAPP","time":"%s","msg":"REQUEST-CURRENT-FAULTS"}`, ts),
								fmt.Sprintf(`{"mode-reason":"RAPP","time":"%s","msg":"REQUEST-CURRENT-STATE"}`, ts),
							}
							for _, m := range msgs {
								fmt.Printf("Sending %s to %s\n", m, cd.CommandTopic())
								_ = cd.SendRaw(cd.CommandTopic(), []byte(m))
							}
						case <-refresh.C:
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
						}
					}
				}(id, cd)
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
			for _, dev := range ds {
				if dev.GetSerial() == serial {
					d = dev
					break
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

		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				if !strings.EqualFold(serial, "ALL") {
					continue
				}
				nds, err := getDevices()
				if err != nil {
					fmt.Println("device refresh:", err)
					continue
				}
				for _, d := range nds {
					cd, ok := d.(devices.ConnectedDevice)
					if !ok {
						continue
					}
					if err := subscribe(d.GetSerial(), cd, false); err != nil {
						fmt.Println(err)
					}
				}
			}
		}()

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
		go func() {
			<-sig
			client.Disconnect(250)
			os.Exit(0)
		}()

		select {}
	}
}
