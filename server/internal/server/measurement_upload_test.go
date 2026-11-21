package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

func TestSendMeasurementsLargeUpload(t *testing.T) {
	// Successful uploads are archived. Keep that archive in the test directory.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	// Each five-digit reading takes about 6 bytes in JSON, including its comma.
	const sampleCount = 250_000
	const targetBytes = 1_500_000

	readings := make([]int, sampleCount)
	for i := range readings {
		readings[i] = 10_000 + i%10_000
	}

	measurements := []map[string]any{{
		"sensor_id":          "AA:BB:CC:DD:EE:FF",
		"time":               "2026-11-21T12:00:00Z",
		"measurement_type":   "audio",
		"sampling_frequency": 50_000,
		"raw_data":           readings,
	}}

	jsonData, err := json.Marshal(measurements)
	if err != nil {
		t.Fatal(err)
	}

	// Capture what the server actually receives, including the gateway wrapper.
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "expected POST", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "could not read upload", http.StatusBadRequest)
			return
		}

		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := &model.Gateway{
		Id:           "test-gateway",
		Password:     "test-password",
		HTTPEndpoint: server.URL,
	}

	start := time.Now()
	resp, err := sendMeasurements(jsonData, gateway)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("sendMeasurements failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got HTTP %d, want 200", resp.StatusCode)
	}

	body := <-received
	if len(body) < targetBytes {
		t.Fatalf("server received %d bytes, want at least %d", len(body), targetBytes)
	}

	var upload struct {
		Measurements []struct {
			RawData []int `json:"raw_data"`
		} `json:"measurements"`
	}
	if err := json.Unmarshal(body, &upload); err != nil {
		t.Fatalf("server received invalid upload JSON: %v", err)
	}
	if len(upload.Measurements) != 1 {
		t.Fatalf("got %d measurements, want 1", len(upload.Measurements))
	}
	if !slices.Equal(upload.Measurements[0].RawData, readings) {
		t.Fatal("received readings differ from the original readings")
	}

	t.Logf("server received %d bytes; sendMeasurements took %v", len(body), elapsed)
}

func TestSendMeasurementsTimeout(t *testing.T) {
	// Use a short deadline for this test and restore the gateway client afterward.
	originalClient := httpClient
	httpClient = &http.Client{Timeout: 100 * time.Millisecond}
	t.Cleanup(func() { httpClient = originalClient })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "could not read upload", http.StatusBadRequest)
			return
		}

		// Delay the response headers. Exit early when the client times out.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	gateway := &model.Gateway{
		Id:           "test-gateway",
		Password:     "test-password",
		HTTPEndpoint: server.URL,
	}
	jsonData := []byte(`[{"measurement_type":"temperature","raw_data":[25]}]`)

	resp, err := sendMeasurements(jsonData, gateway)
	if resp != nil {
		defer resp.Body.Close()
		t.Fatalf("expected a timeout before response headers, got HTTP %d", resp.StatusCode)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a deadline exceeded error, got %v", err)
	}
}
