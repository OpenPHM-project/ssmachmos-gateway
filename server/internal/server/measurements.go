// Handles measurement uploads and local storage, including saving failed uploads
// for retry, archiving successful uploads, and storing raw data for debugging.
// Also converts raw MachMo temperature readings into degrees Celsius.
package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
	"github.com/jukuly/ss_machmos/server/internal/out"
)

// Stores the timestamp supplied when measurements are recorded as unsent.
// This timestamp is not updated after each retry.
type UnsentDataError struct {
	//Reason              string    `json:"reason"`
	LastAttemptedUpload time.Time `json:"last_attempted_upload"`
}

// Tracks unsent upload records in memory for reporting to the client.
// Records are not removed from this slice after successful retries.
var unsentData []UnsentDataError = []UnsentDataError{}

// The leading 0 means these permission values are written in octal.
// Directory execute permission allows traversal into the directory.
const dirPermCode os.FileMode = 0775  // rwxrwxr-x
const filePermCode os.FileMode = 0644 // rw-r--r--

// Returns the directory path for storing application data, creating it if needed.
func dataDir() string {
	// Get the user's cache directory, falling back to the temporary directory
	// if the cache directory cannot be determined.
	userDir, err := os.UserCacheDir()
	if err != nil {
		out.Logger.Println(err.Error())
		out.Logger.Println("defaulting to tmp directory")
		userDir = os.TempDir()
	}

	// Typically ~/.cache/ss_machmos/ on Linux, or /tmp/ss_machmos/ as a fallback.
	dir := path.Join(userDir, "/ss_machmos/")

	// Create the directory and any missing parent directories.
	err = os.MkdirAll(dir, dirPermCode)
	if err != nil {
		out.Logger.Panic(err.Error())
	}
	return dir
}

// Creates the unsent_data directory if needed and returns its path.
// This directory holds measurement files waiting to be uploaded.
func unsentDataDir() string {
	dir := path.Join(dataDir(), "/unsent_data/")
	err := os.MkdirAll(dir, dirPermCode)
	if err != nil {
		out.Logger.Panic(err.Error())
	}
	return dir
}

// Creates the sent_data directory if needed and returns its path.
// This directory holds archived copies of successfully uploaded measurements.
func archivedDataDir() string {
	dir := path.Join(dataDir(), "/sent_data/")
	err := os.MkdirAll(dir, dirPermCode)
	if err != nil {
		out.Logger.Panic(err.Error())
	}
	return dir
}

// Creates the debug_data directory under the temporary directory if needed
// and returns its path.
func debugDataDir() string {
	dir := path.Join(os.TempDir(), "/ss_machmos/debug_data/")
	err := os.MkdirAll(dir, dirPermCode)
	if err != nil {
		out.Logger.Panic(err.Error())
	}
	return dir
}

// FIXME: this should be the only entry point to gateway uploading. Server
// should NOT handle saving unsent measurements.

// Uploads measurements with the gateway credentials to the configured HTTP endpoint.
// Archives measurements after a successful upload and broadcasts upload or
// authentication status.
// Returns the HTTP response and any JSON decoding, encoding, or HTTP request error.
// Archive errors are logged but are not returned to the caller.
func sendMeasurements(jsonData []byte, gateway *model.Gateway) (*http.Response, error) {
	//gateway.AuthError = false

	// Start the request body with the gateway credentials.
	body := model.RequestBody{
		GatewayId:       gateway.Id,
		GatewayPassword: gateway.Password,
	}

	// Decode the supplied measurement JSON into the request body's Measurements field.
	err := json.Unmarshal(jsonData, &body.Measurements)
	if err != nil {
		return nil, err
	}

	// Encode the complete request body, including credentials and measurements.
	json, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	// Send the JSON request body to the gateway's configured HTTP endpoint.
	resp, err := http.Post(gateway.HTTPEndpoint, "application/json", bytes.NewBuffer([]byte(json)))

	// After a successful upload, archive the measurements and notify the client.
	if err == nil && resp.StatusCode == http.StatusOK {
		err := archiveMeasurements(jsonData, time.Now())
		if err != nil {
			out.Logger.Println(err.Error())
		}

		// The upload succeeded even if saving the archived copy failed.
		out.Broadcast("UPLOAD-SUCCESS")
	}

	// TODO: Check for a request error or nil response before accessing StatusCode.
	// A failed HTTP request can leave resp nil, causing this check to panic.

	// Notify the client if the endpoint rejects the gateway credentials.
	if resp.StatusCode == http.StatusUnauthorized {
		out.Broadcast("GATEWAY-INVALID")
	}

	return resp, err
}

// Records an unsent upload, notifies the GUI, and saves the measurement JSON
// for a later retry. Uses the supplied timestamp to name the file.
// The in-memory record and notification occur before the file write is attempted.
func saveUnsentMeasurements(data []byte, timestamp time.Time) error {
	unsentData = append(unsentData, UnsentDataError{
		//Reason: "",
		LastAttemptedUpload: timestamp,
	})

	// Notify the GUI that measurements could not be uploaded.
	out.Broadcast("UPLOAD-FAILED")
	return os.WriteFile(path.Join(unsentDataDir(), timestamp.String()+".json"), data, filePermCode)
}

// Saves an archived copy of the measurements in sent_data,
// using the supplied timestamp to name the JSON file.
func archiveMeasurements(data []byte, timestamp time.Time) error {
	return os.WriteFile(path.Join(archivedDataDir(), timestamp.String()+".json"), data, filePermCode)
}

// Saves a transmission's raw bytes to a binary file for debugging.
// The filename includes the sensor model, data type, and current time.
func saveDebugMeasurements(transmission Transmission) error {
	err := os.WriteFile(path.Join(debugDataDir(), transmission.sensorModel+"_"+transmission.dataType+"_"+time.Now().String()+".bin"), transmission.packets, filePermCode)
	if err != nil {
		out.Logger.Println(err)
	}
	return err
}

// Returns the in-memory records of uploads saved as unsent.
// These records contain timestamps rather than measurement data and are not
// removed after successful retries or reconstructed from disk here.
func PendingUploads() []UnsentDataError {
	return unsentData
}

// Attempts to upload measurement files from unsent_data.
// After a successful upload, attempts to move the original file into sent_data.
// Failed uploads remain in unsent_data for future retries.
func sendUnsentMeasurements() {
	// List the entries in the unsent_data directory.
	files, err := os.ReadDir(unsentDataDir())
	if err != nil {
		out.Logger.Println("Error:", err)
		return
	}

	// Read and attempt to upload each saved measurement file.
	for _, file := range files {
		data, err := os.ReadFile(path.Join(unsentDataDir(), file.Name()))
		if err != nil {
			out.Logger.Println("Error:", err)
			continue
		}

		resp, err := sendMeasurements(data, Gateway)
		if err != nil {
			out.Logger.Println("Error:", err)
			continue
		}

		// After HTTP 200 OK, move the original file to sent_data for debugging.
		// sendMeasurements also saves a separate archived copy with a new timestamp.
		// The result of this move is currently not checked.
		if resp.StatusCode == 200 {
			os.Rename(path.Join(unsentDataDir(), file.Name()), path.Join(archivedDataDir(), file.Name()))
		}
	}
}

// Converts a raw MachMo temperature reading into degrees Celsius.
// First converts the ADC reading into resistance, then applies the appropriate
// temperature conversion equation.
func parseTemperatureData(data int16) (float64, error) {
	// ADC full-scale value and resistance values used in the conversion.
	adc_fs := math.Pow(2, 15) - 1.0
	const r_ref = 1500.0
	const r_0 = 1000.0

	// Convert the raw ADC reading into the temperature sensor's resistance.
	adc_in := float64(data)
	rtd_resistance := adc_in / adc_fs * r_ref

	if rtd_resistance >= 1000 {
		const A = 3.9083e-3
		const B = -5.775e-7

		// Solve the Callendar-Van Dusen equation for resistance at or above 1000 ohms.
		sqrt := math.Sqrt(math.Pow(A, 2) - 4*B*(1-rtd_resistance/r_0))

		// TODO: Check the expression before taking its square root, or check for NaN.
		// A negative square-root input produces NaN, which this comparison does not catch.
		if sqrt < 0 {
			return 0, errors.New("negative square root")
		}
		return (-A + sqrt) / (2 * B), nil
	} else {
		// Use a quadratic approximation for resistance below 1000 ohms.
		const A = -0.00061414
		const B = 3.907359803
		const C = 999.9979

		sqrt := math.Sqrt(math.Pow(B, 2) - 4*A*(C-rtd_resistance))

		// TODO: This comparison also does not detect a NaN square-root result.
		if sqrt < 0 {
			return 0, errors.New("negative square root")
		}
		return (-B + sqrt) / (2 * A), nil
	}
}