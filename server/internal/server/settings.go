// Provides configuration settings for registered sensors

package server

import (
	"time"
	"github.com/jukuly/ss_machmos/server/internal/model"
	"github.com/jukuly/ss_machmos/server/internal/out"
)


/* 0x01 | mac address | Sleep until | repeat {dataTypeByte | active | Sampling Frequency | SamplingDuration}
Finds the registered sensor matching the supplied address
Returns the sensor's settings encoded as bytes
OR
Returns a single 0 byte if the sensor is unknown
*/
func getSettingsForSensor(address string) []byte {
	// Convert the address to a mac address 
	mac, _ := model.StringToMac(address)

	// Find the sensor in the list of registered sensors
	var sensor *model.Sensor
	for i, s := range model.Sensors {
		if s.Mac == mac {
			sensor = &(model.Sensors[i])
			break
		}
	}
	// If the sensor is not found, return a single 0 byte
	if sensor == nil {
		out.Logger.Println("Device", address, "not found in settings, reject")
		return []byte{0x00}
	}
	// Record the current time as the sensor’s last activity and mark it as idle
	sensor.UpdateLastSeen(model.SensorActivityIdle)

	// Get the settings for the sensor and return them as bytes
	settings := sensor.SettingsBytes()

	// Debug announce setting returned
	out.Logger.Printf("%s [%s] receives:\n\t[% x]", sensor.Name, sensor.MacString(), settings)

	return settings
}

/* Estimates how long the sensor passed as parameter needs to remain awake by adding a 30s
transmission allowance to its longest configured sampling duration*/
func getWakeUpDuration(sensor *model.Sensor) time.Duration {
	WAKE_UP_DURATION_BASELINE := time.Second * 30 // baseline to account for transmission time

	// Loops through all settings to find the longest sampling duration
	var maxSamplingDuration uint16 = 0
	for _, setting := range sensor.Settings {
		if setting.SamplingDuration > maxSamplingDuration {
			maxSamplingDuration = setting.SamplingDuration
		}
	}

	// Convert the sampling duration from seconds to a Go duration, then add the transmission allowance/baseline
	return time.Duration(maxSamplingDuration)*time.Second + WAKE_UP_DURATION_BASELINE
}
