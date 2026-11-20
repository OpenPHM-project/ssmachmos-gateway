package server

import (
	"reflect"
	"testing"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

// These tests assume little-endian input.
// Expected values are raw ADC counts, not converted magnetic field values.
func TestHandleFluxData_DecodeLittleEndian(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    []int16
	}{
		{
			name:    "zero",
			payload: []byte{0x00, 0x00},
			want:    []int16{0},
		},
		{
			name:    "positive value",
			payload: []byte{0x01, 0x00},
			want:    []int16{1},
		},
		{
			name:    "byte order",
			payload: []byte{0x00, 0x01},
			want:    []int16{256},
		},
		{
			name:    "negative value",
			payload: []byte{0xFF, 0xFF},
			want:    []int16{-1},
		},
		{
			name:    "maximum signed value",
			payload: []byte{0xFF, 0x7F},
			want:    []int16{32767},
		},
		{
			name:    "minimum signed value",
			payload: []byte{0x00, 0x80},
			want:    []int16{-32768},
		},
		{
			name:    "multiple samples preserve order",
			payload: []byte{0x01, 0x00, 0xFF, 0xFF, 0x00, 0x01},
			want:    []int16{1, -1, 256},
		},
	}

	mac := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	timestamp := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transmission := Transmission{
				macAddress:        mac,
				sensorModel:       "machmo",
				timestamp:         timestamp,
				dataType:          "flux",
				samplingFrequency: 1000,
				packets:           tt.payload,
			}

			measurements := handleFluxData(transmission)

			if len(measurements) != 1 {
				t.Fatalf("got %d measurement records, want 1", len(measurements))
			}

			record := measurements[0]

			readings, ok := record["raw_data"].([]int16)
			if !ok {
				t.Fatalf("raw_data has type %T, want []int16", record["raw_data"])
			}

			if !reflect.DeepEqual(readings, tt.want) {
				t.Errorf("raw_data = %v, want %v", readings, tt.want)
			}

			if record["sensor_id"] != model.MacToString(mac) {
				t.Errorf("sensor_id = %v, want %s", record["sensor_id"], model.MacToString(mac))
			}

			gotTime, ok := record["time"].(time.Time)
			if !ok {
				t.Fatalf("time has type %T, want time.Time", record["time"])
			}
			if !gotTime.Equal(timestamp) {
				t.Errorf("time = %v, want %v", gotTime, timestamp)
			}

			if record["measurement_type"] != "flux" {
				t.Errorf("measurement_type = %v, want flux", record["measurement_type"])
			}

			if record["sampling_frequency"] != transmission.samplingFrequency {
				t.Errorf(
					"sampling_frequency = %v, want %d",
					record["sampling_frequency"],
					transmission.samplingFrequency,
				)
			}

			if _, exists := record["axis"]; exists {
				t.Error("flux record should not contain an axis field")
			}
		})
	}
}

func TestHandleFluxData_InvalidPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{
			name:    "nil payload",
			payload: nil,
		},
		{
			name:    "empty payload",
			payload: []byte{},
		},
		{
			name:    "one incomplete sample",
			payload: []byte{0x01},
		},
		{
			name:    "complete sample followed by incomplete sample",
			payload: []byte{0x01, 0x00, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transmission := Transmission{
				dataType: "flux",
				packets:  tt.payload,
			}

			measurements := handleFluxData(transmission)

			if len(measurements) != 0 {
				t.Errorf(
					"got %d measurement records for invalid payload, want 0",
					len(measurements),
				)
			}
		})
	}
}