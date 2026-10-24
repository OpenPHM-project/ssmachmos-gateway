// Package model stores what the gateway remembers about sensors.
// This file defines a sensor, its settings, and how they are saved to and loaded from JSON files.
package model

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/out"
)

// SENSORS_FILE is the file that stores every approved sensor and its settings.
const SENSORS_FILE = "sensors.json"

// SENSOR_HISTORY_FILE is the file that stores when each sensor was last seen.
const SENSOR_HISTORY_FILE = "sensor_history.json"

// Sensors is the list of all approved sensors and their settings.
var Sensors []Sensor = []Sensor{}

// DATA_SIZE is how many bytes one sample takes for each measurement type.
// Vibration is 6 bytes because it has 3 axes (x, y, z) of 2 bytes each.
var DATA_SIZE = map[string]int{
	"temperature": 2,
	"audio":       3,
	"flux":        2,
	"vibration":   2 * 3,
}

// SENSOR_MODELS maps the model number a sensor sends during pairing to its name.
var SENSOR_MODELS map[byte]string = map[byte]string{
	0x1: "machmo",
	0x2: "machmomini",
}

// SensorActivity is what a sensor was last doing.
type SensorActivity string

// The possible values of SensorActivity.
const (
	SensorActivityIdle         SensorActivity = "idle"
	SensorActivityCollecting   SensorActivity = "collecting"
	SensorActivityTransmitting SensorActivity = "transmitting"
)

// SensorLastSeen records when a sensor was last seen and what it was doing.
type SensorLastSeen struct {
	Mac          string         `json:"mac"`
	LastActivity SensorActivity `json:"last_activity"`
	LastSeen     time.Time      `json:"last_seen"`
}

// settings holds the recording settings for one measurement type.
// SamplingFrequency is in Hz and SamplingDuration is in seconds.
type settings struct {
	Active            bool   `json:"active"`
	SamplingFrequency uint32 `json:"sampling_frequency"`
	SamplingDuration  uint16 `json:"sampling_duration"`
}

// Sensor is everything the gateway remembers about one approved sensor.
// Mac (the Bluetooth address) is the sensor's unique ID.
type Sensor struct {
	Mac                     [6]byte             `json:"mac"`
	Name                    string              `json:"name"`
	Model                   string              `json:"model"`
	Types                   []string            `json:"types"`
	BatteryLevel            int                 `json:"battery_level"` // -1 means unknown
	CollectionCapacity      uint32              `json:"collection_capacity"`
	WakeUpInterval          int                 `json:"wake_up_interval"` // Time in seconds to sleep for
	WakeUpIntervalMaxOffset int                 `json:"wake_up_interval_max_offset"`
	DeviceActive            bool                `json:"device_active"` // false keeps the sensor on standby
	Settings                map[string]settings `json:"settings"`
}

// MacString returns the sensor's Bluetooth address as text.
func (sensor *Sensor) MacString() string {
	return MacToString(sensor.Mac)
}

// GetSleepDuration returns how long the sensor sleeps between check-ups, in seconds.
func (sensor *Sensor) GetSleepDuration() uint32 {
	return uint32(sensor.WakeUpInterval)
}

// Verify checks that the sensor has a name and its recordings fit in its storage.
// BUG: errors.Join's result is never saved into err, so Verify always returns nil.
func (sensor *Sensor) Verify() error {
	var err error = nil
	if sensor.Name == "" {
		errors.Join(err, errors.New("Name must not be null"))
	}

	totalDataUsed := 0
	for k, v := range sensor.Settings {
		thisSize := DATA_SIZE[k] * int(v.SamplingDuration) * int(v.SamplingFrequency)
		totalDataUsed += thisSize
	}
	if totalDataUsed > int(sensor.CollectionCapacity) {
		errors.Join(err, errors.New(fmt.Sprint("Current requested capacity", totalDataUsed, "exceeds maximum capacity", sensor.CollectionCapacity)))
	}

	return err
}

// SensorHistory stores when each sensor was last seen, keyed by its address as text.
var SensorHistory map[string]SensorLastSeen = map[string]SensorLastSeen{}

// UpdateLastSeen records that the sensor was just seen, saves it, and tells the app.
func (s *Sensor) UpdateLastSeen(activity SensorActivity) {
	hist, ok := SensorHistory[MacToString(s.Mac)]
	if !ok {
		hist = SensorLastSeen{}
	}
	hist.LastSeen = time.Now().UTC() // UTC so times match no matter the gateway's time zone
	hist.LastActivity = activity
	SensorHistory[MacToString(s.Mac)] = hist
	saveSensorHistory()
	out.Broadcast("SENSOR-UPDATED")
}

// FetchLastSeen returns when the sensor was last seen and what it was doing.
func (s *Sensor) FetchLastSeen() SensorLastSeen {
	return SensorHistory[MacToString(s.Mac)]
}

// ToString returns the sensor's details as readable text for the CLI.
func (s *Sensor) ToString() string {
	str := s.Name + " - " + MacToString(s.Mac) + "\n"
	str += "Sensor Types: "
	for i, t := range s.Types {
		if i < len(s.Types)-1 {
			str += t + ", "
		} else {
			str += t + "\n"
		}
	}
	str += "Battery Level: "
	if s.BatteryLevel == -1 {
		str += "Unknown\n"
	} else {
		str += strconv.Itoa(s.BatteryLevel) + " %\n"
	}
	str += "Collection Capacity: " + strconv.Itoa(int(s.CollectionCapacity)) + " bytes\n"
	str += "Wake Up Interval: " + strconv.Itoa(s.WakeUpInterval) + " +- " + strconv.Itoa(s.WakeUpIntervalMaxOffset) + " seconds\n"
	// OUT OF DATE: NextWakeUp was removed from Sensor.
	// str += "Next Wake Up: " + s.NextWakeUp.Local().Format(time.RFC3339) + "\n"
	// BUG: missing "\n" here, so "Settings:" prints on the same line.
	str += "\t\tDevice is Active: " + strconv.FormatBool(s.DeviceActive)
	str += "Settings:\n"
	for setting, value := range s.Settings {
		str += "\t" + setting + ":\n"
		str += "\t\tActive: " + strconv.FormatBool(value.Active) + "\n"
		// Temperature is a single reading, so it has no frequency or duration.
		if setting == "temperature" {
			continue
		}
		str += "\t\tSampling Frequency: " + strconv.Itoa(int(value.SamplingFrequency)) + " Hz\n"
		str += "\t\tSampling Duration: " + strconv.Itoa(int(value.SamplingDuration)) + " seconds\n"
	}
	return str
}

// IsMacEqual reports whether the sensor has the given address (written as text).
func (s *Sensor) IsMacEqual(mac string) bool {
	m, err := StringToMac(mac)
	if err != nil {
		return false
	}
	return s.Mac == m
}

// StringToMac converts an address like "AA:BB:CC:11:22:33" into 6 bytes.
func StringToMac(mac string) ([6]byte, error) {
	var m [6]byte
	_, err := fmt.Sscanf(mac, "%02X:%02X:%02X:%02X:%02X:%02X", &m[0], &m[1], &m[2], &m[3], &m[4], &m[5])
	if err != nil {
		return [6]byte{}, err
	}
	return m, nil
}

// MacToString converts 6 address bytes into text like "AA:BB:CC:11:22:33".
func MacToString(mac [6]byte) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

// LoadSensors reads sensors.json into Sensors when the gateway starts.
// If any sensor fails Verify, nothing is loaded.
func LoadSensors() error {
	// Load sensors from home directory, because access to /var/ by non-root can be tricky
	configPath, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	err = os.MkdirAll(path.Join(configPath, "ss_machmos"), 0777)
	if err != nil {
		return err
	}

	jsonStr, err := os.ReadFile(path.Join(configPath, "ss_machmos", SENSORS_FILE))
	if err != nil {
		//sensors = make([]Sensor, 0)
		return err
	}
	var newSensors []Sensor
	err = json.Unmarshal(jsonStr, &newSensors)
	if err != nil {
		return err
	}

	// Verify loaded configuration for errors
	for _, curSensor := range newSensors {
		curErr := curSensor.Verify()
		if curErr != nil {
			err = errors.Join(err, curErr)
		}
	}
	if err != nil {
		return err
	}

	// No errors, copy over new config
	Sensors = newSensors
	return nil
}

// RemoveSensor deletes the sensor with this address and saves the file.
func RemoveSensor(mac [6]byte) error {
	if Sensors == nil {
		return errors.New("sensors is nil")
	}
	for i, s := range Sensors {
		if s.Mac == mac {
			//Sensors = append((Sensors)[:i], (Sensors)[i+1:]...)
			Sensors = slices.Delete(Sensors, i, i+1)
			err := saveSensors()
			return err
		}
	}
	return nil
}

// getDefaultSensor creates a new sensor with default settings for each type it supports.
// OUT OF DATE: the publicKey parameter is from the old RSA design.
// BUG: flux gets no default settings.
func getDefaultSensor(mac [6]byte, types []string, collectionCapacity uint32 /*, publicKey *rsa.PublicKey*/) Sensor {
	sensor := Sensor{
		Mac:                     mac,
		Name:                    "Sensor " + MacToString(mac),
		Model:                   "unknown",
		Types:                   types,
		BatteryLevel:            -1,
		CollectionCapacity:      collectionCapacity,
		WakeUpInterval:          3600, // wake once an hour
		WakeUpIntervalMaxOffset: 300,  // allow up to 5 minutes early or late
		// OUT OF DATE: NextWakeUp was removed from Sensor.
		// NextWakeUp:              time.Now().Add(3600 * time.Second),
		DeviceActive: false, // new sensors stay on standby until someone turns them on
		Settings:     map[string]settings{},
	}

	for _, t := range types {
		switch t {
		case "vibration":
			sensor.Settings["vibration"] = settings{
				Active:            true,
				SamplingFrequency: 8000, // 8 kHz default
				SamplingDuration:  1,
			}
		case "temperature":
			sensor.Settings["temperature"] = settings{
				Active: true,
			}
		case "audio":
			sensor.Settings["audio"] = settings{
				Active:            true,
				SamplingFrequency: 22110, // about 22 kHz default
				SamplingDuration:  1,
			}
		}
	}

	return sensor
}

// AddSensor adds a newly approved sensor with default settings and saves the file.
func AddSensor(mac [6]byte, types []string, collectionCapacity uint32) error {
	if Sensors == nil {
		return errors.New("sensors is nil")
	}

	Sensors = append(Sensors, getDefaultSensor(mac, types, collectionCapacity))
	err := saveSensors()
	return err
}

// SettingsBytes packs the sensor's settings into the bytes sent to it over Bluetooth.
// Layout: active (1) + MAC (6) + sleep seconds (4) + 8 bytes per measurement type.
// BUG: flux settings are skipped as an "unknown data type".
func (sensor *Sensor) SettingsBytes() []byte {
	response := []byte{}
	if sensor.DeviceActive {
		response = append(response, 0x01)
	} else {
		response = append(response, 0x00)
	}
	response = append(response, sensor.Mac[:]...)
	// Place time in seconds
	response = binary.LittleEndian.AppendUint32(response, sensor.GetSleepDuration())

	for dataType, settings := range sensor.Settings {
		var active byte
		if settings.Active {
			active = 0x01
		} else {
			active = 0x00
		}
		// Type codes the sensor expects: 0x00 vibration, 0x01 audio, 0x02 temperature.
		switch dataType {
		case "vibration":
			// 1 + 1 + 4 + 2 = 8
			response = append(response, 0x00, active)
			response = binary.LittleEndian.AppendUint32(response, settings.SamplingFrequency)
			response = binary.LittleEndian.AppendUint16(response, settings.SamplingDuration)
		case "audio":
			// 1 + 1 + 4 + 2 = 8
			response = append(response, 0x01, active)
			response = binary.LittleEndian.AppendUint32(response, settings.SamplingFrequency)
			response = binary.LittleEndian.AppendUint16(response, settings.SamplingDuration)
		case "temperature":
			// Padded with zeros so every type takes 8 bytes.
			// 							1	  2       3		4	  5	    6	  7     8
			response = append(response, 0x02, active, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
		default:
			out.Logger.Printf("%s [%s]: Parsing unknown data type %s", sensor.Name, MacToString(sensor.Mac), dataType)
		}
	}

	return response
}

// UpdateSensorSetting changes one setting on a sensor and saves the file.
// Settings are named like "name", "auto" (reset to defaults) or "vibration_sampling_frequency".
// BUG: "flux_..." settings are rejected as an invalid data type.
func UpdateSensorSetting(mac [6]byte, setting string, value string) error {
	if Sensors == nil {
		return errors.New("sensors is nil")
	}

	var sensor *Sensor
	for i, s := range Sensors {
		if s.Mac == mac {
			sensor = &(Sensors)[i]
			break
		}
	}
	if sensor == nil {
		return errors.New("sensor not found")
	}

	if setting == "auto" {
		// OUT OF DATE: the publicKey argument is from the old RSA design.
		*sensor = getDefaultSensor(mac, sensor.Types, sensor.CollectionCapacity /*, &sensor.PublicKey*/)
		return saveSensors()
	}

	if setting == "name" {
		sensor.Name = value
		return saveSensors()
	}

	if setting == "device_active" {
		deviceActive, err := strconv.ParseBool(value)
		if err != nil {
			return errors.New("invalid boolean for device_active " + value)
		}
		sensor.DeviceActive = deviceActive
		return saveSensors()
	}

	if setting == "wake_up_interval" {
		intValue, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("invalid value for wake_up_interval setting (must be an integer (seconds))")
		}
		// OUT OF DATE: this range check was turned off when the interval changed from milliseconds to seconds.
		// when converted to milliseconds it will not be a uint32
		// if intValue < sensor.WakeUpIntervalMaxOffset || intValue > 4294967 {
		// 	return errors.New("invalid value for wake_up_interval setting (must an integer between wake_up_interval_max_offset and 4 294 967)")
		// }
		sensor.WakeUpInterval = intValue
		return saveSensors()
	}
	if setting == "wake_up_interval_max_offset" {
		intValue, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("invalid value for wake_up_interval_max_offset setting (must be an integer (seconds))")
		}
		// the max offset must be smaller than the wake up interval
		if intValue < 0 || intValue >= sensor.WakeUpInterval {
			return errors.New("invalid value for wake_up_interval_max_offset setting (must an integer between 0 and wake_up_interval)")
		}
		sensor.WakeUpIntervalMaxOffset = intValue
		return saveSensors()
	}

	settingParts := strings.Split(setting, "_")
	if len(settingParts) < 2 {
		return errors.New("invalid setting format")
	}

	dataType := settingParts[0]
	if dataType != "vibration" && dataType != "temperature" && dataType != "audio" {
		return errors.New("invalid setting data type")
	}
	setting = strings.Join(settingParts[1:], "_")

	switch setting {
	case "active":
		setting := sensor.Settings[dataType]
		setting.Active = value == "true"
		sensor.Settings[dataType] = setting
	case "sampling_frequency":
		intValue, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("invalid value for sampling_frequency setting (must an integer (Hz))")
		}
		// 4294967295 is the largest number a uint32 can hold.
		if intValue < 0 || intValue > 4294967295 {
			return errors.New("invalid value for sampling_frequency setting (must an integer between 0 and 4 294 967 295)")
		}
		err = isExceedingCollectionCapacity(sensor, "sampling_frequency", intValue, dataType)
		if err != nil {
			return err
		}

		setting := sensor.Settings[dataType]
		setting.SamplingFrequency = uint32(intValue)
		sensor.Settings[dataType] = setting
	case "sampling_duration":
		intValue, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("invalid value for sampling_duration setting (must be an integer (seconds))")
		}
		// 65535 is the largest number a uint16 can hold.
		if intValue < 0 || intValue > 65535 {
			return errors.New("invalid value for sampling_duration setting (must an integer between 0 and 65 535)")
		}
		err = isExceedingCollectionCapacity(sensor, "sampling_duration", intValue, dataType)
		if err != nil {
			return err
		}

		setting := sensor.Settings[dataType]
		setting.SamplingDuration = uint16(intValue)
		sensor.Settings[dataType] = setting
	default:
		return errors.New("setting " + setting + " doesn't exist")
	}

	return saveSensors()
}

// getCollectionSize returns how many bytes one full recording takes with the current settings
// (sample size × frequency × duration, added up for every type).
func getCollectionSize(sensor *Sensor) int {
	result := 0
	for dataType, settings := range sensor.Settings {
		if dataType == "temperature" { // Temperature data is a single number (no frequency or duration)
			result += DATA_SIZE["temperature"]
			continue
		}

		term := int(settings.SamplingFrequency) * int(settings.SamplingDuration)
		term *= DATA_SIZE[dataType]
		result += term
	}
	return result
}

// isExceedingCollectionCapacity returns an error if a new frequency or duration
// would make the recording too big for the sensor's storage.
// BUG: if dataType is not in DATA_SIZE, sizeOfData is 0 and the division crashes.
func isExceedingCollectionCapacity(sensor *Sensor, setting string, newSettingValue int, dataType string) error {
	if dataType == "temperature" {
		return nil
	}

	settings := sensor.Settings[dataType]
	if settings.SamplingDuration == 0 || settings.SamplingFrequency == 0 {
		return errors.New("sampling_duration and sampling_frequency must be greater than 0")
	}

	sizeOfData := DATA_SIZE[dataType]
	var thisFactor int
	var otherFactor int
	if setting == "sampling_frequency" {
		thisFactor = int(settings.SamplingFrequency)
		otherFactor = int(settings.SamplingDuration)
	} else {
		thisFactor = int(settings.SamplingDuration)
		otherFactor = int(settings.SamplingFrequency)
	}

	// Free space left, turned into how much this setting can grow, plus its current value.
	max := (int(sensor.CollectionCapacity)-getCollectionSize(sensor))/(sizeOfData*otherFactor) + thisFactor
	if newSettingValue > max {
		return errors.New("invalid value " + strconv.Itoa(newSettingValue) + " for " + setting + " setting (exceeds collection capacity of sensor for " + dataType + " (current maximum: " + strconv.Itoa(max) + "))")
	}
	return nil
}

// GetConfigDir returns the ~/.config/ss_machmos folder, creating it if needed.
func GetConfigDir() (string, error) {
	configPath, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	err = os.MkdirAll(path.Join(configPath, "ss_machmos"), 0777)
	if err != nil {
		return "", err
	}

	return path.Join(configPath, "ss_machmos"), nil
}

// saveSensors writes the Sensors list to sensors.json.
// OUT OF DATE: the old comment below says it takes input, but it takes none.
// Only reason we take as input is because we can't import server ourselves
// BUG: 0777 lets any user on the computer edit the file; 0644 would be safer.
func saveSensors() error {
	if Sensors == nil {
		return errors.New("sensors is nil")
	}

	jsonStr, err := json.MarshalIndent(Sensors, "", "\t")
	if err != nil {
		return err
	}

	confDir, err := GetConfigDir()
	if err != nil {
		return err
	}

	return os.WriteFile(path.Join(confDir, SENSORS_FILE), jsonStr, 0777)
}

// LoadSensorHistory reads sensor_history.json into SensorHistory at startup.
func LoadSensorHistory() error {
	confDir, err := GetConfigDir()
	if err != nil {
		return err
	}

	jsonStr, err := os.ReadFile(path.Join(confDir, SENSOR_HISTORY_FILE))
	if err != nil {
		SensorHistory = make(map[string]SensorLastSeen)
		return err
	}

	err = json.Unmarshal(jsonStr, &SensorHistory)
	if err != nil {
		return err
	}
	return nil
}

// saveSensorHistory writes SensorHistory to sensor_history.json.
// BUG: 0777 lets any user on the computer edit the file; 0644 would be safer.
func saveSensorHistory() error {
	confDir, err := GetConfigDir()
	if err != nil {
		return err
	}

	jsonStr, err := json.MarshalIndent(SensorHistory, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(path.Join(confDir, SENSOR_HISTORY_FILE), jsonStr, 0777)
}
