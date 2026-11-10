package model

import (
	"bytes"
	"slices"
	"testing"
)

var testMac = [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}

// Header of SettingsBytes for a default sensor:
// device active (1 byte) | MAC (6 bytes) | sleep duration in seconds (uint32, little endian)
// A default sensor is inactive and sleeps 3600 s = 0x00000E10.
var defaultHeader = []byte{0x00, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x10, 0x0E, 0x00, 0x00}

// Every data type is an 8-byte record:
// type id | active | sampling frequency (uint32 LE) | sampling duration (uint16 LE)
var (
	// vibration: id 0x00, 8000 Hz = 0x00001F40, 1 s
	vibrationRecord = []byte{0x00, 0x01, 0x40, 0x1F, 0x00, 0x00, 0x01, 0x00}
	// audio: id 0x01, 22110 Hz = 0x0000565E, 1 s
	audioRecord = []byte{0x01, 0x01, 0x5E, 0x56, 0x00, 0x00, 0x01, 0x00}
	// temperature: id 0x02, active, rest is padding
	temperatureRecord = []byte{0x02, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
)

func TestSettingsBytes_DefaultSensorSingleType(t *testing.T) {
	tests := []struct {
		dataType string
		record   []byte
	}{
		{"vibration", vibrationRecord},
		{"audio", audioRecord},
		{"temperature", temperatureRecord},
	}
	for _, tc := range tests {
		t.Run(tc.dataType, func(t *testing.T) {
			sensor := getDefaultSensor(testMac, []string{tc.dataType}, 4096)

			got := sensor.SettingsBytes()
			want := append(slices.Clone(defaultHeader), tc.record...)
			if !bytes.Equal(got, want) {
				t.Fatalf("SettingsBytes() = % X\n              want % X", got, want)
			}
		})
	}
}

func TestSettingsBytes_DefaultSensorNoTypes(t *testing.T) {
	sensor := getDefaultSensor(testMac, nil, 4096)

	got := sensor.SettingsBytes()
	if !bytes.Equal(got, defaultHeader) {
		t.Fatalf("SettingsBytes() = % X, want header only % X", got, defaultHeader)
	}
}

// The order of the per-type records follows Go map iteration (random),
// so check the header exactly and the 8-byte records as a set.
func TestSettingsBytes_DefaultSensorAllTypes(t *testing.T) {
	sensor := getDefaultSensor(testMac, []string{"vibration", "audio", "temperature"}, 4096)

	got := sensor.SettingsBytes()
	if len(got) != len(defaultHeader)+3*8 {
		t.Fatalf("len(SettingsBytes()) = %d, want %d", len(got), len(defaultHeader)+3*8)
	}
	if !bytes.Equal(got[:len(defaultHeader)], defaultHeader) {
		t.Fatalf("header = % X, want % X", got[:len(defaultHeader)], defaultHeader)
	}

	var gotRecords [][]byte
	for body := got[len(defaultHeader):]; len(body) > 0; body = body[8:] {
		gotRecords = append(gotRecords, body[:8])
	}
	wantRecords := [][]byte{vibrationRecord, audioRecord, temperatureRecord}
	slices.SortFunc(gotRecords, bytes.Compare)
	slices.SortFunc(wantRecords, bytes.Compare)
	for i := range wantRecords {
		if !bytes.Equal(gotRecords[i], wantRecords[i]) {
			t.Errorf("record %d = % X, want % X", i, gotRecords[i], wantRecords[i])
		}
	}
}

func TestSettingsBytes_ActiveDeviceAndSleepTime(t *testing.T) {
	sensor := getDefaultSensor(testMac, nil, 4096)
	sensor.DeviceActive = true
	sensor.WakeUpInterval = 0x01020304 // seconds

	got := sensor.SettingsBytes()
	want := []byte{0x01, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x04, 0x03, 0x02, 0x01}
	if !bytes.Equal(got, want) {
		t.Fatalf("SettingsBytes() = % X, want % X", got, want)
	}
}

func TestSettingsBytes_InactiveDataTypeAndCustomSettings(t *testing.T) {
	sensor := getDefaultSensor(testMac, []string{"vibration"}, 4096)
	sensor.Settings["vibration"] = settings{Active: false, SamplingFrequency: 0x00010203, SamplingDuration: 0x0405}

	got := sensor.SettingsBytes()
	want := append(slices.Clone(defaultHeader),
		0x00, 0x00, // vibration, not active
		0x03, 0x02, 0x01, 0x00, // frequency, little endian
		0x05, 0x04, // duration, little endian
	)
	if !bytes.Equal(got, want) {
		t.Fatalf("SettingsBytes() = % X, want % X", got, want)
	}
}

func TestSettingsBytes_UnknownDataTypeIsSkipped(t *testing.T) {
	sensor := getDefaultSensor(testMac, nil, 4096)
	sensor.Settings["radar"] = settings{Active: true}

	got := sensor.SettingsBytes()
	if !bytes.Equal(got, defaultHeader) {
		t.Fatalf("SettingsBytes() = % X, want header only % X", got, defaultHeader)
	}
}