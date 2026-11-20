/*
Sets up the gateway's Bluetooth Low Energy (BLE) services.

Registers characteristics through which sensors send measurements,
announce their capabilities, read configuration settings, and obtain
the current time.

Also configures advertising, handles connection events, starts the
transmission watchdog, and provides functions for notifying sensors
about configuration changes or collection requests.
*/
package server

import (
	"encoding/binary"
	"errors"
	"os"
	"os/signal"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
	"github.com/jukuly/ss_machmos/server/internal/out"
	"tinygo.org/x/bluetooth"
)

// Default HTTP endpoint for uploading measurements.
// Upload functions use the endpoint stored in the Gateway configuration.
const DEFAULT_GATEWAY_HTTP_ENDPOINT = "https://openphm.org/gateway_data"

// Shared Bluetooth adapter used to register services and manage advertising.
var adapter = bluetooth.DefaultAdapter

// UUIDs identify Bluetooth services and their characteristics.
// A service groups related functionality; characteristics provide individual
// values or communication points that devices can read, write, or subscribe to.

// Measurement upload service and its debug/control characteristics.
// The control characteristic's registration is currently commented out below.
var DATA_SERVICE_UUID = bluetooth.MustParseUUID("2deacc71-7b29-4ff4-8fc2-59461c7a73f5")
var DEBUG_DATA_CHRC_UUID = bluetooth.MustParseUUID("ad690aaa-cfd4-4b4a-96a6-1110cb6782f6")
var CONTROL_DATA_CHRC_UUID = bluetooth.MustParseUUID("0b7f9057-38ef-4db5-8e25-64bc66fb1963")

// Separate characteristics route incoming bytes by measurement type:
// flux, temperature, vibration, and audio.
var FLUX_DATA_CHRC_UUID = bluetooth.MustParseUUID("68e92ad3-0fb5-4c93-8b99-0d21771576fd")
var RTD_DATA_CHRC_UUID = bluetooth.MustParseUUID("e64d1230-86ba-46aa-a62d-736d6f58226c")
var ACCEL_DATA_CHRC_UUID = bluetooth.MustParseUUID("e70ada20-ac8e-45f8-9f5d-593226bb7284")
var MIC_DATA_CHRC_UUID = bluetooth.MustParseUUID("fee1ed78-2a76-490e-8a7c-9b698c9202d1")

// Configuration service, device identification/settings characteristic,
// and time synchronization characteristic.
var CONFIG_SERVICE_UUID, _ = bluetooth.ParseUUID("0ffd06bd-5f9c-4583-b852-e92fdbe8e862")
var CONFIG_IDENTIFY_CHRC_UUID, _ = bluetooth.ParseUUID("4a488208-f3b9-414f-85c7-17eb16c653b0")
var CONFIG_TIME_UUID, _ = bluetooth.ParseUUID("7aeb6aa1-5e11-4543-ba04-a6d1d6bf936f")

// Chrc to notify a sensor to start sending data immediately.
var CONFIG_START_SAMPLING_CHRC_UUID, _ = bluetooth.ParseUUID("f6344769-e905-4c4d-a6e8-0aa8b63f1153")

// Chrc to notify a sensor of next wake up time.
// This characteristic is declared but is not registered in Init below.
var CONFIG_WAKE_AT_CHRC_UUID, _ = bluetooth.ParseUUID("9203c6cb-b4d4-49e2-a84d-415d2cb790f1")

// Maps individual data type codes to their names.
// These are separate from the capability bit flags used in pairing.go.
var DATA_TYPES = map[byte]string{
	0x00: "vibration",
	0x01: "audio",
	0x02: "temperature",
	0x03: "battery",
	0x04: "flux",
}

// Characteristic handles used to interact with registered characteristics.
// Init assigns the settings, start-sampling, and time handles below.
// The pairing-response and wake-time handles are not assigned in this file.
var pairResponseCharacteristic bluetooth.Characteristic
var settingsCharacteristic bluetooth.Characteristic
var configWakeAtChar bluetooth.Characteristic
var configStartSampleChar bluetooth.Characteristic
var configTimeChar bluetooth.Characteristic

// Gateway config
// Shared configuration supplied to Init and used when uploading measurements.
var Gateway *model.Gateway

// List of devices flagged for collection
// The start-sampling ReadEvent consumes a matching address from this slice.
// No function in this file adds addresses to the slice.
var flaggedForCollect []string

// Prepares storage, enables Bluetooth, registers services and callbacks,
// configures advertising, and starts the transmission watchdog.
// Advertising is started separately by StartAdvertising.
func Init(g *model.Gateway) error {
	// Prepare the directories used for measurement storage.
	dataDir() // ensure data dir exists
	unsentDataDir()
	archivedDataDir()

	// Make the supplied gateway configuration available to the other functions.
	Gateway = g

	// Enable the Bluetooth adapter before registering services.
	err := adapter.Enable()
	if err != nil {
		return err
	}

	// Begin with pairing acceptance disabled and no pending requests.
	state = pairingState{
		active:    false,
		requested: make(map[[6]byte]request),
	}

	// Data collection service, requires pairing and bonding for authentication
	// https://lpccs-docs.renesas.com/Tutorial-DA145x-BLE-Security/ble_security.html
	// Out of Band could be done with a USB serial connection

	// The comments above describe the intended security requirement.
	// The flags below specify write permissions; this setup does not itself
	// demonstrate that Bluetooth pairing or bonding is enforced.
	// handleData separately checks whether the device is a saved sensor.
	dataService := bluetooth.Service{
		UUID: DATA_SERVICE_UUID,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				// Accept debug writes and pass their bytes to the debug handler.
				// Both regular writes and writes without a response are permitted.
				UUID:  DEBUG_DATA_CHRC_UUID,
				Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				WriteEvent: func(client bluetooth.Connection, address string, offset int, value []byte) {
					// Dump any data received into a bin file
					// The handler accumulates bytes until it receives an end marker.
					handleDebugData(address, value)
				},
			},

			// This control characteristic is currently disabled.
			// {
			// 	UUID:  CONTROL_DATA_CHRC_UUID,
			// 	Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
			// 	WriteEvent: handleControlCode,
			// },
			{
				// Route accelerometer writes to vibration packet processing.
				UUID:  ACCEL_DATA_CHRC_UUID,
				Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				// offset is used for when the MTU is less than 512 bytes, but maybe bluez handles that???
				// The offset argument is forwarded but currently unused by handleData.
				WriteEvent: func(client bluetooth.Connection, address string, offset int, value []byte) {
					handleData("vibration", client, address, offset, value)
				},
			},
			{
				// Route flux writes to the shared packet handler.
				// handleData currently has no conversion branch for completed flux data.
				UUID:  FLUX_DATA_CHRC_UUID,
				Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				WriteEvent: func(client bluetooth.Connection, address string, offset int, value []byte) {
					handleData("flux", client, address, offset, value)
				},
			},
			{
				// Route microphone writes to audio packet processing.
				UUID:  MIC_DATA_CHRC_UUID,
				Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				WriteEvent: func(client bluetooth.Connection, address string, offset int, value []byte) {
					handleData("audio", client, address, offset, value)
				},
			},
			{
				// Route temperature writes to temperature packet processing.
				UUID:  RTD_DATA_CHRC_UUID,
				Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				WriteEvent: func(client bluetooth.Connection, address string, offset int, value []byte) {
					handleData("temperature", client, address, offset, value)
				},
			},
		},
	}

	// Register the measurement service.
	// Any error returned by this call is currently ignored.
	adapter.AddService(&dataService)

	// Register configuration, time synchronization, and collection controls.
	configService := bluetooth.Service{
		UUID: CONFIG_SERVICE_UUID,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				// Keep this, use for devices to manage settings

				// Retain a handle so TriggerSettingCollection can write to
				// this characteristic when configuration changes.
				Handle: &settingsCharacteristic,
				UUID:   CONFIG_IDENTIFY_CHRC_UUID,
				Flags:  bluetooth.CharacteristicReadPermission | bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicNotifyPermission,

				// WriteEvent to collect device information
				// A sensor writes its capabilities here to identify itself.
				WriteEvent: func(connection bluetooth.Connection, address string, offset int, value []byte) {
					// Convert the sender's address before processing its capabilities.
					mac, err := bluetooth.ParseMAC(address)
					if err != nil {
						out.Logger.Println("SettingsCharacteristic: Failed to parse MAC", address)
						return
					}

					// Forward non-empty data to the capability parser.
					// That function checks the expected payload length.
					if len(value) > 0 {
						pairReceiveCapabilities(mac, value)
					} else {
						out.Logger.Println("Settings characteristic: Received zero value from device", address, " how.")
					}
				},

				// ReadEvent to return device-appropriate settings
				// Also how...
				// Look up settings using the requesting device's address.
				ReadEvent: func(client bluetooth.Connection, address string, offset int) []byte {
					out.Logger.Println("Device", address, "requested settings")
					return getSettingsForSensor(address)
				},
			},
			{
				// Separate from settings characteristic to reduce latency
				Handle: &configTimeChar,
				UUID:   CONFIG_TIME_UUID,
				Flags:  bluetooth.CharacteristicReadPermission,
				ReadEvent: func(_ bluetooth.Connection, _ string, _ int) []byte {
					// Return the server's Unix time in microseconds,
					// encoded as eight little-endian bytes.
					var now int64 = time.Now().UnixMicro()
					return binary.LittleEndian.AppendUint64([]byte{}, uint64(now))
				},
			},
			{
				// Signals to devices if they should start sampling immediately
				// works if device is awake and connected, mostly for debugging transmission speed
				Handle: &configStartSampleChar,
				UUID:   CONFIG_START_SAMPLING_CHRC_UUID,
				Flags:  bluetooth.CharacteristicReadPermission | bluetooth.CharacteristicNotifyPermission,
				ReadEvent: func(client bluetooth.Connection, address string, offset int) []byte {
					// Check whether this device has a pending collection flag.
					for i, dev := range flaggedForCollect {
						if dev == address {
							// Remove flag FIXME: this is just invalid code.
							// Replace with a map for the nicer API

							// Replace the matching entry with the last entry,
							// then shorten the slice. This does not preserve order.
							flaggedForCollect[i] = flaggedForCollect[len(flaggedForCollect)-1]
							flaggedForCollect = flaggedForCollect[:len(flaggedForCollect)-1]

							// Tell the requesting device to begin sampling.
							return []byte{0x1}
						}
					}

					// No collection flag was found for this address.
					return []byte{0x0}
				},
			},
		},
	}

	// Register the configuration service and return any registration error.
	err = adapter.AddService(&configService)
	if err != nil {
		return err
	}

	/*
	 * Signal pairing procedure over serial.
	 * Share pairing code over serial
	 * This will need a separate BlueZ BLE agent to handle pairing.
	 * Could the default agent handle it? Think of supplying a code other than 0000
	 * Alternative would be to accept pairing only if device sends MAC address over serial
	 */

	// Register a callback for Bluetooth connection and disconnection events.
	adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		if connected {
			// NOTE upstream go bluetooth MAC address arrays are reversed
			// This here works if using the patched branch
			// On connect add to list of devices pending pairing

			// The current pairDeviceConnected implementation logs new devices
			// and updates known sensors. Pending requests are created when
			// pairReceiveCapabilities receives capabilities.
			pairDeviceConnected(device.Address.MAC)
			out.Logger.Println("Bluetooth connection with device", device.Address.MAC.String())
		} else {
			// Remove any pending request and broadcast the disconnection.
			pairDeviceDisconnected(device.Address.MAC)
			out.Logger.Println("Bluetooth disconnected", device.Address.MAC.String())
		}
	})

	// Enable is called again here; its returned error is currently ignored.
	adapter.Enable()

	// Obtain the advertisement object used to make the gateway discoverable.
	adv := adapter.DefaultAdvertisement()
	if adv == nil {
		return errors.New("advertisement is nil")
	}

	// Configure the advertised name and service UUID.
	// This prepares the advertisement without starting it.
	err = adv.Configure(bluetooth.AdvertisementOptions{
		LocalName: "Gateway Server",
		// BUG can't advertise more than one service UUID
		ServiceUUIDs: []bluetooth.UUID{DATA_SERVICE_UUID},
	})
	if err != nil {
		return err
	}

	// Setup watchdog timer
	// Run the idle-transmission checks concurrently in a background goroutine.
	go startWatchdog()

	return nil
}

// Starts Bluetooth advertising and installs an interrupt handler.
// On an interrupt such as Ctrl+C, stops advertising and exits the process.
func StartAdvertising() error {
	// Use a buffered channel to receive operating-system interrupt signals.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// Listen for interrupts in the background while the server continues running.
	go func() {
		for sig := range c {
			if sig == os.Interrupt {
				err := adapter.DefaultAdvertisement().Stop()
				if err != nil {
					out.Logger.Println("Error:", err)
				}
				out.Logger.Println("Stopping server")
				os.Exit(0)
				return
			}
		}
	}()

	// Start the advertisement prepared during Init.
	adv := adapter.DefaultAdvertisement()
	if adv == nil {
		return errors.New("advertisement is nil")
	}
	return adv.Start()
}

// Attempts to stop advertising, then exits the entire process.
// The error returned by Stop is currently ignored.
func StopAdvertising() {
	adapter.DefaultAdvertisement().Stop()
	out.Logger.Println("Stopping server")
	os.Exit(0)
}

// Notify all connected devices to read configuration again
// Writes a notification value through the saved settings characteristic handle.
func TriggerSettingCollection() {
	out.Logger.Println("Notifying all connected devices of new configs")
	// NOTE: this triggers our own ReadEvent callback
	// because this library has no way to direct notify...
	settingsCharacteristic.Write([]byte{0x0})
}

// Builds and writes a collection request containing the target device's address.
// This function does not add the address to flaggedForCollect.
func TriggerCollection(address string) {
	out.Logger.Println("Notifying " + address + " of collection request")

	// notify a device that we want it to start collecting data
	mac, _ := model.StringToMac(address)

	// Build an eight-byte message:
	// six reversed MAC-address bytes, followed by two control bytes.
	output := make([]byte, 8)
	for i := range mac {
		// Write mac address reversed
		output[i] = mac[len(mac)-i-1]
	}
	output[6] = 0x0 // Command byte
	output[7] = 0x0 // Targets sensor

	// Notify
	// Write the request through the start-sampling characteristic.
	// Any write result or error is currently ignored.
	configStartSampleChar.Write(output)
}