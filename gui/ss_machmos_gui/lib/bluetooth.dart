// Bluetooth pairing screen: a switch to turn pairing on and a list of nearby
// sensors to pair with.

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'package:ss_machmos_gui/connection.dart';

/// Shows the pairing switch and the list of sensors found nearby.
class Bluetooth extends StatelessWidget {
  const Bluetooth({super.key});

  /// Builds the pairing switch and, when pairing is on, the list of nearby sensors.
  @override
  Widget build(BuildContext context) {
    // watch (not read) so the screen rebuilds when pairing or the nearby sensors change.
    Connection _conn = context.watch<Connection>();
    bool pairingEnabled = _conn.pairingEnabled;
    // The MAC address of the sensor being paired right now.
    String pairingWith = _conn.pairingWith;
    // The MAC addresses of the sensors found while pairing is on.
    List<String> sensorsNearby = _conn.sensorsNearby;
    return Padding(
      padding: const EdgeInsets.fromLTRB(8, 12, 8, 8),
      child: Column(
        children: [
          Row(
            children: [
              const Icon(
                Icons.bluetooth,
                size: 30,
              ),
              const Text("Discover Sensors"),
              const Spacer(),
              Switch(
                value: pairingEnabled,
                onChanged: (value) => _conn.setPairingState(value),
              ),
            ],
          ),
          const SizedBox(height: 12),
          // The list is only shown while pairing is on.
          if (pairingEnabled)
            // Expanded gives the list the rest of the height, so it can scroll.
            Expanded(
              child: DecoratedBox(
                decoration: BoxDecoration(
                  // 0.5 makes a thin line around the list.
                  border: Border.all(color: Colors.grey, width: 0.5),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: ListView.separated(
                  // One extra item for the spinner at the bottom.
                  itemCount: sensorsNearby.length + 1,
                  separatorBuilder: (_, __) => const Divider(
                      color: Colors.grey, height: 0.5, thickness: 0.5),
                  itemBuilder: (BuildContext context, int index) {
                    // Last item is the spinner at the bottom
                    // It shows that the app is still looking for sensors.
                    if (index == sensorsNearby.length) {
                      return const Center(
                        child: Padding(
                          // 30 gives the spinner some room above and below.
                          padding: EdgeInsets.symmetric(vertical: 30),
                          child: SizedBox(
                            width: 25,
                            height: 25,
                            child: CircularProgressIndicator(
                              strokeWidth: 3,
                            ),
                          ),
                        ),
                      );
                    }
                    // Tapping a sensor starts pairing with it.
                    // BUG: only the sensor being paired is disabled, so another one can be tapped while pairing.
                    return SensorListItem(
                      mac: sensorsNearby[index],
                      pairing: pairingWith == sensorsNearby[index],
                      onTap: () =>
                          _conn.acceptPairing(sensorsNearby[index]),
                    );
                  },
                ),
              ),
            ),
        ],
      ),
    );
  }
}

/// Shows one nearby sensor's MAC address, with a small spinner while it is pairing.
class SensorListItem extends StatelessWidget {
  final String mac;
  // True when this sensor is the one being paired right now.
  final bool pairing;
  final void Function() onTap;

  const SensorListItem(
      {super.key,
      required this.mac,
      required this.pairing,
      required this.onTap});

  /// Builds one row of the list.
  @override
  Widget build(BuildContext context) {
    return ListTile(
      title: Row(
        children: [
          Text(mac, style: const TextStyle(fontSize: 14)),
          const Spacer(),
          // A smaller spinner than the one at the bottom of the list, so the two are easy to tell apart.
          if (pairing)
            const SizedBox(
              width: 15,
              height: 15,
              child: CircularProgressIndicator(
                strokeWidth: 2.5,
              ),
            ),
        ],
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 20),
      // null turns the row off, so the same sensor cannot be tapped twice while it pairs.
      onTap: pairing ? null : onTap,
    );
  }
}
