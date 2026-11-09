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
