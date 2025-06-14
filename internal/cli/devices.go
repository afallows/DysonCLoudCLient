// internal/cli/devices.go
package cli

import (
    "fmt"
    "os"
    "os/signal"
    "strings"
    "sync"
    "syscall"

    "github.com/libdyson-wg/opendyson/devices"
)

func DeviceGetter(getDevices func() ([]devices.Device, error)) func() ([]devices.Device, error) {
    return func() ([]devices.Device, error) {
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

        if strings.EqualFold(serial, "ALL") {
            found := false
            for _, d := range ds {
                cd, ok := d.(devices.ConnectedDevice)
                if !ok {
                    continue
                }
                found = true
                if iot {
                    cd.SetMode(devices.ModeIoT)
                }
                devSerial := d.GetSerial()
                for name, topic := range map[string]string{
                    "Status:   ":  cd.StatusTopic(),
                    "Fault:    ":  cd.FaultTopic(),
                    "Command:  ":  cd.CommandTopic(),
                } {
                    printLine(fmt.Sprintf("[%s] Subscribing to %s", devSerial, topic))
                    n, t := name, topic
                    if err = cd.SubscribeRaw(t, func(bytes []byte) {
                        printLine(fmt.Sprintf("[%s] %s%s", devSerial, n, string(bytes)))
                    }); err != nil {
                        return err
                    }
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
            if iot {
                cd.SetMode(devices.ModeIoT)
            }
            for name, topic := range map[string]string{
                "Status:   ": cd.StatusTopic(),
                "Fault:    ": cd.FaultTopic(),
                "Command:  ": cd.CommandTopic(),
            } {
                printLine(fmt.Sprintf("Subscribing to %s", topic))
                n, t := name, topic
                if err = cd.SubscribeRaw(t, func(bytes []byte) {
                    printLine(fmt.Sprintf("%s%s", n, string(bytes)))
                }); err != nil {
                    return err
                }
            }
        }

        sig := make(chan os.Signal, 1)
        signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
        go func() {
            <-sig
            os.Exit(0)
        }()

        select {}
    }
}
