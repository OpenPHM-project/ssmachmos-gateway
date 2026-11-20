package model

import (
	"testing"
)

func TestVerify(t *testing.T) {
	s := Sensor{
		Name:               "sensor1",
		CollectionCapacity: 599,
		Settings:           map[string]settings{"vibration": settings{SamplingFrequency: 100, SamplingDuration: 1}},
	}

	err := s.Verify()

	if err == nil {
		t.Fatal("expected an error, got nil")
	}

}

func TestLoadSensorsFirstRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := LoadSensors()

	if err != nil {
		t.Fatal("expected no error, got", err)
	}

	if len(Sensors) != 0 {
		t.Fatal("expected sensor number 0, but got", len(Sensors))
	}
}

func TestUnknownDataType(t *testing.T) {
	sensor := Sensor{
		CollectionCapacity: 1000,
		Settings: map[string]settings{
			"banana": {SamplingFrequency: 10, SamplingDuration: 1},
		},
	}
	err := isExceedingCollectionCapacity(&sensor, "sampling_frequency", 20, "banana")
	if err == nil {
		t.Error("expected an error for an unknown data type")
	}
}
