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

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/libdyson-wg/opendyson/devices"
	"github.com/libdyson-wg/opendyson/internal/shell"
)

func Repeater(
	getDevices func() ([]devices.Device, error),
) func(serial string, iot bool, host, user, password string, refresh int) error {
	return func(serial string, iot bool, host, user, password string, refresh int) error {
		opts := paho.NewClientOptions()
		opts.SetResumeSubs(true)
		if Verbose {
			fmt.Printf("[repeater] connecting to %s\n", host)
			if user != "" {
				fmt.Printf("[repeater] username: %s password: %s\n", user, password)
			}
		}
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
		if Verbose {
			fmt.Println("[repeater] connected")
		}

		ds, err := getDevices()
		if err != nil {
			return err
		}

		subscribed := make(map[string]struct{})
		commandTargets := make(map[string]devices.ConnectedDevice)
		mu := sync.RWMutex{}
		dedup := make(map[string]map[string]struct{})
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
			for _, topic := range []string{cd.StatusTopic(), cd.FaultTopic()} {
				t := topic
				if err := cd.SubscribeRaw(t, func(b []byte) {
					fmt.Printf("Incoming message %s on topic %s\n", string(b), t)
					client.Publish(t, 0, false, b)
				}); err != nil {
					return err
				}
			}

			if err := cd.SubscribeRaw(cd.CommandTopic(), func(b []byte) {
				key := dedupKey(b)
				mu.Lock()
				if m, ok := dedup[cd.CommandTopic()]; ok {
					if _, seen := m[key]; seen {
						delete(m, key)
						if len(m) == 0 {
							delete(dedup, cd.CommandTopic())
						}
						mu.Unlock()
						return
					}
				}
				mu.Unlock()
				fmt.Printf("Incoming message %s on topic %s\n", string(b), cd.CommandTopic())
				// Do not publish command messages back to MQTT server
			}); err != nil {
				return err
			}

			mu.Lock()
			commandTargets[cd.CommandTopic()] = cd
			mu.Unlock()

			if token := client.Subscribe(cd.CommandTopic(), 0, func(c paho.Client, msg paho.Message) {
				payload := msg.Payload()
				fmt.Printf("Forwarding %s from host to %s\n", string(payload), cd.CommandTopic())
				key := dedupKey(payload)
				mu.Lock()
				if dedup[cd.CommandTopic()] == nil {
					dedup[cd.CommandTopic()] = make(map[string]struct{})
				}
				dedup[cd.CommandTopic()][key] = struct{}{}
				mu.Unlock()
				payloadCopy := make([]byte, len(payload))
				copy(payloadCopy, payload)
				if err := cd.SendRaw(cd.CommandTopic(), payloadCopy); err != nil {
					fmt.Println(err)
					return
				}
				time.AfterFunc(10*time.Second, func() {
					mu.Lock()
					if m, ok := dedup[cd.CommandTopic()]; ok {
						delete(m, key)
						if len(m) == 0 {
							delete(dedup, cd.CommandTopic())
						}
					}
					mu.Unlock()
				})
			}); !token.WaitTimeout(5 * time.Second) {
				return fmt.Errorf("subscribe timeout")
			} else if token.Error() != nil {
				return token.Error()
			}

			if iot {
				ctx, cancel := context.WithCancel(context.Background())
				cancels[id] = cancel
				go func(ctx context.Context, id string, cd devices.ConnectedDevice) {
					var ticker *time.Ticker
					if refresh > 0 {
						ticker = time.NewTicker(time.Duration(refresh) * time.Second)
						defer ticker.Stop()
					}
					credRefresh := time.NewTicker(23 * time.Hour)
					defer credRefresh.Stop()
					for {
						if ticker != nil {
							select {
							case <-ticker.C:
								for _, m := range []string{"REQUEST-CURRENT-FAULTS", "REQUEST-CURRENT-STATE"} {
									ts := time.Now().UTC().Format(time.RFC3339)
									msg := fmt.Sprintf(`{"mode-reason":"RAPP","time":"%s","msg":"%s"}`, ts, m)
									fmt.Printf("Sending %s to %s\n", msg, cd.CommandTopic())
									key := dedupKey([]byte(msg))
									mu.Lock()
									if dedup[cd.CommandTopic()] == nil {
										dedup[cd.CommandTopic()] = make(map[string]struct{})
									}
									dedup[cd.CommandTopic()][key] = struct{}{}
									mu.Unlock()
									time.AfterFunc(10*time.Second, func() {
										mu.Lock()
										if mm, ok := dedup[cd.CommandTopic()]; ok {
											delete(mm, key)
											if len(mm) == 0 {
												delete(dedup, cd.CommandTopic())
											}
										}
										mu.Unlock()
									})
									_ = cd.SendRaw(cd.CommandTopic(), []byte(msg))
								}
							case <-credRefresh.C:
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
						} else {
							select {
							case <-credRefresh.C:
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
		shell.ListenForCtrlX(sig)
		go func() {
			<-sig
			if Verbose {
				fmt.Println("[repeater] disconnecting")
			}
			client.Disconnect(250)
			os.Exit(0)
		}()

		select {}
	}
}
