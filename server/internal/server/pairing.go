// Handles discovering devices, tracking requests, and accepting sensors into the system

package server

import (
	"encoding/binary"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
	"github.com/jukuly/ss_machmos/server/internal/out"
)

// Stores information about one device requesting pairing 
type request struct {
	announcedModel     string // Model reported by the device
	dataTypes          []string // Measurement types the device supports, e.g. vibration, temperature, audio
	collectionCapacity uint32 // How much data the device can store in bytes
	isPaired           bool // Whether the device has been accepted for pairing
	announcedSensors   bool // Whether the device has announced its sensors/provided its capabilities
}

// Stores information returned for saved sensors, including their connection status and last recorded activity
type SensorStatus struct {
	Address   string               `json:"address"` // MAC address of the sensor
	Name      string               `json:"name"` // Name of the sensor
	Connected bool                 `json:"connected"` // Whether the sensor is currently connected
	LastSeen  time.Time            `json:"last_seen"` // When the sensor was last seen
	Activity  model.SensorActivity `json:"activity"` // The sensor's current activity
}

/*
 * List of devices that are connected but aren't fully done yet.
 * Done here means BLE agent pairs and authenticates
 * and device announced sensors on board
 */

// Tracks pairing across the application
type pairingState struct {
	active    bool // Controls whether pairing (Pair()) is enabled or disabled
	requested map[[6]byte]request // Maps MAC addresses to pending pairing requests
}

// List of all devices pending pairing.
// Here this means devices that are connected, but haven't announced capabilities or finished pairing
var state pairingState

// Enables pairing mode, allowing new devices to be paired with the system
func EnablePairing() {
	state.active = true
	out.Logger.Println("Enabled pairing")
}

// Disables pairing mode, preventing new devices from being paired with the system
func DisablePairing() {
	state.active = false
	out.Logger.Println("Disabled pairing")
}

// Returns list of MAC addresses of devices pending pairing
// A sensor must announce capabilites to show up here
func ListDevicesPendingPairing() []string {
	keys := make([]string, 0, len(state.requested))
	for mac, req := range state.requested {
		if req.announcedSensors == true {
			keys = append(keys, model.MacToString(mac))
		}
	}
	return keys
}

// Returns each saved sensor’s connection status and last recorded activity
// GUI compares that to the sensors last seen
func ConnectedDevices() []SensorStatus {
	var devices []SensorStatus = []SensorStatus{}
	// Get all saved sensors
	for _, sensor := range model.Sensors {
		connected := false
		// Intersect with bluetooth connected devices
		for _, dev := range adapter.GetConnectedDevices() {
			if sensor.IsMacEqual(dev.Address.MAC.String()) {
				connected = true
				break
			}
		}
		// Add to list of devices with connection status
		devices = append(devices, SensorStatus{
			Address:   model.MacToString(sensor.Mac),
			Name:      sensor.Name,
			Connected: connected,
			LastSeen:  sensor.FetchLastSeen().LastSeen,
			Activity:  sensor.FetchLastSeen().LastActivity,
		})
	}
	return devices
}

// Checks if a device with the given MAC address is already saved in the system
// If the device is already written down, returns the sensor object, otherwise returns nil
func sensorExists(MAC [6]byte) *model.Sensor {
	for _, sens := range model.Sensors {
		for m := range MAC {
			if MAC[m] != sens.Mac[m] {
				// Get next sensor
				break
			}
			// Exact match, early out with true
			return &sens
		}
	}
	// No sensors matched, false
	return nil
}


/* Handles a device connection
Updates activity and broadcasts a connection event for an existing sensor 
Returns true if the device is new, or false if already saved */
func pairDeviceConnected(MAC [6]byte) bool {
	// Test if MAC address is already stored in settings
	sensor := sensorExists(MAC)
	if sensor != nil {
		out.Logger.Println("pairDeviceConnected " + model.MacToString(MAC) + " already exists.")
		// Update last seen log
		sensor.UpdateLastSeen(model.SensorActivityIdle)
		// Log that device already exists
		out.Broadcast("SENSOR-CONNECTED:" + model.MacToString(MAC))
		return false
	} else {
		out.Logger.Println("pairConnectedDevice newly discovered " + model.MacToString(MAC))
		// Device is newly connected, give out notification
		//out.Broadcast("PAIR-DEVICE-CONNECTED:" + model.MacToString(MAC))
	}
	out.Logger.Println("Connected device address " + model.MacToString(MAC))

	return true
}

// Called on device disconnect, removes from pending list and notifies GUI
func pairDeviceDisconnected(MAC [6]byte) bool {
	_, ok := state.requested[MAC] // Get the request for this MAC address
	if ok { // If the device was pending pairing, remove it from the list and notify GUI
		delete(state.requested, MAC)
		out.Broadcast("PAIR-DEVICE-DISCONNECTED: " + model.MacToString(MAC))
	}
	out.Broadcast("SENSOR-DISCONNECTED:" + model.MacToString(MAC))

	return true
}

// Device writes out what sensors are on board 
// and what data types it can collect. This is the first step in pairing
func pairReceiveCapabilities(MAC [6]byte, data []byte) bool {
	if sensorExists(MAC) != nil { // If the device is already paired, ignore the capabilities
		return true
	}

	// Go throught with the process only if the sensor is new
	req, ok := state.requested[MAC]

	// If not found
	if !ok {
		// Address is new, store to memory and don't write out until pairing is done + capabilities are out
		// BUG the connect/disconnect callbacks aren't really reliable, might be a BlueZ or library issue
		// In any case, if you write something to this characteristic you're definitely connected
		// and probably know what this bluetooth service does
		state.requested[MAC] = request{
			isPaired:         false, // Depends on user acceptance
			announcedSensors: false, // Depends on sensor giving out details
		}
		req = state.requested[MAC]
		out.Logger.Println("New device", model.MacToString(MAC), "requests pairing")

	}

	// Check that the data is the expected length
	if len(data) != 6 {
		out.Logger.Println("pairReceiveCapabilities expect data with 6 bytes, received", len(data), "bytes instead")
		return false
	}

	// Parse sensor information
	// from protocol.md
	// data types (1 byte) | collection capacity in bytes (4 bytes) | sensor model (1 byte)
	// Data types: b(0 0 0 0 0 vibration temperature audio)
	// Save sensors to memory
	bit_types := map[byte]string{
		1 << 0: "audio",
		1 << 1: "temperature",
		1 << 2: "vibration",
		1 << 3: "flux",
	}
	for bit, name := range bit_types {
		// If bit is turned on
		if data[0]&bit == bit {
			req.dataTypes = append(req.dataTypes, name)
		}
	}
	// Save collection capacity and model
	req.collectionCapacity = binary.LittleEndian.Uint32(data[1:5])

	// Check if the model is known, otherwise mark as unknown
	if model, ok := model.SENSOR_MODELS[data[5]]; ok {
		req.announcedModel = model
	} else {
		req.announcedModel = "unknown"
	}

	// Mark that the device has announced its sensors and capabilities
	req.announcedSensors = true
	state.requested[MAC] = req

	// Announce new sensor pending pairing
	out.PairingLog("REQUEST-NEW:" + model.MacToString(MAC))
	out.Logger.Println("Identified sensor", model.MacToString(MAC), ", waiting for pair confirmation")
	return true
}

// see protocol.md to understand what is going on here
/* Completes application registration by adding the sensor, 
removing its pending request, and broadcasting pairing success */
func pairConfirmation(mac [6]byte) {
	// Check if the device is in the requested list, if not, log an error and return
	if _, exists := state.requested[mac]; !exists {
		out.Logger.Println("ERR:Device does not exist")
		return
	}

	// state.pairing = [6]byte{}
	// Display pair code inline with each entry?
	// Or just do it automatically over serial maybe
	// Write sensor data to disk
	model.AddSensor(mac, state.requested[mac].dataTypes, state.requested[mac].collectionCapacity)
	delete(state.requested, mac)

	// I'm 80% sure GUI reads this for pairing information
	out.Broadcast("PAIR-SUCCESS:" + model.MacToString(mac))
}

// see protocol.md to understand what is going on here
// Client requests pairing with this device
// PAIR-ACCEPT $mac
func Pair(mac [6]byte) {
	if !state.active {
		out.PairingLog("PAIRING-DISABLED")
		return
	}
	out.Logger.Println(state.requested)
	request, ok := state.requested[mac]
	if !ok {
		out.PairingLog("REQUEST-NOT-FOUND " + model.MacToString(mac))
		return
	}
	// TODO start BLE pairing here
	request.isPaired = true
	state.requested[mac] = request
	// For now just mark as paired
	out.PairingLog("PAIRING-WITH:" + model.MacToString(mac))
	out.Logger.Println("Confirming pairing with device")
	pairConfirmation(mac)
}

// TODO: implement this function to disconnect a device by its MAC address
func DisconnectDevice(mac [6]byte) {
	//adapter.
}
