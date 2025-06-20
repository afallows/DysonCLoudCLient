package cloud

import (
	"context"
	"fmt"
	"github.com/libdyson-wg/opendyson/internal/generated/oapi"
	"net/http"
	"time"

	"github.com/libdyson-wg/opendyson/devices"
)

func GetDevices() ([]devices.Device, error) {
	if Verbose {
		fmt.Printf("[cloud] fetching device list from %s\n", CurrentServer())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()
	resp, err := client.GetDevicesWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting devices from cloud: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("error getting devices from cloud, http status code: %d", resp.StatusCode())
	}

	ds := make([]devices.Device, len(*resp.JSON200))
	for i := 0; i < len(ds); i++ {
		ds[i] = resp2device((*resp.JSON200)[i])
	}

	return ds, nil
}

func GetDeviceIoT(serial string) (devices.IoT, error) {
	if Verbose {
		fmt.Printf("[cloud] requesting IoT credentials for %s\n", serial)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	resp, err := client.GetIoTInfoWithResponse(ctx, oapi.GetIoTInfoJSONRequestBody{
		Serial: serial,
	})

	if err != nil {
		return devices.IoT{}, fmt.Errorf("error getting IoT info from cloud for %s, %w", serial, err)
	}

	if resp.StatusCode() != http.StatusOK {
		return devices.IoT{}, fmt.Errorf("error getting IoT info from cloud, http status code: %d", resp.StatusCode())
	}

	iot := mapIoT(*resp.JSON200)
	if Verbose {
		fmt.Printf("[cloud] endpoint: %s\n", iot.Endpoint)
		fmt.Printf("[cloud] clientID: %s\n", iot.ClientID)
		fmt.Printf("[cloud] token key: %s value: %s\n", iot.TokenKey, iot.TokenValue)
		fmt.Printf("[cloud] token signature: %s\n", iot.TokenSignature)
	}
	return iot, nil
}
