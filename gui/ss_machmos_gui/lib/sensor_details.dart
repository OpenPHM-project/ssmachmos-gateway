// Sensor details screen: a form to rename a sensor, set its wake-up timing and
// per-measurement settings, then save or forget it.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';
import 'package:provider/provider.dart';
import 'package:ss_machmos_gui/connection.dart';
import 'package:ss_machmos_gui/sensors.dart';
import 'package:ss_machmos_gui/utils.dart';

/// Shows the settings form for the sensor that is currently selected.
class SensorDetails extends StatefulWidget {
  const SensorDetails({super.key});

  /// Creates the state that holds the form values.
  @override
  State<SensorDetails> createState() => _SensorDetailsState();
}

/// Holds the form values for SensorDetails and handles Save and Forget.
class _SensorDetailsState extends State<SensorDetails> {
  // NOTE: There's a mix of controllers and provider state management. This is
  // becuase I can't access the state in the subwidgets without specifying
  // everything by hand
  // The key lets onSave ask the form to validate itself.
  final _formKey = GlobalKey<FormState>();
  // BUG: name, wakeupInterval and wakeupOffset are never disposed (there is no dispose method).
  late TextEditingController name;
  late bool deviceActive;
  late TextEditingController wakeupInterval;
  late TextEditingController wakeupOffset;
  // Widgets for the per-measurement settings. Rebuilt on every build().
  final List<Widget> configs = [];

  /// Fills the form with the values of the selected sensor.
  @override
  void initState() {
    super.initState();
    // BUG: values are copied only once here, so the form can show an old sensor's values if the selected sensor changes.
    // read (not watch) is used because initState runs only once.
    Sensor initSensor = context.read<Connection>().displayedSensor!;
    name = TextEditingController(text: initSensor.name);
    deviceActive = initSensor.deviceActive;
    wakeupInterval =
        TextEditingController(text: initSensor.wakeUpInterval.toString());
    wakeupOffset = TextEditingController(
        text: initSensor.wakeUpIntervalMaxOffset.toString());
  }

  // Clear all to initial
  /// Forgets the displayed sensor.
  void onForget(BuildContext context) {
    Connection conn = context.read<Connection>();
    // The callback runs when the request ends, and err is null if it worked.
    // BUG: nothing is shown when err is not null, so the user gets no sign that forgetting failed.
    conn.forgetSensor(conn.displayedSensor!, (_, err) {
      if (err == null) {
        showMessage("Sensor removed", context);
      }
      return false;
    });
    return;
  }

  // Reset to defaults
  //void onReset() {}

  // Send settings to backend
  /// Checks the form, copies the values to the sensor, and asks the backend to save them.
  void onSave(BuildContext context) {
    // Verify offsets are integers
    // collect sub sensor settings and verify those are also integers
    // BUG: the fields have no validators, so validate() always passes and the ! below crashes on text that is not a number.
    if (!_formKey.currentState!.validate()) {
      return;
    }
    Sensor sensor = context.read<Connection>().displayedSensor!;

    sensor.name = name.text;
    sensor.deviceActive = deviceActive;
    // BUG: the Help page says the interval must be greater than the offset, but nothing checks this.
    sensor.wakeUpInterval = int.tryParse(wakeupInterval.text)!;
    sensor.wakeUpIntervalMaxOffset = int.tryParse(wakeupOffset.text)!;

    Connection conn = context.read<Connection>();
    // BUG: no message is shown when saving fails.
    conn.saveSensor(conn.displayedSensor!, (_, err) {
      if (err == null) {
        showMessage("Successfully saved sensor", context);
      }
      return true;
    });
  }

  /// Builds the form for the displayed sensor.
  @override
  Widget build(BuildContext context) {
    // watch (not read) so the form rebuilds when the sensor data changes.
    Sensor sensor = context.watch<Connection>().displayedSensor!;
    // Sensors with singlePoint (such as temperature) only need an on/off box.
    // The other types also need a frequency and a duration.
    // BUG: the settings below change the sensor object right away, even if the user never presses Save.
    configs.clear();
    configs.addAll(sensor.model.sensorsAvailable.entries.map((entry) => entry.value.singlePoint
        ? SensorConfigSingleSample(
            specific: entry.key,
            value: sensor.settings[entry.key]!.active,
            onActivate: (enable) =>
                setState(() => sensor.settings[entry.key]!.active = enable!),
          )
        : SensorConfigMultipleSamples(sensor: sensor, specific: entry.key)));

    // Expanded fills the space left in the parent, so this widget must be placed inside a Column or Row.
    return Expanded(
        child: SingleChildScrollView(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 32, vertical: 8),
        child: Form(
          key: _formKey,
          child: Column(
            // spacing needs a recent Flutter version (3.27 or newer).
            spacing: 8,
            //crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              DividerText("Sensor configuration"),
              Row(spacing: 8, children: [
                Expanded(
                    // flex 2: the name needs more room than the checkbox.
                    flex: 2,
                    child: TextFormField(
                        controller: name,
                        decoration: InputDecoration(labelText: "Name"))),
                Expanded(
                    child: CheckboxListTile(
                        contentPadding: EdgeInsets.zero,
                        controlAffinity: ListTileControlAffinity.leading,
                        title: Text("Device active?"),
                        value: deviceActive,
                        onChanged: (value) =>
                            setState(() => deviceActive = value!))),
                // Asks for a measurement now, without waiting for the next wake-up.
                TextButton(
                    child: const Text("Collect now"),
                    onPressed: () =>
                        context.read<Connection>().collectFromSensor(sensor)),
              ]),
              Text("Types: ${sensor.types.join(' | ')}"),
              Row(spacing: 8, children: [
                Expanded(
                    child: TextFormField(
                        controller: wakeupInterval,
                        decoration: InputDecoration(
                            labelText: "Wake-Up Interval",
                            suffixText: "seconds"))),
                Expanded(
                    child: TextFormField(
                        controller: wakeupOffset,
                        decoration: InputDecoration(
                            labelText: "Wake-up interval offset",
                            suffixText: "seconds"))),
              ]),
              Text("Predicted next wake-up: ${sensor.predictedWakeupTime}"),
              DividerText("Individual sensor configurations"),
              Text("Memory usage: ${sensor.memoryUsage}/${sensor.model.maxCapacity} bytes"),
              ...configs,
              Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  spacing: 8,
                  children: [
                    TextButton(
                        onPressed: () => onForget(context),
                        child: Text("Forget Sensor")),
                    TextButton(
                        onPressed: () => onSave(context), child: Text("Save")),
                  ])
            ],
          ),
        ),
      ),
    ));
  }
}

/// Shows one checkbox to turn a single-value measurement on or off.
class SensorConfigSingleSample extends StatelessWidget {
  final String specific;
  // Called when the box is ticked. The parent updates the sensor and rebuilds.
  final Function(bool?) onActivate;
  final bool value;

  const SensorConfigSingleSample(
      {super.key,
      required this.specific,
      required this.value,
      required this.onActivate});

  /// Builds the checkbox with the measurement name as its title.
  @override
  Widget build(BuildContext context) {
    return Container(
        padding: EdgeInsets.symmetric(horizontal: 8, vertical: 0),
        child: CheckboxListTile(
          controlAffinity: ListTileControlAffinity.leading,
          value: value,
          title: Text(toBeginningOfSentenceCase(specific),
              style: TextStyle(fontWeight: FontWeight.bold)),
          subtitle: Text("Activate on next collection?"),
          onChanged: onActivate,
        ));
  }
}

/// Shows the settings for a measurement taken over time: on/off, frequency and duration.
class SensorConfigMultipleSamples extends StatefulWidget {
  final String specific;
  final Sensor sensor;
  const SensorConfigMultipleSamples(
      {super.key, required this.sensor, required this.specific});

  /// Creates the state, which only exists so setState can rebuild this row.
  @override
  State<SensorConfigMultipleSamples> createState() =>
      _SensorConfigMultipleSamplesState();
}

/// Builds the settings row and rebuilds it when a value changes.
class _SensorConfigMultipleSamplesState
    extends State<SensorConfigMultipleSamples> {
  _SensorConfigMultipleSamplesState();

  /// Builds a row with the checkbox, the frequency dropdown and the duration field.
  @override
  Widget build(BuildContext context) {
    // display is the sensor's own settings object, so edits change it directly.
    SensorSettings display = widget.sensor.settings[widget.specific]!;
    return Container(
        padding: EdgeInsets.symmetric(horizontal: 8, vertical: 0),
        child: Row(
          //spacing: 16,
          children: [
            Expanded(
              // flex 2: the checkbox and its two lines of text need more room.
              flex: 2,
                child: CheckboxListTile(
                    controlAffinity: ListTileControlAffinity.leading,
                    value: display.active,
                    title: Text(toBeginningOfSentenceCase(widget.specific),
                        style: TextStyle(fontWeight: FontWeight.bold)),
                    subtitle: Text("Activate on next collection?"),
                    // Make sure to use setState to rebuild widgets
                    onChanged: (value) =>
                        setState(() => display.active = value!))),
            Expanded(
                child: FrequencySelectionDropdown(
                    freqs:
                        widget.sensor.samplingFreqsForSetting(widget.specific),
                    // No choice (null) is stored as 0.
                    onSelected: (val) =>
                        setState(() => display.samplingFrequency = val ?? 0),
                    initialSelection: display.samplingFrequency)),
            Expanded(
                child: TextFormField(
              initialValue: display.samplingDuration.toString(),
              // An empty or invalid value is stored as 0.
              onChanged: (value) => setState(
                  () => display.samplingDuration = int.tryParse(value) ?? 0),
              decoration: InputDecoration(
                  labelText: "Sampling duration", suffixText: "seconds"),
              // Only digits are allowed, so int.tryParse above works.
              inputFormatters: [FilteringTextInputFormatter.digitsOnly],
            )),
          ],
        ));
  }
}

/// Shows a dropdown to pick the sampling frequency.
class FrequencySelectionDropdown extends StatelessWidget {
  final List<int> freqs;
  final Function(int?) onSelected;
  final int initialSelection;

  const FrequencySelectionDropdown(
      {super.key,
      required this.freqs,
      required this.onSelected,
      required this.initialSelection});

  /// Builds the dropdown with one entry per available frequency.
  @override
  Widget build(BuildContext context) {
    return DropdownMenu<int>(
        label: Text("Max detectable frequency"),
        onSelected: onSelected,
        // BUG: ignores initialSelection, so the dropdown always starts on the highest frequency.
        // BUG: freqs.last crashes if the list is empty.
        initialSelection: freqs.last,
        dropdownMenuEntries: freqs
            // Each value is a sampling rate. The highest frequency a sensor can
            // detect is half of that, so the label shows x/2.
            // BUG: x/2 is a decimal in Dart, so the label reads like "500.0 Hz".
            .map((x) => DropdownMenuEntry<int>(value: x, label: "${x/2} Hz"))
            .toList());
  }
}

/// Shows a bold label with a line on each side, to separate parts of the form.
class DividerText extends StatelessWidget {
  final String label;
  const DividerText(this.label, {super.key});

  /// Builds the label between two dividers.
  @override
  Widget build(BuildContext context) =>
      Row(spacing: 4, crossAxisAlignment: CrossAxisAlignment.center, children: [
        Expanded(child: Divider()),
        Text(label,
            style: TextStyle(
                fontWeight: FontWeight.bold,
                // 15 is the base size, scaled by the user's text size setting.
                fontSize: MediaQuery.textScalerOf(context).scale(15))),
        // BUG: this extra Divider is not inside Expanded, so it probably draws nothing.
        Divider(),
        Expanded(child: Divider())
      ]);
}
