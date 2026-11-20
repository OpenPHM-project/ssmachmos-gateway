package model

import (
	"bytes"
	"testing"
)

func TestFluxSettings(t *testing.T) {
	// On Linux/WSL, save settings in a temporary directory for this test.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	previousSensors := Sensors
	t.Cleanup(func() {
		Sensors = previousSensors
	})

	// With only flux enabled as a supported type, there is exactly one settings record.
	mac := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	Sensors = []Sensor{getDefaultSensor(mac, []string{"flux"}, 10000)}

	checkFluxRecord := func(want []byte) {
		t.Helper()

		const headerSize = 11
		got := Sensors[0].SettingsBytes()

		if len(got) != headerSize+len(want) {
			t.Fatalf("settings length = %d bytes, want %d", len(got), headerSize+len(want))
		}

		record := got[headerSize:]
		if !bytes.Equal(record, want) {
			t.Fatalf("flux settings bytes = % X, want % X", record, want)
		}
	}

	// Each record contains: type, active flag, frequency (4 bytes), duration (2 bytes).
	// Default: flux off, 2000 Hz, 1 second.
	checkFluxRecord([]byte{0x03, 0x00, 0xD0, 0x07, 0x00, 0x00, 0x01, 0x00})

	// Change the rate to 1000 Hz while flux is still off.
	if err := UpdateSensorSetting(mac, "flux_sampling_frequency", "1000"); err != nil {
		t.Fatalf("setting flux frequency to 1000 Hz: %v", err)
	}
	checkFluxRecord([]byte{0x03, 0x00, 0xE8, 0x03, 0x00, 0x00, 0x01, 0x00})

	// Turn flux on. The configured rate and duration should stay the same.
	if err := UpdateSensorSetting(mac, "flux_active", "true"); err != nil {
		t.Fatalf("turning flux on: %v", err)
	}
	checkFluxRecord([]byte{0x03, 0x01, 0xE8, 0x03, 0x00, 0x00, 0x01, 0x00})

	// Turn flux off again. Only the active byte should change.
	if err := UpdateSensorSetting(mac, "flux_active", "false"); err != nil {
		t.Fatalf("turning flux off: %v", err)
	}
	checkFluxRecord([]byte{0x03, 0x00, 0xE8, 0x03, 0x00, 0x00, 0x01, 0x00})
}
