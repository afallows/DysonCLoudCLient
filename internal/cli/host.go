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

	mqttsrv "github.com/mochi-co/mqtt/server"
	"github.com/mochi-co/mqtt/server/events"
	"github.com/mochi-co/mqtt/server/listeners"
	"github.com/mochi-co/mqtt/server/listeners/auth"

	"github.com/libdyson-wg/opendyson/devices"
)

func Host(
	getDevices func() ([]devices.Device, error),
) func(serial string, iot bool) error {
	return func(serial string, iot bool) error {
		srv := mqttsrv.New()
		tcp := listeners.NewTCP("t1", ":1883")
		if err := srv.AddListener(tcp, &listeners.Config{Auth: new(auth.Allow)}); err != nil {
			return fmt.Errorf("add listener: %w", err)
		}
		go func() {
			if err := srv.Serve(); err != nil {
				fmt.Println(err)
			}
		}()

		ds, err := getDevices()
		if err != nil {
			return err
		}

		subscribed := make(map[string]struct{})
		commandTargets := make(map[string]devices.ConnectedDevice)
		mu := sync.RWMutex{}
		dedup := make(map[string]map[string]struct{})

		srv.Events.OnMessage = func(cl events.Client, pk events.Packet) (events.Packet, error) {
			mu.RLock()
			cd, ok := commandTargets[pk.TopicName]
			mu.RUnlock()
			if ok {
				payload := pk.Payload
				go func(d devices.ConnectedDevice, p []byte) {
					if err := d.SendRaw(d.CommandTopic(), p); err != nil {
						fmt.Println("relay:", err)
						return
					}
					mu.Lock()
					if dedup[d.CommandTopic()] == nil {
						dedup[d.CommandTopic()] = make(map[string]struct{})
					}
					dedup[d.CommandTopic()][string(p)] = struct{}{}
					mu.Unlock()
					time.AfterFunc(10*time.Second, func() {
						mu.Lock()
						if m, ok := dedup[d.CommandTopic()]; ok {
							delete(m, string(p))
							if len(m) == 0 {
								delete(dedup, d.CommandTopic())
							}
						}
						mu.Unlock()
					})
				}(cd, payload)
			}
			return pk, nil
		}

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
					if t == cd.CommandTopic() {
						mu.Lock()
						if m, ok := dedup[t]; ok {
							if _, seen := m[string(b)]; seen {
								delete(m, string(b))
								if len(m) == 0 {
									delete(dedup, t)
								}
								mu.Unlock()
								return
							}
						}
						mu.Unlock()
					}
					fmt.Printf("Incoming message %s on topic %s\n", string(b), t)
					srv.Publish(t, b, false)
				}); err != nil {
					return err
				}
			}

			mu.Lock()
			commandTargets[cd.CommandTopic()] = cd
			mu.Unlock()

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
			srv.Close()
			os.Exit(0)
		}()

		select {}
	}
}
