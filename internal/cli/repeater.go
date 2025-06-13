package cli

import (
	"fmt"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/libdyson-wg/opendyson/devices"
)

func Repeater(getDevices func() ([]devices.Device, error)) func(serial string, iot bool, ip, user, pw string) error {
	return func(serial string, iot bool, ip, user, pw string) error {
		opts := paho.NewClientOptions()
		opts.AddBroker(fmt.Sprintf("tcp://%s:1883", ip))
		opts.SetClientID("opendyson-repeater")
		if user != "" {
			opts.SetUsername(user)
			opts.SetPassword(pw)
		}
		client := paho.NewClient(opts)
		t := client.Connect()
		if !t.WaitTimeout(time.Second * 5) {
			return fmt.Errorf("mqtt connect timeout")
		}
		if t.Error() != nil {
			return fmt.Errorf("unable to connect: %w", t.Error())
		}

		subscribed := make(map[string]struct{})
		mu := sync.Mutex{}

		subscribe := func(cd devices.ConnectedDevice) error {
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
			if iot {
				go func() {
					ticker := time.NewTicker(30 * time.Second)
					defer ticker.Stop()
					for range ticker.C {
						ts := time.Now().UTC().Format(time.RFC3339)
						msgs := []string{
							fmt.Sprintf(`{"mode-reason":"RAPP","time":"%s","msg":"REQUEST-CURRENT-FAULTS"}`, ts),
							fmt.Sprintf(`{"mode-reason":"RAPP","time":"%s","msg":"REQUEST-CURRENT-STATE"}`, ts),
						}
						for _, m := range msgs {
							fmt.Printf("Sending %s to %s\n", m, cd.CommandTopic())
							_ = cd.SendRaw(cd.CommandTopic(), []byte(m))
						}
					}
				}()
			}
			return nil
		}

		updateSubs := func() error {
			ds, err := getDevices()
			if err != nil {
				return err
			}
			for _, d := range ds {
				if serial != "ALL" && !strings.EqualFold(serial, d.GetSerial()) {
					continue
				}
				cd, ok := d.(devices.ConnectedDevice)
				if !ok {
					continue
				}
				mu.Lock()
				if _, ok := subscribed[d.GetSerial()]; !ok {
					if err := subscribe(cd); err != nil {
						mu.Unlock()
						return err
					}
					subscribed[d.GetSerial()] = struct{}{}
				}
				mu.Unlock()
			}
			return nil
		}

		if err := updateSubs(); err != nil {
			return err
		}

		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				if err := updateSubs(); err != nil {
					fmt.Println(err)
				}
			}
		}()

		select {}
	}
}
