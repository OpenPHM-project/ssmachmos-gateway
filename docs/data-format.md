# Gateway Data Format

Reference for the measurement header, sensor payloads, and JSON uploaded to OpenPHM.

## 1. Header

The gateway reads an **8-byte header** describing the incoming measurement data.

| Byte positions | Size | Meaning | Encoding |
|---|---|---|---|
| 0–3 | 4 bytes | Total measurement payload size, excluding the header | Unsigned 32-bit, little endian |
| 4–7 | 4 bytes | Sampling frequency in Hz (samples per second) | Unsigned 32-bit, little endian |

The **measurement payload** contains the actual sensor readings as bytes. For example, 100 vibration samples × 6 bytes = **600 payload bytes**, excluding the header.

## 2. Measurement/Sensor Payloads

### Vibration

| Property | Format |
|---|---|
| Bytes per sample | 6 |
| Layout | X: 2 bytes, followed by Y: 2 bytes, then Z: 2 bytes |
| Encoding | Each axis is a signed 16-bit integer, little endian |
| Conversion | Multiply each decoded axis value by `0.000244` |
| Output unit | g (acceleration expressed relative to Earth's gravity) |

The gateway decodes each axis's two bytes into a signed integer, then applies the scaling factor to obtain acceleration in g.

### Temperature

| Property | Format |
|---|---|
| Payload size | Exactly 2 bytes: one temperature reading |
| Encoding | Signed 16-bit integer, little endian |
| `machmomini` conversion | Multiply the decoded value by `0.0625` |
| `machmo` conversion | Convert the decoded value to resistance, then calculate temperature using `parseTemperatureData()` |
| Output unit | Degrees Celsius (°C) |

**MachMo conversion**

First, calculate resistance in ohms:

`R = (raw reading / 32767) × 1500`

Then calculate temperature in °C:

| Condition | Formula | Constants |
|---|---|---|
| R ≥ 1000 Ω | `T = (-A + sqrt(A² - 4B(1 - R/1000))) / (2B)` | A = 0.0039083; B = -0.0000005775 |
| R < 1000 Ω | `T = (-B + sqrt(B² - 4A(C - R))) / (2A)` | A = -0.00061414; B = 3.907359803; C = 999.9979 |

**Examples:** A `machmomini` raw reading of 400 gives 25°C. A `machmo` resistance of 1000 Ω gives 0°C.

### Audio

| Property | Format |
|---|---|
| Bytes per sample | 3 |
| Current decoding | Unsigned 24-bit value, big endian |
| Conversion | Combine three bytes into an integer amplitude; no physical-unit scaling |
| Output unit | Raw amplitude counts |

**Processing:** The gateway removes leading zero samples if the first nonzero sample is found within indices 0–512, inclusive. Otherwise, it leaves the array unchanged.

**Pending confirmation:** The gateway decodes audio as big endian, but the task flags a conflicting little-endian comment. Confirm the sensor's actual byte order using firmware or a known sample.

### Flux *WIP

| Property | Format |
|---|---|
| Bytes per sample | 2, per task specification |
| Encoding | Signedness and byte order not yet confirmed |
| Conversion | Not implemented |
| Output unit | Not yet confirmed |
| Gateway handling | No flux conversion branch in `handleData()`; treated as an unknown data type |

## 3. Upload JSON

The gateway sends an HTTP POST with an `application/json` body. The outer object contains gateway credentials and an array of measurement records.

### Request fields

| Field | Meaning |
|---|---|
| `gateway_id` | Gateway identifier |
| `gateway_password` | Gateway authentication password |
| `measurements` | Array of measurement records |

### Measurement fields

| Field | Meaning |
|---|---|
| `sensor_id` | Sensor's MAC address |
| `time` | Gateway time when it received the transmission header |
| `measurement_type` | `vibration`, `temperature`, or `audio` |
| `sampling_frequency` | Sampling frequency from the header, in Hz |
| `axis` | `x`, `y`, or `z`; present only for vibration |
| `raw_data` | Array of decoded values: g for vibration, °C for temperature, or raw amplitude counts for audio |

Despite its name, `raw_data` contains decoded values, not the original bytes. A valid vibration transmission produces **three records**, one per axis. Valid temperature and audio transmissions each produce **one record**.

### Example: temperature upload

All identifiers and credentials below are placeholders.

```json
{
  "gateway_id": "example-gateway",
  "gateway_password": "example-password",
  "measurements": [
    {
      "sensor_id": "AA:BB:CC:DD:EE:FF",
      "time": "2026-10-25T17:00:00Z",
      "measurement_type": "temperature",
      "sampling_frequency": 1,
      "raw_data": [25.0]
    }
  ]
}
```

This represents one reading of **25°C**. The sampling frequency is illustrative and is not used in temperature conversion. Temperature records omit `axis`.

## 4. Sample and Pending Validation

- **Sample availability:** No sensor sample collected; only the repository was available.
- **Audio:** Confirm the sensor's byte order against firmware or a known sample.
- **Flux:** Confirm the stated 2-byte sample size, signedness, byte order, conversion, and output unit.
- **Future validation:** Collect a `.bin` file from `/tmp/ss_machmos/debug_data/` on the gateway machine. These files contain measurement payload bytes **without the header**. Record the sensor model, measurement type, and sampling frequency separately.

## Source References

Paths are relative to the repository root.

| File | Relevant functions or types |
|---|---|
| `server/internal/server/packetAssembling.go` | `savePacket()`, `handleData()`, `handleVibrationData()`, `handleTemperatureData()`, `handleAudioData()` |
| `server/internal/server/measurements.go` | `parseTemperatureData()`, `sendMeasurements()`, `saveDebugMeasurements()` |
| `server/internal/model/gateway.go` | `RequestBody` |
