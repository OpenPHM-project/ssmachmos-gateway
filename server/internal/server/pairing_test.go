package server

import (
	"slices"
	"testing"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

// withSensors replaces the global sensor list for one test and restores it after.
func withSensors(t *testing.T, sensors ...model.Sensor) {
	t.Helper()
	old := model.Sensors
	model.Sensors = sensors
	t.Cleanup(func() { model.Sensors = old })
}

func TestSensorExists_ExactMatch(t *testing.T) {
	mac := [6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}
	withSensors(t, model.Sensor{Mac: mac, Name: "known", Model: "machmo"})

	got := sensorExists(mac)
	if got == nil {
		t.Fatalf("sensorExists(%X) = nil, want the registered sensor", mac)
	}
	if got.Name != "known" || got.Model != "machmo" {
		t.Errorf("got sensor %q (%q), want %q (%q)", got.Name, got.Model, "known", "machmo")
	}
}

func TestSensorExists_NoSensors(t *testing.T) {
	withSensors(t)

	if got := sensorExists([6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}); got != nil {
		t.Fatalf("sensorExists with no sensors = %+v, want nil", got)
	}
}

// Two different MACs that share the first byte must NOT match.
func TestSensorExists_SameFirstByteDoesNotMatch(t *testing.T) {
	known := [6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}
	other := [6]byte{0xAA, 0x99, 0x88, 0x77, 0x66, 0x55} // same first byte, different sensor
	withSensors(t, model.Sensor{Mac: known, Name: "known"})

	if got := sensorExists(other); got != nil {
		t.Fatalf("sensorExists(%X) matched sensor %X, want nil (only the first byte is shared)", other, got.Mac)
	}
}

// A MAC that differs from a registered one in any single byte must not match.
func TestSensorExists_AnySingleByteDifferenceDoesNotMatch(t *testing.T) {
	known := [6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}
	withSensors(t, model.Sensor{Mac: known, Name: "known"})

	for i := range known {
		other := known
		other[i] ^= 0xFF
		if got := sensorExists(other); got != nil {
			t.Errorf("MAC differing at byte %d (%X) matched sensor %X, want nil", i, other, got.Mac)
		}
	}
}

// With two sensors sharing a first byte, looking up the second must return the
// second one, not the first one in the list.
func TestSensorExists_ReturnsTheRightSensorAmongSimilarMACs(t *testing.T) {
	first := [6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}
	second := [6]byte{0xAA, 0x99, 0x88, 0x77, 0x66, 0x55}
	withSensors(t,
		model.Sensor{Mac: first, Name: "first", Model: "machmo"},
		model.Sensor{Mac: second, Name: "second", Model: "machmomini"},
	)

	got := sensorExists(second)
	if got == nil {
		t.Fatalf("sensorExists(%X) = nil, want the second sensor", second)
	}
	if got.Name != "second" || got.Model != "machmomini" {
		t.Errorf("got sensor %q (%q), want %q (%q)", got.Name, got.Model, "second", "machmomini")
	}
}

// withPairingState gives a test a fresh pairing state (normally created by Init)
// and restores the old one afterwards.
func withPairingState(t *testing.T) {
	t.Helper()
	old := state
	state = pairingState{requested: make(map[[6]byte]request)}
	t.Cleanup(func() { state = old })
}

// Capability packet from the sensor (6 bytes):
// data types bitmask (1 byte) | collection capacity in bytes (uint32 LE) | sensor model (1 byte)
// Bitmask: audio = bit 0, temperature = bit 1, vibration = bit 2, flux = bit 3.
func TestPairReceiveCapabilities_ParsesTypesCapacityAndModel(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		wantTypes    []string
		wantCapacity uint32
		wantModel    string
	}{
		{
			name:         "audio and vibration on a machmo",
			data:         []byte{0b0101, 0x00, 0x10, 0x00, 0x00, 0x01},
			wantTypes:    []string{"audio", "vibration"},
			wantCapacity: 4096,
			wantModel:    "machmo",
		},
		{
			name:         "every type on a machmomini, capacity is little endian",
			data:         []byte{0b1111, 0x04, 0x03, 0x02, 0x01, 0x02},
			wantTypes:    []string{"audio", "flux", "temperature", "vibration"},
			wantCapacity: 0x01020304,
			wantModel:    "machmomini",
		},
		{
			name:         "temperature only",
			data:         []byte{0b0010, 0x01, 0x00, 0x00, 0x00, 0x01},
			wantTypes:    []string{"temperature"},
			wantCapacity: 1,
			wantModel:    "machmo",
		},
		{
			name:         "no types announced",
			data:         []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			wantTypes:    nil,
			wantCapacity: 0,
			wantModel:    "machmo",
		},
		{
			name:         "unknown model byte",
			data:         []byte{0b0001, 0x00, 0x01, 0x00, 0x00, 0x7F},
			wantTypes:    []string{"audio"},
			wantCapacity: 256,
			wantModel:    "unknown",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withSensors(t) // no paired sensors
			withPairingState(t)
			mac := [6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}

			if ok := pairReceiveCapabilities(mac, tc.data); !ok {
				t.Fatalf("pairReceiveCapabilities returned false, want true")
			}

			req, found := state.requested[mac]
			if !found {
				t.Fatalf("no pairing request stored for %X", mac)
			}
			if !req.announcedSensors {
				t.Errorf("announcedSensors = false, want true")
			}
			// Types come out of a map, so compare in sorted order.
			gotTypes := slices.Clone(req.dataTypes)
			slices.Sort(gotTypes)
			if !slices.Equal(gotTypes, tc.wantTypes) {
				t.Errorf("dataTypes = %v, want %v", gotTypes, tc.wantTypes)
			}
			if req.collectionCapacity != tc.wantCapacity {
				t.Errorf("collectionCapacity = %d, want %d", req.collectionCapacity, tc.wantCapacity)
			}
			if req.announcedModel != tc.wantModel {
				t.Errorf("announcedModel = %q, want %q", req.announcedModel, tc.wantModel)
			}
			// An announced sensor shows up as pending pairing.
			if pending := ListDevicesPendingPairing(); len(pending) != 1 || pending[0] != "AA:11:22:33:44:55" {
				t.Errorf("ListDevicesPendingPairing() = %v, want [AA:11:22:33:44:55]", pending)
			}
		})
	}
}

// Packets that are not exactly 6 bytes are rejected and the sensor is not announced.
func TestPairReceiveCapabilities_WrongLengthIsRejected(t *testing.T) {
	for _, n := range []int{1, 5, 7} {
		withSensors(t)
		withPairingState(t)
		mac := [6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}

		if ok := pairReceiveCapabilities(mac, make([]byte, n)); ok {
			t.Errorf("%d-byte packet: returned true, want false", n)
		}
		if req := state.requested[mac]; req.announcedSensors {
			t.Errorf("%d-byte packet: sensor was announced, want not announced", n)
		}
		if pending := ListDevicesPendingPairing(); len(pending) != 0 {
			t.Errorf("%d-byte packet: ListDevicesPendingPairing() = %v, want empty", n, pending)
		}
	}
}

// A sensor that is already paired is not put through pairing again.
func TestPairReceiveCapabilities_AlreadyPairedSensorIsIgnored(t *testing.T) {
	mac := [6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}
	withSensors(t, model.Sensor{Mac: mac, Name: "known"})
	withPairingState(t)

	if ok := pairReceiveCapabilities(mac, []byte{0b0101, 0x00, 0x10, 0x00, 0x00, 0x01}); !ok {
		t.Fatalf("returned false, want true")
	}
	if _, found := state.requested[mac]; found {
		t.Errorf("already paired sensor was added to the pairing requests")
	}
}

// A different sensor that only shares the first MAC byte with a paired one
// is a new sensor and must go through pairing (this was broken by the sensorExists bug).
func TestPairReceiveCapabilities_NewSensorSharingFirstByteIsPaired(t *testing.T) {
	known := [6]byte{0xAA, 0x11, 0x22, 0x33, 0x44, 0x55}
	newMac := [6]byte{0xAA, 0x99, 0x88, 0x77, 0x66, 0x55}
	withSensors(t, model.Sensor{Mac: known, Name: "known"})
	withPairingState(t)

	if ok := pairReceiveCapabilities(newMac, []byte{0b0001, 0x00, 0x10, 0x00, 0x00, 0x01}); !ok {
		t.Fatalf("returned false, want true")
	}
	if req, found := state.requested[newMac]; !found || !req.announcedSensors {
		t.Errorf("new sensor %X was not announced for pairing", newMac)
	}
}