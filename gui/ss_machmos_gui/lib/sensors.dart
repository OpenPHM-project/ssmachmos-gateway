// Sensors tab: a dropdown to pick a sensor and show its details, plus the
// Sensor data classes and helpers used across the app.

import 'dart:developer';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';
import 'package:provider/provider.dart';
import 'package:ss_machmos_gui/connection.dart';
import 'package:ss_machmos_gui/sensor_details.dart';
import 'package:ss_machmos_gui/sensor_models.dart';

/// Shows the sensor picker and the details of the selected sensor.
class Sensors extends StatelessWidget {
  const Sensors({super.key});

  /// Builds the sensor picker, or a message if no sensor is paired.
  @override
  Widget build(BuildContext context) {
    // watch (not read) so the screen rebuilds when the sensors or the selection change.
    Connection conn = context.watch<Connection>();
    List<Sensor> sensors = conn.sensors;
    // Nothing to select yet, so show a message instead of an empty dropdown.
    if (sensors.isEmpty) {
      return Column(children: [
        const Padding(
          // 100 moves the message down so it is not stuck at the top edge.
          padding: EdgeInsets.only(top: 100.0),
          child: Text("No sensors currently paired with the Gateway"),
        )
      ]);
    }

    return Column(children: [
      Padding(
          padding: const EdgeInsets.all(8),
          child: Row(mainAxisAlignment: MainAxisAlignment.spaceEvenly, children: [
            DropdownMenu(
              label: Text("Sensor selection:"),
              hintText: "Select Sensor",
              initialSelection: conn.displayedSensor,
              onSelected: (selectedSensor) {
                // Read provider state
                // Remember the choice in the provider so SensorDetails shows this sensor.
                // The cast is needed because DropdownMenu has no type given, so the value is dynamic.
                conn.displayedSensor = selectedSensor as Sensor;
              },
              dropdownMenuEntries: conn.sensors.map((s) => DropdownMenuEntry(value: s, label: s.name)).toList(),
            ),
            Text(conn.displayedSensor?.address ?? ""),
            Text(conn.displayedSensor?.model.name ?? "")
          ])),
      // Container(height: 0.5, color: Colors.grey),
      // SensorDetails is only shown once a sensor is selected.
      // BUG: SensorDetails has no key, so Flutter reuses the old form when the sensor changes and shows the previous sensor's values.
      if (conn.displayedSensor != null) SensorDetails()
    ]);
  }
}

/// Holds the settings of one measurement type of a sensor.
class SensorSettings {
  // Whether this measurement is taken on the next collection.
  bool active;
  // Samples per second.
  int samplingFrequency;
  // Seconds.
  int samplingDuration;

  SensorSettings({
    required this.active,
    required this.samplingFrequency,
    required this.samplingDuration,
  });
}

// Data class
/// Holds everything the gateway knows about one paired sensor.
class Sensor {
  // The MAC address as raw bytes.
  Uint8List mac;
  /// The MAC address as text. It is also the sensor's id.
  String get address => macToString(mac);
  String name;
  SensorModel model;
  List<String> types;
  // In millivolts.
  int batteryLevel;
  int collectionCapacity;
  // Seconds between two wake-ups.
  int wakeUpInterval;
  // Seconds. How far a wake-up may move from the expected time.
  int wakeUpIntervalMaxOffset;
  // The next wake-up time comes from predictedWakeupTime instead of this field.
  // DateTime nextWakeUp;
  bool deviceActive; // Should the sensor start sampling or stay idle
  // The last seen time comes from status.lastSeen instead of this field.
  //DateTime? lastSeen;
  // Null until the sensor has been seen at least once.
  SensorStatus? status;
  // Settings for each measurement type. The type name is the key.
  Map<String, SensorSettings> settings;

  Sensor(
      {required this.mac,
      required this.name,
      required this.model,
      required this.types,
      required this.batteryLevel,
      required this.collectionCapacity,
      required this.settings,
      required this.wakeUpInterval,
      required this.wakeUpIntervalMaxOffset,
      required this.deviceActive,
      this.status,
      // required this.lastSeen,
      // required this.nextWakeUp
  });

  /// Builds a Sensor from the JSON data sent by the server.
  factory Sensor.fromJson(Map<String, dynamic> s) {
    Map<String, SensorSettings> settings = {};
    // Each key is a measurement type, and its value holds that type's settings.
    for (var k in s["settings"].keys) {
      settings[k] = SensorSettings(
        active: s["settings"][k]["active"],
        samplingFrequency: s["settings"][k]["sampling_frequency"],
        samplingDuration: s["settings"][k]["sampling_duration"],
      );
    }
    return Sensor(
      // The MAC arrives as a list of numbers, so it is turned into bytes.
      mac: Uint8List.fromList(s["mac"].cast<int>()),
      name: s["name"],
      // An empty string is used when the server sends no model.
      model: SensorModel.fromString(s["model"] ?? ""),
      types: s["types"] != null ? s["types"].cast<String>() : [], // Can be null sometimes
      collectionCapacity: s["collection_capacity"],
      wakeUpInterval: s["wake_up_interval"],
      wakeUpIntervalMaxOffset: s["wake_up_interval_max_offset"],
      //nextWakeUp: DateTime.parse(s["next_wake_up"]),
      batteryLevel: s["battery_level"],
      deviceActive: s["device_active"] as bool,
      //lastSeen: DateTime.parse(s["last_seen"]), // KEEP THIS AS-IS, stored as local time
      settings: settings,
    );
  }

  /// Builds the text command that sends this sensor's settings to the server.
  String get sensorSettingsCommand {
    // One group of three values for each measurement type.
    var subSettings = settings.entries
        .map<String>((e) => "${e.key}_active ${e.value.active}"
            " ${e.key}_sampling_frequency ${e.value.samplingFrequency}"
            " ${e.key}_sampling_duration ${e.value.samplingDuration}")
        .join(" ");

    // Convert name spaces to underscores
    // The command is split on spaces, so a name with spaces would break it.
    // BUG: spaces in the name are probably saved as underscores, so the name may change after saving.
    return "${macToString(mac)}"
        " name ${name.replaceAll(' ', '_')}"
        " device_active $deviceActive"
        " wake_up_interval $wakeUpInterval"
        " wake_up_interval_max_offset $wakeUpIntervalMaxOffset"
        " $subSettings";
  }

  /// Works out when the sensor will wake up next, as text for the screen.
  String get predictedWakeupTime {
    // try/catch so a bad date shows "Unknown" instead of crashing the screen.
    try {
      if (status != null) {
        // BUG: ignores wakeUpIntervalMaxOffset, so it may differ from the time the server really picks.
        // BUG: if the sensor is late or offline, the time shown is in the past.
        // Shown in local time, as the Help page says.
        DateTime next = status!.lastSeen.toLocal().add(Duration(seconds: wakeUpInterval));
        return DateFormat('yyyy-MM-dd HH:mm:ss').format(next);
      } else {
        return "Never seen before!";
      }
    } catch (e) {
      log(e.toString());
      return "Unknown";
    }
  }

  /// Estimates how many bytes the active measurements use in the sensor's memory.
  int get memoryUsage {
    int totalSamples = 0;
    for (MapEntry<String, SensorSettings> setting in settings.entries) {
      // Only measurements that are turned on use memory.
      if(setting.value.active) {
        // BUG: the ! crashes if the model does not list this setting (for example when the model is unknown).
        if (model.sensorsAvailable[setting.key]!.singlePoint) {
          // A single-value measurement is one sample.
          totalSamples += 1;
        } else {
          // Samples per second times seconds.
          totalSamples += setting.value.samplingDuration * setting.value.samplingFrequency;
        }
      }
    }
    return totalSamples * 2; // Two bytes per sample
  }

  /// Returns the sampling frequencies the model allows for a setting, or an empty list if there are none.
  // BUG: an empty list makes FrequencySelectionDropdown crash, because it uses freqs.last.
  List<int> samplingFreqsForSetting(String setting) {
    // The log helps to find why a dropdown has no choices.
    if (!settings.containsKey(setting)) {
      log("Setting $setting no sampling frequencies returned");
      return [];
    }
    return model.sensorsAvailable[setting]?.samplingFrequencies ?? [];
  }
}

/// Turns MAC bytes into text like "AA:BB:CC:DD:EE:FF".
String macToString(Uint8List mac) {
  // padLeft keeps two digits per byte, so 10 gives "0a" and not "a".
  return mac.map((b) => b.toRadixString(16).padLeft(2, "0")).join(":").toUpperCase();
}
