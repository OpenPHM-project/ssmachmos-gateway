package server

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

func TestSavePacket_HeaderThenDataPackets(t *testing.T){
	const (
		samplingFrequency = uint32(1000)
		totalLength       = uint32(100)
		dataType          = "vibration"
		sensorModel       = "machmo"
	)
	mac := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}

	// savePacket looks up the sensor's model, so register a fake sensor.
	oldSensors := model.Sensors
	model.Sensors = []model.Sensor{{Mac: mac, Name: "fake", Model: sensorModel}}
	t.Cleanup(func() {
		model.Sensors = oldSensors
		transmissionMutex.Lock()
		delete(transmissions, mac)
		transmissionMutex.Unlock()
	})

	// Header packet: 8 bytes = total size + sampling rate, little endian.
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], totalLength)
	binary.LittleEndian.PutUint32(header[4:8], samplingFrequency)

	// Data packets that add up to exactly totalLength bytes.
	payload := make([]byte, totalLength)
	for i := range payload {
		payload[i] = byte(i)
	}
	chunks := [][]byte{payload[:30], payload[30:65], payload[65:]}

	if _, ok := savePacket(header, mac, dataType); ok {
		t.Fatalf("ok = true after header only, want false")
	}

	var (
		got Transmission
		ok  bool
	)
	for i, c := range chunks {
		got, ok = savePacket(c, mac, dataType)
		if i < len(chunks)-1 && ok {
			t.Fatalf("ok = true after data packet %d of %d, want false", i+1, len(chunks))
		}
	}

	if !ok {
		t.Fatalf("last savePacket call: ok = false, want true")
	}
	if !bytes.Equal(got.packets, payload) {
		t.Fatalf("last savePacket call returned wrong data\n got: %v\nwant: %v", got.packets, payload)
	}
	if got.totalLength != totalLength {
		t.Errorf("totalLength = %d, want %d", got.totalLength, totalLength)
	}
	if got.samplingFrequency != samplingFrequency {
		t.Errorf("samplingFrequency = %d, want %d", got.samplingFrequency, samplingFrequency)
	}
	if got.sensorModel != sensorModel {
		t.Errorf("sensorModel = %q, want %q", got.sensorModel, sensorModel)
	}
}
	func TestSavePacket_PartialUploadReturnsNothing(t *testing.T) {
		const (
			samplingFrequency = uint32(1000)
			totalLength       = uint32(100)
			dataType          = "vibration"
		)
		mac := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0x01}
	 
		// savePacket looks up the sensor's model, so register a fake sensor.
		oldSensors := model.Sensors
		model.Sensors = []model.Sensor{{Mac: mac, Name: "fake", Model: "machmo"}}
		t.Cleanup(func() {
			model.Sensors = oldSensors
			transmissionMutex.Lock()
			delete(transmissions, mac)
			transmissionMutex.Unlock()
		})
	 
		// Header announces 100 bytes...
		header := make([]byte, 8)
		binary.LittleEndian.PutUint32(header[0:4], totalLength)
		binary.LittleEndian.PutUint32(header[4:8], samplingFrequency)
	 
		if _, ok := savePacket(header, mac, dataType); ok {
			t.Fatalf("ok = true after header only, want false")
		}
	 
		// ...but only 40 bytes of data arrive (in two packets), so it is incomplete.
		partial := make([]byte, 40)
		for i := range partial {
			partial[i] = byte(i)
		}
		for i, c := range [][]byte{partial[:25], partial[25:]} {
			got, ok := savePacket(c, mac, dataType)
			if ok {
				t.Fatalf("ok = true after partial data packet %d, want false", i+1)
			}
			if len(got.packets) != 0 {
				t.Fatalf("partial upload returned %d bytes of data, want none", len(got.packets))
			}
		}
	 
		// The partial data should still be held, waiting for the rest.
		transmissionMutex.RLock()
		pending, exists := transmissions[mac]
		transmissionMutex.RUnlock()
		if !exists {
			t.Fatalf("pending transmission was dropped, want it kept for the rest of the data")
		}
		if !bytes.Equal(pending.packets, partial) {
			t.Errorf("pending data = %v, want %v", pending.packets, partial)
		}
	}

