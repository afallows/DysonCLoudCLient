package devices

import (
	"fmt"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type MQTT struct {
	Password  string `yaml:"password"`
	Username  string `yaml:"username"`
	TopicRoot string `yaml:"root_topic"`
	Address   string `yaml:"address"`
}

func (d *BaseConnectedDevice) mqttOptions() (*paho.ClientOptions, error) {
	opts := paho.NewClientOptions()

	if err := d.ResolveLocalAddress(); err != nil {
		return opts, err
	}

	opts.AddBroker(fmt.Sprintf("%s:1883", d.MQTT.Address))
	opts.SetClientID("libdyson-wg/opendyson")
	opts.SetUsername(d.MQTT.Username)
	opts.SetPassword(d.MQTT.Password)
	opts.SetResumeSubs(true)

	if Verbose {
		fmt.Printf("[MQTT] Broker: %s:1883\n", d.MQTT.Address)
		fmt.Printf("[MQTT] Username: %s Password: %s\n", d.MQTT.Username, d.MQTT.Password)
	}

	return opts, nil
}
