package model

import (
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"
)

var settingTestMac = [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}

// A sensor with 100000 bytes of storage and the default settings uses:
//
//	vibration: 8000 Hz * 1 s * 6 bytes = 48000 bytes
//	temperature:                           2 bytes
//
// which leaves room to grow, so (see isExceedingCollectionCapacity):
//
//	largest vibration frequency = (100000-48002)/(6*1) + 8000 = 16666 Hz
//	largest vibration duration  = (100000-48002)/(6*8000) + 1 = 2 s
const settingTestCapacity = 100000

// setUpSettingTest gives the test its own empty config folder and one sensor
// (added with AddSensor, so it is saved to sensors.json like in the real app).
func setUpSettingTest(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	oldSensors := Sensors
	Sensors = []Sensor{}
	t.Cleanup(func() { Sensors = oldSensors })

	if err := AddSensor(settingTestMac, []string{"vibration", "temperature"}, settingTestCapacity); err != nil {
		t.Fatalf("AddSensor: %v", err)
	}
}

// Every one of these must return an error, and must leave the sensor untouched.
func TestUpdateSensorSetting_ReturnsErrors(t *testing.T) {
	tests := []struct {
		name    string
		mac     [6]byte
		setting string
		value   string
		wantMsg string // part of the error message, to be sure the right check fired
	}{
		// The six cases from the ticket.
		{"unknown sensor MAC", [6]byte{1, 2, 3, 4, 5, 6}, "name", "Pump", "sensor not found"},
		{"bad setting format (no underscore)", settingTestMac, "banana", "1", "invalid setting format"},
		{"unknown data type", settingTestMac, "banana_sampling_frequency", "100", "invalid setting data type"},
		{"frequency is not a number", settingTestMac, "vibration_sampling_frequency", "abc", "sampling_frequency"},
		{"negative duration", settingTestMac, "vibration_sampling_duration", "-1", "sampling_duration"},
		{"frequency exceeds collection capacity", settingTestMac, "vibration_sampling_frequency", "16667", "exceeds collection capacity"},

		// The other error paths of the same function.
		{"duration exceeds collection capacity", settingTestMac, "vibration_sampling_duration", "3", "exceeds collection capacity"},
		{"duration is not a number", settingTestMac, "vibration_sampling_duration", "abc", "sampling_duration"},
		{"negative frequency", settingTestMac, "vibration_sampling_frequency", "-5", "sampling_frequency"},
		{"frequency too big for a uint32", settingTestMac, "vibration_sampling_frequency", "4294967296", "sampling_frequency"},
		{"duration too big for a uint16", settingTestMac, "vibration_sampling_duration", "65536", "sampling_duration"},
		{"unknown setting name for a known type", settingTestMac, "vibration_banana", "1", "doesn't exist"},
		{"device_active is not a boolean", settingTestMac, "device_active", "maybe", "device_active"},
		{"wake_up_interval is not a number", settingTestMac, "wake_up_interval", "soon", "wake_up_interval"},
		{"max offset not below the wake up interval", settingTestMac, "wake_up_interval_max_offset", "3600", "wake_up_interval_max_offset"},
		{"negative max offset", settingTestMac, "wake_up_interval_max_offset", "-1", "wake_up_interval_max_offset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setUpSettingTest(t)
			before, _ := json.Marshal(Sensors)

			err := UpdateSensorSetting(tt.mac, tt.setting, tt.value)

			if err == nil {
				t.Fatalf("UpdateSensorSetting(%q, %q) returned nil, want an error", tt.setting, tt.value)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tt.wantMsg)
			}
			if after, _ := json.Marshal(Sensors); string(after) != string(before) {
				t.Errorf("a failed update changed the sensor:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

func TestUpdateSensorSetting_ErrorsWhenSensorListIsNil(t *testing.T) {
	setUpSettingTest(t)
	Sensors = nil // restored by the cleanup in setUpSettingTest

	if err := UpdateSensorSetting(settingTestMac, "name", "Pump"); err == nil {
		t.Fatal("expected an error when the sensor list is nil, got nil")
	}
}

// Control test: the same set-up accepts valid values, so the errors above come
// from the bad values and not from a broken test set-up.
func TestUpdateSensorSetting_AcceptsValidValues(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		value   string
		check   func(s Sensor) bool
	}{
		{"name", "name", "Pump 3", func(s Sensor) bool { return s.Name == "Pump 3" }},
		{"largest frequency that fits", "vibration_sampling_frequency", "16666", func(s Sensor) bool { return s.Settings["vibration"].SamplingFrequency == 16666 }},
		{"largest duration that fits", "vibration_sampling_duration", "2", func(s Sensor) bool { return s.Settings["vibration"].SamplingDuration == 2 }},
		{"turn a data type off", "vibration_active", "false", func(s Sensor) bool { return !s.Settings["vibration"].Active }},
		{"device_active", "device_active", "true", func(s Sensor) bool { return s.DeviceActive }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setUpSettingTest(t)

			if err := UpdateSensorSetting(settingTestMac, tt.setting, tt.value); err != nil {
				t.Fatalf("UpdateSensorSetting(%q, %q) = %v, want nil", tt.setting, tt.value, err)
			}
			if !tt.check(Sensors[0]) {
				t.Errorf("the sensor was not updated: %+v", Sensors[0])
			}

			// The change is saved to sensors.json too.
			dir, err := GetConfigDir()
			if err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(path.Join(dir, SENSORS_FILE))
			if err != nil {
				t.Fatalf("sensors.json was not written: %v", err)
			}
			var onDisk []Sensor
			if err := json.Unmarshal(saved, &onDisk); err != nil || len(onDisk) != 1 {
				t.Fatalf("sensors.json is not a list with one sensor: %v (%s)", err, saved)
			}
			if !tt.check(onDisk[0]) {
				t.Errorf("the saved file does not hold the change: %s", saved)
			}
		})
	}
}