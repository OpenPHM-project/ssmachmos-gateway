// Status panel: lists the sensors that have been seen, with their connection
// state and last seen time, and shows the server's process ID.

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
import 'package:provider/provider.dart';
import 'package:ss_machmos_gui/connection.dart';
import 'package:ss_machmos_gui/sensors.dart';

// Aims to be main panel, a more user-friendly logs pane
// Shows connected devices and their status
// Show failed uploads
/// Shows the server's process ID and the list of sensors that have been seen.
class Status extends StatelessWidget {
  const Status({super.key});

  /// Builds the refresh button, the process ID and the list of devices.
  @override
  Widget build(BuildContext context) {
    // read is fine here because the watch on the next line already rebuilds this screen.
    Connection conn = context.read<Connection>();
    // Only sensors that have a status (seen at least once) are listed.
    List<_Device> devices = context.watch<Connection>().sensors.where((Sensor s) => s.status != null).map((Sensor e) => _Device(e)).toList();

    return Container(
        margin: const EdgeInsets.fromLTRB(8, 12, 8, 8),
        child: Column(children: [
          // The refresh button and the server's process ID.
          Row(mainAxisAlignment: MainAxisAlignment.center, children: [
            // Asks the server for the list of connected sensors again.
            IconButton(icon: const Icon(Icons.refresh), tooltip: "Refresh device list", onPressed: () => conn.loadConnectedSensors()),
            // serverPid is null when no server process is known, so "none" is shown.
            Text("Process PID ${conn.serverPid ?? 'none'}"),
          ]),
          // shrinkWrap makes the list only as tall as its items.
          // BUG: the list is not in Expanded or a scroll view, so many devices overflow the screen and cannot be scrolled.
          ListView(
            shrinkWrap: true,
            padding: const EdgeInsets.all(8),
            children: devices,
          )
        ]));
  }
}

/// Shows one sensor: its connection state, name, model, address and last seen time.
class _Device extends StatelessWidget {
  final Sensor sensor;
  const _Device(this.sensor);

  /// Builds one row of the device list.
  @override
  Widget build(BuildContext context) {
    // The ! is safe because Status only builds devices that have a status.
    SensorStatus ss = sensor.status!;
    // BUG: lastSeen is shown without toLocal(), but predictedWakeupTime in sensors.dart uses toLocal(), so the two times may use different time zones.
    String timestamp = DateFormat('yyyy-MM-dd HH:mm:ss').format(ss.lastSeen);
    return ListTile(
      // A green tower when the sensor is connected, a crossed-out icon when it is not.
      leading: ss.connected ? Icon(Icons.cell_tower, color: Colors.green) : Icon(Icons.portable_wifi_off),
      title: Text("${ss.name} - ${sensor.model.name}"),
      // BUG: the model name is shown twice, here and in the title.
      // BUG: the texts are not wrapped or limited, so a narrow window can overflow this row.
      subtitle: Flex(direction: Axis.horizontal, children: [
        Text(ss.address),
        VerticalDivider(),
        Text(sensor.model.name),
        VerticalDivider(),
        Text("Last seen at $timestamp"),
        // Spacer pushes the texts to the left.
        Spacer(),
        //Spacer(flex: 4),
      ]),
      trailing: Column(children: [
        ss.connected ? Text("Device connected.") : Text("Device not connected."),
        Text(ss.activity),
      ]),
    );
  }
}
