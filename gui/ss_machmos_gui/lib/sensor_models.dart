// Sensor models: the supported sensor hardware, their memory size and the
// measurements each one offers.

// Embedding data as enums
/// Lists the sensor hardware models and what each one can measure.
enum SensorModel {
  // To be more precise, the cache partition is 0x2D0000 bytes
  machmo(name: "MachMo", autoMaxCapacity: true, maxCapacity: 0x2D0000, sensorsAvailable: {
    "vibration": (singlePoint: false, samplingFrequencies: [25, 50, 100, 200, 400, 800, 1600, 3200, 6400, 12800, 25600]),
    // Audio has only one allowed frequency.
    "audio": (singlePoint: false, samplingFrequencies: [221100]),
    "temperature": (singlePoint: true, samplingFrequencies: []),
    // BUG: the Help page does not list flux as a measurement type.
    "flux": (singlePoint: true, samplingFrequencies: []),
  }),
  // 2949120 is the same size as 0x2D0000, the MachMo value above.
  machmomini(name: "MachMoMini", autoMaxCapacity: false, maxCapacity: 2949120, sensorsAvailable: {
    "vibration": (singlePoint: false, samplingFrequencies: [25, 50, 100, 200, 1000, 2000, 4000, 8000, 16000, 32000]),
    "temperature": (singlePoint: true, samplingFrequencies: []),
  }),
  // Used when the model is not recognized.
  // BUG: it lists no measurements, so Sensor.memoryUsage can crash for a sensor with this model.
  unknown(name: "Unknown", autoMaxCapacity: false, maxCapacity: 0, sensorsAvailable: {});

  const SensorModel({required this.name, required this.autoMaxCapacity, required this.maxCapacity, required this.sensorsAvailable});

  // The name shown on screen.
  final String name;
  // Not read in this file. It is true only for MachMo. Search the project for autoMaxCapacity to see what it does.
  final bool autoMaxCapacity;
  // The memory size in bytes.
  final int maxCapacity;
  // The measurements the model offers, with the measurement name as the key.
  // singlePoint is true for one value per collection, so it has no frequency or duration.
  // samplingFrequencies lists the allowed samples per second, and is empty for singlePoint.
  final Map<String, ({bool singlePoint, List<int> samplingFrequencies})> sensorsAvailable;

  /// Turns the model text from the server into a SensorModel.
  factory SensorModel.fromString(String str) {
    // The text must match exactly, in lowercase.
    if (str == "machmo") return SensorModel.machmo;
    if (str == "machmomini") return SensorModel.machmomini;
    // Anything else is unknown, so the app does not crash on a new model.
    return SensorModel.unknown;
  }
}
