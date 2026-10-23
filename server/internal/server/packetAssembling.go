/*
Receives measurement data from sensors over Bluetooth Low Energy (BLE).

Combines incoming packets into complete transmissions, converts the raw
bytes into vibration, temperature, or audio measurements, and encodes
those measurements as JSON for upload.

Also marks idle transmissions as stale and handles separate debug
transmissions that are saved directly to binary files.
*/
package server

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
	"github.com/jukuly/ss_machmos/server/internal/out"
	"tinygo.org/x/bluetooth"
)

// Number of idle seconds before the watchdog marks a transmission as stale.
// The next packet from that device will then be treated as a new header.
const TRANSMISSION_TIMEOUT = 5 // Seconds

// Represents packet bytes and their offset.
// This type is not used by the assembly functions in this file.
type Packet struct {
	offset int
	data   []byte
}

// Stores the metadata and accumulated raw bytes for one sensor transmission.
type Transmission struct {
	macAddress        [6]byte   // FIXME make this a string - MAC address identifying the sending device.
	sensorModel       string    // Board model used to choose the conversion method.
	timestamp         time.Time // Time the server received the transmission header.
	endTimestamp      time.Time // Time the server considered the transmission complete.
	dataType          string    // Measurement type, such as vibration or temperature.
	samplingFrequency uint32    // Sampling frequency announced in the header.
	currentLength     int       // Number of measurement bytes received so far.
	totalLength       uint32    // Expected number of measurement bytes from the header.
	packets           []byte    // Accumulated measurement bytes, excluding the header.
	lastActivity      int64     // Time of the latest header or data packet, in Unix seconds.
	stale             bool      // Whether the transmission should be replaced on the next packet.
}

// Coordinates access to the shared transmission map.
// RLock allows readers; Lock provides exclusive access for changes.
// TODO: Some accesses below are not protected by this mutex.
var transmissionMutex sync.RWMutex 
		// https://go.dev/doc/faq#atomic_maps
		// TODO: See if another package like concurrent-map might help here

// Maps each device's MAC address to its current transmission.
// Only one transmission per device can be tracked at a time.
type TransmissionMap map[[6]byte]Transmission

// Stores transmissions that are still being assembled.
var transmissions TransmissionMap = make(TransmissionMap)

// Runs continuously in a background goroutine, checking for idle transmissions.
// Marks timed-out transmissions as stale so savePacket can replace them.
// Stale entries remain in memory until replaced or otherwise removed.
func startWatchdog() {
	for {
		// Check for idle transmissions approximately once per second.
		time.Sleep(1 * time.Second)
		now := time.Now().Unix()


		transmissionMutex.RLock()
		for mac, transmission := range transmissions {
			// Release the read lock before potentially acquiring a write lock.
			transmissionMutex.RUnlock()

			// TODO: Recheck the entry's activity after acquiring the write lock.
			// Another handler could update or remove it after this snapshot was read.
			if !transmission.stale && now-transmission.lastActivity >= TRANSMISSION_TIMEOUT {
				transmissionMutex.Lock()

				// Map values are structs, so update a local copy and store it back.
				t := transmissions[mac]
				t.stale = true
				transmissions[mac] = t

				transmissionMutex.Unlock()
				out.Logger.Printf("Idle timeout transmission for %s datatype %s", model.MacToString(mac), transmission.dataType)
			}

			// Reacquire the read lock before continuing the map iteration.
			transmissionMutex.RLock()
		}
		transmissionMutex.RUnlock()
	}
}

// Processes one packet for a device.
// For a new or stale transmission, interprets the packet as a header.
// Otherwise, appends the packet's bytes to the current transmission.
// Returns the assembled transmission and true once the expected byte count
// is reached or exceeded; otherwise returns an empty Transmission and false.
func savePacket(data []byte, macAddress [6]byte, dataType string) (t Transmission, ok bool) {
	// Look up any transmission already being assembled for this device.
	transmissionMutex.RLock()
	transmission, exists := transmissions[macAddress]
	transmissionMutex.RUnlock()

	if exists == false || transmission.stale == true {
		out.Logger.Println("COLLECT-START:" + model.MacToString(macAddress))

		// The header contains two little-endian 32-bit unsigned integers:
		// bytes 0–3: expected measurement byte count
		// bytes 4–7: sampling frequency
		// TODO: Verify that data contains at least 8 bytes before slicing it.
		totalLength := binary.LittleEndian.Uint32(data[0:4])
		samplingFrequency := binary.LittleEndian.Uint32(data[4:8])

		// Sensor-provided timestamp decoding is currently disabled.
		//timestamp := int64(binary.LittleEndian.Uint64(data[8:16]))

		transmissionMutex.Lock()

		// Start a fresh transmission with an empty measurement buffer.
		// The header itself is not appended to packets.
		// TODO: Check sensorExists(macAddress) for nil before accessing Model.
		transmissions[macAddress] = Transmission{
			macAddress:        macAddress,
			sensorModel:       sensorExists(macAddress).Model,
			timestamp:         time.Now(), // Uses server time rather than a sensor timestamp.
			dataType:          dataType,
			samplingFrequency: samplingFrequency,
			currentLength:     0,
			totalLength:       totalLength,
			packets:           make([]byte, 0),
			lastActivity:      time.Now().Unix(),
			stale:             false,
		}
		transmissionMutex.Unlock()

		// TODO: Protect these map reads with the transmission mutex.
		out.Logger.Println("Received collection header")
		out.Logger.Println("DataType:", transmissions[macAddress].dataType)
		out.Logger.Println("Total expected length:", transmissions[macAddress].totalLength)
	} else {
		transmissionMutex.Lock()

		// Subsequent packets contain measurement bytes.
		// Append them in arrival order, update the count, and reset the idle timer.
		transmission := transmissions[macAddress]
		transmission.packets = append(transmission.packets, data...)
		transmission.currentLength += len(data)
		transmission.lastActivity = time.Now().Unix()
		transmissions[macAddress] = transmission

		transmissionMutex.Unlock()
		out.Logger.Println("Received packet from", model.MacToString(macAddress), "total", transmission.currentLength, "/", transmission.totalLength)
	}

	// Consider the transmission complete once enough measurement bytes arrive.
	// Extra bytes are currently accepted and retained.
	// TODO: Protect this completion check and map deletion with the mutex.
	if transmissions[macAddress].currentLength >= int(transmissions[macAddress].totalLength) {
		fullTransmit := transmissions[macAddress]
		fullTransmit.endTimestamp = time.Now()

		//copy(fullTransmit.packets, transmissions[macAddress].packets)

		// Remove the completed transmission from the pending map.
		delete(transmissions, macAddress)
		out.Logger.Println("Assembled transmission packets for", model.MacToString(macAddress),
			"time taken", fullTransmit.endTimestamp.Sub(fullTransmit.timestamp))

		out.Logger.Println("COLLECT-END:" + model.MacToString(macAddress))
		return fullTransmit, true
	}

	// More packets are needed before the measurements can be processed.
	return Transmission{}, false
}

// Handles a write to a measurement characteristic for a registered sensor.
// Collects packets until a transmission is complete, converts its raw data,
// and attempts to upload the resulting JSON.
// The connection and integer argument are unused here.
func handleData(dataType string, _ bluetooth.Connection, address string, _mtu int, value []byte) {
	// Ignore empty writes.
	if len(value) == 0 {
		out.Logger.Println("Zero byte array received from " + address + " handling data for " + dataType)
		return
	}

	// Find the sending device in the saved sensor list.
	macAddress, _ := model.StringToMac(address)
	var sensor *model.Sensor = nil
	for i, s := range model.Sensors {
		if s.Mac == macAddress {
			// Keep a pointer to the actual sensor entry in the slice.
			sensor = &(model.Sensors[i])
			break
		}
	}

	// Reject data from devices that are not registered in the application.
	// This check does not itself perform Bluetooth security authentication.
	if sensor == nil {
		out.Logger.Println("Device " + address + " tried to send data, but it is not paired with this gateway")
		return
	}

	// Record that the sensor is currently sending data.
	sensor.UpdateLastSeen(model.SensorActivityTransmitting)

	// Process the header or append measurement bytes to the pending transmission.
	transmitData, ok := savePacket(value, macAddress, dataType)
	if !ok {
		// Wait for later calls to supply the remaining packets.
		return
	}

	// All expected bytes have arrived; mark the sensor as idle.
	out.Logger.Println("Received " + dataType + " data transmission from " + model.MacToString(macAddress) + " (" + sensor.Name + ")")
	sensor.UpdateLastSeen(model.SensorActivityIdle)

	// Save a raw binary copy for debugging before attempting conversion.
	// saveDebugMeasurements logs its own file-writing errors.
	saveDebugMeasurements(transmitData)

	// Convert the raw bytes into measurement records that can be encoded as JSON.
	// Each record maps a field name to a value, such as a timestamp or sample array.
	var measurements []map[string]interface{}
	if dataType == "vibration" {
		measurements = handleVibrationData(transmitData)
	} else if dataType == "temperature" {
		measurements = handleTemperatureData(transmitData)
	} else if dataType == "audio" {
		measurements = handleAudioData(transmitData)
	} else {
		// Other types, including flux, have no conversion branch here.
		out.Logger.Println("unknown data type", dataType)
		return
	}

	// Encode the converted measurement records as JSON.
	jsonData, err := json.Marshal(measurements)
	if err != nil {
		out.Logger.Println("Error:", err)
		return
	}

	// Upload using the configured gateway credentials and HTTP endpoint.
	resp, err := sendMeasurements(jsonData, Gateway)

	// If preparing or sending the request fails, save the JSON for a later retry.
	if err != nil {
		out.Logger.Println("Error:", err)
		if err := saveUnsentMeasurements(jsonData, transmitData.timestamp); err != nil {
			out.Logger.Println("Error:", err)
		}
		return
	}

	// TODO: Close the response body on both successful and unsuccessful responses.

	// Treat any HTTP status other than 200 as an unsuccessful upload.
	if resp.StatusCode != 200 {
		out.Logger.Println("Error sending data to server")

		// Read and log the response body to help diagnose the failure.
		// TODO: ContentLength can be -1, and one Read may not return the whole body.
		body := make([]byte, resp.ContentLength)
		defer resp.Body.Close()
		resp.Body.Read(body)
		out.Logger.Println(string(body))

		// Preserve the measurement JSON for a future upload attempt.
		if err := saveUnsentMeasurements(jsonData, transmitData.timestamp); err != nil {
			out.Logger.Println(err)
		}
		return
	}

	// After this upload succeeds, retry any measurements already saved on disk.
	sendUnsentMeasurements()
}

// Converts raw vibration samples into three measurement records, one per axis.
// Each sample contains six bytes: two for X, two for Y, and two for Z.
func handleVibrationData(transmitData Transmission) []map[string]interface{} {
	// Conversion factor used for the accelerometer's 8G range.
	convRange8G := .000244
	rawData := transmitData.packets
	numberOfMeasurements := len(rawData) / 6

	out.Logger.Println("Vibration data consists of", len(rawData), "bytes =", numberOfMeasurements, "measurements.")

	// Allocate a separate sample array for each axis.
	x, y, z := make([]float64, numberOfMeasurements), make([]float64, numberOfMeasurements), make([]float64, numberOfMeasurements)

	// TODO: Reject data whose length is not a multiple of six before this loop.
	// An incomplete final sample would cause an out-of-range access.
	for i := 0; i < len(rawData); i += 6 {
		//out.Logger.Println(rawData[i:i+6])

		// Combine each little-endian byte pair into a signed 16-bit reading,
		// then convert it to float64 and apply the scaling factor.
		// i/6 gives the sample's index in the output arrays.
		x[i/6] = float64(int16(transmitData.packets[i+1])<<8|int16(transmitData.packets[i+0])) * convRange8G
		y[i/6] = float64(int16(transmitData.packets[i+3])<<8|int16(transmitData.packets[i+2])) * convRange8G
		z[i/6] = float64(int16(transmitData.packets[i+5])<<8|int16(transmitData.packets[i+4])) * convRange8G
	}

	// Build one record per axis with the same sensor and transmission metadata.
	// The raw_data fields contain the scaled readings.
	measurements := []map[string]interface{}{}
	measurements = append(measurements,
		map[string]interface{}{
			"sensor_id":          model.MacToString(transmitData.macAddress),
			"time":               transmitData.timestamp,
			"measurement_type":   transmitData.dataType,
			"sampling_frequency": transmitData.samplingFrequency,
			"axis":               "x",
			"raw_data":           x,
		},
		map[string]interface{}{
			"sensor_id":          model.MacToString(transmitData.macAddress),
			"time":               transmitData.timestamp,
			"measurement_type":   transmitData.dataType,
			"sampling_frequency": transmitData.samplingFrequency,
			"axis":               "y",
			"raw_data":           y,
		},
		map[string]interface{}{
			"sensor_id":          model.MacToString(transmitData.macAddress),
			"time":               transmitData.timestamp,
			"measurement_type":   transmitData.dataType,
			"sampling_frequency": transmitData.samplingFrequency,
			"axis":               "z",
			"raw_data":           z,
		},
	)

	return measurements
}

// Converts a two-byte temperature reading into a measurement record.
// Chooses the conversion method using the sensor's board model.
// Returns an empty slice if the byte count is invalid or conversion reports an error.
func handleTemperatureData(transmitData Transmission) []map[string]any {
	measurements := []map[string]any{}

	// A temperature transmission must contain one two-byte reading.
	if len(transmitData.packets) == 2 {
		var temperature float64
		var err error

		if transmitData.sensorModel == "machmo" {
			// Decode a signed little-endian 16-bit ADC reading, then convert
			// it to degrees Celsius using the MachMo resistance-based formula.
			var digitalTemp int16 = int16(transmitData.packets[1])<<8 | int16(transmitData.packets[0])
			temperature, err = parseTemperatureData(digitalTemp)
			out.Logger.Println("MachMo temperature reading")
		} else if transmitData.sensorModel == "machmomini" {
			// MachMo Mini uses a direct scale of 0.0625 degrees Celsius per count.
			var digitalTemp int16 = int16(transmitData.packets[1])<<8 | int16(transmitData.packets[0])
			err = nil
			temperature = float64(digitalTemp) * 0.0625
			out.Logger.Println(transmitData.packets)
			out.Logger.Printf("MachMo mini temperature digital %d celsius %f", digitalTemp, temperature)
		} else {
			// No temperature conversion is defined here for other board models.
			err = errors.New("Unknown board model " + transmitData.sensorModel)
		}

		if err == nil {
			measurements = append(measurements,
				map[string]any{
					"sensor_id":          model.MacToString(transmitData.macAddress),
					"time":               transmitData.timestamp,
					"measurement_type":   transmitData.dataType,
					"sampling_frequency": transmitData.samplingFrequency,

					// Keep the temperature in an array to match the measurement format.
					"raw_data": []float64{temperature},
				},
			)
		} else {
			out.Logger.Println("Error:", err)
		}
	} else {
		out.Logger.Println("Invalid temperature data received, expected 2 bytes but received", len(transmitData.packets))
	}

	return measurements
}

// Combines each three-byte audio sample into an integer and builds one measurement record.
// Attempts to trim leading zero samples near the start of the recording.
// Returns an empty slice if the byte count is not a multiple of three.
func handleAudioData(transmitData Transmission) []map[string]interface{} {
	measurements := []map[string]interface{}{}

	// Each audio sample occupies three bytes.
	if len(transmitData.packets)%3 == 0 {
		//out.Logger.Println(transmitData.packets[5000])
		numberOfMeasurements := len(transmitData.packets) / 3
		amplitude := make([]int, numberOfMeasurements)

		for i := 0; i < numberOfMeasurements; i++ {
			// Assemble a 24-bit value with the first byte in the highest position:
			// first byte << 16 | second byte << 8 | third byte.
			// This expression does not interpret the result as a signed 24-bit value.
			amplitude[i] = int(transmitData.packets[i*3])<<16 | int(transmitData.packets[i*3+1])<<8 | int(transmitData.packets[i*3+2])
		}

		// Search sample indices 0 through 512 for the first non-zero amplitude.
		// If found, discard the preceding zero samples.
		// If none is found within this range, leave the array unchanged.
		for i, amp := range amplitude {
			if i > 512 {
				break
			}
			if amp != 0 {
				amplitude = amplitude[i:]
				break
			}
		}

		// Attach the sensor and transmission metadata to the audio sample array.
		measurements = append(measurements,
			map[string]interface{}{
				"sensor_id":          model.MacToString(transmitData.macAddress),
				"time":               transmitData.timestamp,
				"measurement_type":   transmitData.dataType,
				"sampling_frequency": transmitData.samplingFrequency,
				"raw_data":           amplitude,
			},
		)
	} else {
		out.Logger.Println("Invalid audio data received. Packets of length", len(transmitData.packets), "not multiple of 3.")
	}
	return measurements
}

// Stores accumulated debug bytes separately for each device address.
var bufferCache map[string][]byte = make(map[string][]byte)

// Collects bytes received through the debug characteristic.
// A write containing exactly one zero byte marks the end of the transmission.
// Saves the accumulated bytes to a file in the current working directory.
func handleDebugData(address string, value []byte) {
	buffer, ok := bufferCache[address]
	//println(value)

	// A single zero byte is an end marker and is not added to the saved data.
	if len(value) == 1 && value[0] == 0x00 {
		if ok {
			// Replace colons in the MAC address to form the output filename.
			// Repeated debug transmissions from this address overwrite the same file.
			err := os.WriteFile("./"+strings.ReplaceAll(address, ":", "_")+"_debug.bin", buffer, 0644)
			if err != nil {
				println("Failed to write out file:")
				println(err.Error())
			}

			// Remove the cached bytes after the write attempt, even if it failed.
			delete(bufferCache, address)
			out.Logger.Println("Debug data received of size", len(buffer))
		} else {
			// No buffer exists for this device when the end marker arrives.
			out.Logger.Println("Device", address, "attempted to write empty array to disk")
			//out.Logger.Println(bufferCache)
		}
	} else {
		if ok {
			// Append bytes to the device's existing debug transmission.
			bufferCache[address] = append(bufferCache[address], value...)
		} else {
			// Start a debug buffer using the received byte slice.
			bufferCache[address] = value
		}
	}
}