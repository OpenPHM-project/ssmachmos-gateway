package server

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

// TestSendMeasurementsLargeUpload sends about 1.5 MB of measurement JSON to a local server, verifies that all 250,000 readings arrive in order, and logs how long sendMeasurements takes.
func TestSendMeasurementsLargeUpload(t *testing.T) {
	// Successful uploads are archived. Keep that archive in the test directory.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	// Each five-digit reading takes about 6 bytes in JSON, including its comma.
	const sampleCount = 250_000
	const targetBytes = 1_500_000

	// Generate a large slice of readings to test the upload function.
	// The readings range from 10,000 to 19,999, repeating every 10,000 readings.
	readings := make([]int, sampleCount)
	for i := range readings {
		readings[i] = 10_000 + i%10_000
	}

	// Create a single measurement record with the large slice of readings as its raw_data field.
	measurements := []map[string]any{{
		"sensor_id":          "AA:BB:CC:DD:EE:FF",
		"time":               "2026-11-21T12:00:00Z",
		"measurement_type":   "audio",
		"sampling_frequency": 50_000,
		"raw_data":           readings,
	}}

	// Marshal the measurement record into JSON for sending to the gateway.
	jsonData, err := json.Marshal(measurements)
	if err != nil {
		t.Fatal(err)
	}

	// Capture what the server actually receives, including the gateway wrapper.
	received := make(chan []byte, 1) // 1 means holding one upload at a time.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The test server only accepts POST requests.
		if r.Method != http.MethodPost {
			http.Error(w, "expected POST", http.StatusMethodNotAllowed)
			return
		}

		// Read the request body, which may be large, and send it to the channel.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "could not read upload", http.StatusBadRequest)
			return
		}

		// Puts the bytes into received, a channel with a buffer of 1, so the test can read it later.
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create a test gateway with the test server's URL as its endpoint.
	gateway := &model.Gateway{
		Id:           "test-gateway",
		Password:     "test-password",
		HTTPEndpoint: server.URL,
	}

	// Measure how long the upload takes and check that the server received the expected data.
	start := time.Now()
	resp, err := sendMeasurements(jsonData, gateway)
	elapsed := time.Since(start) // Record the elapsed time for the upload.
	if err != nil {
		t.Fatalf("sendMeasurements failed: %v", err)
	}
	defer resp.Body.Close()

	// Check that the server responded with HTTP 200 OK.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got HTTP %d, want 200", resp.StatusCode)
	}

	// Check that the server received at least the expected number of bytes.
	body := <-received
	if len(body) < targetBytes {
		t.Fatalf("server received %d bytes, want at least %d", len(body), targetBytes)
	}

	// Unmarshal the received JSON into a struct to verify that the raw_data matches the original readings.
	var upload struct {
		Measurements []struct {
			RawData []int `json:"raw_data"`
		} `json:"measurements"`
	}
	if err := json.Unmarshal(body, &upload); err != nil {
		t.Fatalf("server received invalid upload JSON: %v", err)
	}
	// Check that the server received exactly one measurement and that its raw_data matches the original readings.
	if len(upload.Measurements) != 1 {
		t.Fatalf("got %d measurements, want 1", len(upload.Measurements))
	}

	// Check that the received readings match the original readings.
	if !slices.Equal(upload.Measurements[0].RawData, readings) {
		t.Fatal("received readings differ from the original readings")
	}

	// Log the number of bytes received and the time taken for the upload.
	t.Logf("server received %d bytes; sendMeasurements took %v", len(body), elapsed)
}

// Tells server to wait 1 second before replying, sets the gateway's timeout to 100ms, and verifies that sendMeasurements returns a context deadline exceeded error.
func TestSendMeasurementsTimeout(t *testing.T) {
	// Use a short deadline for this test and restore the gateway client afterward.
	originalClient := httpClient
	httpClient = &http.Client{Timeout: 100 * time.Millisecond} // Give the test client a 100ms timeout to trigger a deadline exceeded error.
	t.Cleanup(func() { httpClient = originalClient })          // Restore the original client after the test.

	// Create a test server that delays its response to simulate a slow endpoint.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to avoid leaving it unread, which can cause issues with the HTTP client.
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "could not read upload", http.StatusBadRequest)
			return
		}

		// Delay the response headers. Exit early when the client times out.
		select {
		// If the request context is done (e.g., due to timeout), return without writing a response.
		case <-r.Context().Done():
			return
		// If the request context is not done, wait for 1 second before sending a response.
		case <-time.After(time.Second):
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	// Create a test gateway with the test server's URL as its endpoint.
	gateway := &model.Gateway{
		Id:           "test-gateway",
		Password:     "test-password",
		HTTPEndpoint: server.URL,
	}
	// Prepare a small JSON payload to pass to sendMeasurements().
	jsonData := []byte(`[{"measurement_type":"temperature","raw_data":[25]}]`)

	// Call sendMeasurements with the small JSON payload and the test gateway.
	resp, err := sendMeasurements(jsonData, gateway)
	if resp != nil {
		// If the response is not nil, close the body to avoid resource leaks.
		defer resp.Body.Close()
		t.Fatalf("expected a timeout before response headers, got HTTP %d", resp.StatusCode)
	}
	// Check that the error returned is a context deadline exceeded error, indicating that the request timed out as expected.
	// If the error is not a deadline exceeded error, the test fails with a fatal error message.
	var timeoutErr net.Error
	if !errors.As(err, &timeoutErr) || !timeoutErr.Timeout() {
		t.Fatalf("expected an HTTP timeout, got %v", err)
	}
}
