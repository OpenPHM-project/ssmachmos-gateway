package server

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
	"tinygo.org/x/bluetooth"
)

// A fake MachMo sensor, so the whole data path can be tested without hardware.
//
// fakeSensorRig sets up everything around handleData():
//   - a sensor registered in model.Sensors (savePacket() crashes if it is missing),
//   - a fake OpenPHM server that records every upload,
//   - temporary config, cache and temp folders, so nothing real is touched.
//
// sendRecording() then plays a complete recording the way a real sensor does:
// first a header packet (8 bytes: total size, then sampling rate, both little
// endian), then the data in small Bluetooth-sized packets.

// Bytes of measurement data per Bluetooth packet. Real sensors send about 20 to
// 240 bytes at a time; 20 does not line up with 2, 3 or 6 byte samples, so
// samples are split across packets like on real hardware.
const fakeSensorPacketSize = 20

type fakeSensorRig struct {
	t       *testing.T
	mac     [6]byte
	uploads chan model.RequestBody // what the fake OpenPHM server received
}

func newFakeSensorRig(t *testing.T, boardModel string, sensorTypes ...string) *fakeSensorRig {
	t.Helper()

	// Everything handleData() writes goes to throwaway folders.
	configDir, cacheDir, tmpDir := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir) // sensors.json, sensor_history.json
	t.Setenv("XDG_CACHE_HOME", cacheDir)   // sent_data, unsent_data
	t.Setenv("TMPDIR", tmpDir)             // debug copies of the raw bytes

	rig := &fakeSensorRig{
		t:       t,
		mac:     [6]byte{0xAA, 0xBB, 0xCC, 0x00, 0x00, 0x01},
		uploads: make(chan model.RequestBody, 16),
	}

	// Fake OpenPHM server: remembers each upload and answers 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body model.RequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rig.uploads <- body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	oldGateway := Gateway
	Gateway = &model.Gateway{Id: "gw-fake", Password: "pw-fake", HTTPEndpoint: srv.URL}
	t.Cleanup(func() { Gateway = oldGateway })

	// The fake sensor, registered like an approved real one.
	oldSensors := model.Sensors
	model.Sensors = []model.Sensor{}
	t.Cleanup(func() { model.Sensors = oldSensors })
	if err := model.AddSensor(rig.mac, sensorTypes, 100000); err != nil {
		t.Fatalf("AddSensor: %v", err)
	}
	model.Sensors[0].Model = boardModel // "machmo" or "machmomini"

	t.Cleanup(func() {
		transmissionMutex.Lock()
		delete(transmissions, rig.mac)
		transmissionMutex.Unlock()
		delete(model.SensorHistory, model.MacToString(rig.mac))
	})
	return rig
}

// write is one Bluetooth write from the sensor, exactly like the real callback.
func (r *fakeSensorRig) write(from [6]byte, dataType string, value []byte) {
	var connection bluetooth.Connection // not used by handleData()
	handleData(dataType, connection, model.MacToString(from), 247, value)
}

// sendRecording sends one full recording: the header, then the data in packets.
// handleData() uploads synchronously on the last packet, so nothing may reach
// the server before that.
func (r *fakeSensorRig) sendRecording(dataType string, samplingRate uint32, recording []byte) {
	r.t.Helper()

	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(recording)))
	binary.LittleEndian.PutUint32(header[4:8], samplingRate)
	r.write(r.mac, dataType, header)
	r.expectNoUpload("after the header")

	for start := 0; start < len(recording); start += fakeSensorPacketSize {
		end := min(start+fakeSensorPacketSize, len(recording))
		r.write(r.mac, dataType, recording[start:end])
		if end < len(recording) {
			r.expectNoUpload(fmt.Sprintf("after %d of %d bytes", end, len(recording)))
		}
	}
}

func (r *fakeSensorRig) expectNoUpload(when string) {
	r.t.Helper()
	select {
	case body := <-r.uploads:
		r.t.Fatalf("an upload reached the server %s, before the recording was complete: %+v", when, body)
	default:
	}
}

// expectUpload returns the records that the gateway uploaded for a finished
// recording, after checking what every upload must contain.
func (r *fakeSensorRig) expectUpload(dataType string, wantRecords int) []map[string]any {
	r.t.Helper()

	var body model.RequestBody
	select {
	case body = <-r.uploads:
	case <-time.After(2 * time.Second):
		r.t.Fatal("no upload reached the fake server")
	}

	if body.GatewayId != "gw-fake" || body.GatewayPassword != "pw-fake" {
		r.t.Errorf("upload credentials = %q/%q, want gw-fake/pw-fake", body.GatewayId, body.GatewayPassword)
	}
	if len(body.Measurements) != wantRecords {
		r.t.Fatalf("upload holds %d records, want %d: %+v", len(body.Measurements), wantRecords, body.Measurements)
	}
	for i, record := range body.Measurements {
		if record["sensor_id"] != model.MacToString(r.mac) {
			r.t.Errorf("record %d sensor_id = %v, want %s", i, record["sensor_id"], model.MacToString(r.mac))
		}
		if record["measurement_type"] != dataType {
			r.t.Errorf("record %d measurement_type = %v, want %s", i, record["measurement_type"], dataType)
		}
	}

	// Exactly one upload per recording.
	r.expectNoUpload("as a second upload")
	return body.Measurements
}

// fakeSensorFloats turns the raw_data of an uploaded record into numbers.
func fakeSensorFloats(t *testing.T, value any) []float64 {
	t.Helper()
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("raw_data has type %T, want a list", value)
	}
	numbers := make([]float64, len(list))
	for i, item := range list {
		number, ok := item.(float64)
		if !ok {
			t.Fatalf("raw_data[%d] has type %T, want a number", i, item)
		}
		numbers[i] = number
	}
	return numbers
}

func fakeSensorExpectClose(t *testing.T, what string, got, want []float64, tolerance float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s has %d values, want %d", what, len(got), len(want))
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > tolerance {
			t.Fatalf("%s[%d] = %v, want %v (tolerance %g)", what, i, got[i], want[i], tolerance)
		}
	}
}

func fakeSensorInt16Bytes(values ...int16) []byte {
	var bytes []byte
	for _, v := range values {
		bytes = binary.LittleEndian.AppendUint16(bytes, uint16(v))
	}
	return bytes
}

// --- The four data types, each as a full recording through handleData() ---

// Vibration: 6 bytes per sample (x, y, z as little-endian int16), scaled for the 8 g range.
// One upload with three records, one per axis.
func TestFakeSensor_VibrationRecording(t *testing.T) {
	const samples = 100 // 600 bytes, sent in 30 packets of 20 bytes
	const samplingRate = 3200
	const scale = 0.000244 // 8 g range

	rig := newFakeSensorRig(t, "machmo", "vibration")

	var recording []byte
	wantX, wantY, wantZ := make([]float64, samples), make([]float64, samples), make([]float64, samples)
	for i := 0; i < samples; i++ {
		x, y, z := int16(i*50), int16(-i*30), int16((i%7)*1000-3000)
		recording = append(recording, fakeSensorInt16Bytes(x, y, z)...)
		wantX[i], wantY[i], wantZ[i] = float64(x)*scale, float64(y)*scale, float64(z)*scale
	}

	rig.sendRecording("vibration", samplingRate, recording)
	records := rig.expectUpload("vibration", 3)

	for i, axis := range []struct {
		name string
		want []float64
	}{{"x", wantX}, {"y", wantY}, {"z", wantZ}} {
		if records[i]["axis"] != axis.name {
			t.Errorf("record %d axis = %v, want %s", i, records[i]["axis"], axis.name)
		}
		if records[i]["sampling_frequency"] != float64(samplingRate) {
			t.Errorf("axis %s sampling_frequency = %v, want %d", axis.name, records[i]["sampling_frequency"], samplingRate)
		}
		fakeSensorExpectClose(t, "axis "+axis.name, fakeSensorFloats(t, records[i]["raw_data"]), axis.want, 1e-9)
	}
}

// Temperature: one 2-byte reading. A "machmo" board gives a PT1000 ADC reading
// (21845 is about 1000 ohm, so about 0 C); a "machmomini" counts 0.0625 C per step.
func TestFakeSensor_TemperatureRecording(t *testing.T) {
	tests := []struct {
		boardModel  string
		raw         int16
		wantCelsius float64
		tolerance   float64
	}{
		{"machmo", 21845, 0, 0.01},
		{"machmo", 23971, 25, 0.01},
		{"machmomini", 400, 25, 1e-9},
		{"machmomini", -160, -10, 1e-9},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s raw %d", tt.boardModel, tt.raw), func(t *testing.T) {
			rig := newFakeSensorRig(t, tt.boardModel, "temperature")

			rig.sendRecording("temperature", 1, fakeSensorInt16Bytes(tt.raw))
			records := rig.expectUpload("temperature", 1)

			got := fakeSensorFloats(t, records[0]["raw_data"])
			fakeSensorExpectClose(t, "temperature", got, []float64{tt.wantCelsius}, tt.tolerance)
			if records[0]["sampling_frequency"] != float64(1) {
				t.Errorf("sampling_frequency = %v, want 1", records[0]["sampling_frequency"])
			}
		})
	}
}

// Audio: 3 bytes per sample, first byte most significant. The leading zero
// samples are trimmed, and the 0.5 % clock correction is applied to the
// sampling rate.
func TestFakeSensor_AudioRecording(t *testing.T) {
	const samples = 300 // 900 bytes, sent in 45 packets of 20 bytes
	const samplingRate = 22110

	rig := newFakeSensorRig(t, "machmo", "audio")
	setting := model.Sensors[0].Settings["audio"]
	setting.ClockCorrectionPercent = 0.5
	model.Sensors[0].Settings["audio"] = setting

	var recording []byte
	var want []float64
	for i := 0; i < samples; i++ {
		amplitude := 0 // the first two samples are silence
		if i >= 2 {
			amplitude = i * 40000 // largest is 11,960,000, which fits in 24 bits
		}
		recording = append(recording, byte(amplitude>>16), byte(amplitude>>8), byte(amplitude))
		if i >= 2 {
			want = append(want, float64(amplitude))
		}
	}

	rig.sendRecording("audio", samplingRate, recording)
	records := rig.expectUpload("audio", 1)

	fakeSensorExpectClose(t, "audio", fakeSensorFloats(t, records[0]["raw_data"]), want, 0)
	wantRate := float64(samplingRate) * 1.005
	if got, ok := records[0]["sampling_frequency"].(float64); !ok || math.Abs(got-wantRate) > 1e-6 {
		t.Errorf("sampling_frequency = %v, want %v (22110 Hz with 0.5 %% clock correction)", records[0]["sampling_frequency"], wantRate)
	}
}

// Flux: 2 bytes per sample (little-endian int16), converted to millitesla.
// One count is exactly 1/78080 mT (see parseFluxData).
func TestFakeSensor_FluxRecording(t *testing.T) {
	const samplingRate = 1000

	rig := newFakeSensorRig(t, "machmo", "flux")

	counts := []int16{0, 1, -1, 1000, -1000, 16384, 32767, -32768}
	for i := 0; i < 40; i++ { // a longer recording, sent in 4 packets
		counts = append(counts, int16(i*700-14000))
	}
	want := make([]float64, len(counts))
	for i, c := range counts {
		want[i] = float64(c) / 78080.0
	}

	rig.sendRecording("flux", samplingRate, fakeSensorInt16Bytes(counts...))
	records := rig.expectUpload("flux", 1)

	fakeSensorExpectClose(t, "flux", fakeSensorFloats(t, records[0]["raw_data"]), want, 1e-9)
	if records[0]["sampling_frequency"] != float64(samplingRate) {
		t.Errorf("sampling_frequency = %v, want %d", records[0]["sampling_frequency"], samplingRate)
	}
}

// Data from a sensor that is not registered must be ignored, not crash
// (savePacket() would crash looking up its model) and not upload anything.
func TestFakeSensor_UnregisteredSensorIsIgnored(t *testing.T) {
	rig := newFakeSensorRig(t, "machmo", "vibration")
	stranger := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}

	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], 6)
	binary.LittleEndian.PutUint32(header[4:8], 3200)
	rig.write(stranger, "vibration", header)
	rig.write(stranger, "vibration", fakeSensorInt16Bytes(1, 2, 3))

	rig.expectNoUpload("from an unregistered sensor")
	transmissionMutex.RLock()
	_, started := transmissions[stranger]
	transmissionMutex.RUnlock()
	if started {
		t.Error("a transmission was started for an unregistered sensor")
	}
}