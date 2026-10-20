// Logs tab: shows the server's log lines in a scrolling box, newest at the bottom.

import 'package:flutter/material.dart';

/// Shows the server logs in a scrolling box where the text can be selected.
class Logs extends StatelessWidget {
  const Logs({
    super.key,
    // required ScrollController logsScrollController,
    required this.logs,
  });
  // The log lines, oldest first.
  final List<String> logs;
  // static so the same controller is kept when this widget is rebuilt.
  // BUG: every Logs widget shares it, so showing two at once would fail.
  // BUG: it is never disposed.
  static final ScrollController _logsScrollController = ScrollController();

  /// Builds the bordered box with the log lines.
  @override
  Widget build(BuildContext context) {
    //String logs = context.watch<Connection>().logs;
    return Stack(
      children: [
        Container(
          decoration: BoxDecoration(
            // 0.5 makes a thin line around the box.
            border: Border.all(color: Colors.grey, width: 0.5),
            borderRadius: BorderRadius.circular(10),
          ),
          margin: const EdgeInsets.all(32),
          padding: const EdgeInsets.all(16),
          // Keeps a short list at the bottom, like a longer one.
          alignment: Alignment.bottomLeft,
          // Lets the user select and copy log text.
          child: SelectionArea(
            child: ListView.builder(
              itemCount: logs.length,
              // reverse starts the list at the bottom, so new lines show up there.
              reverse: true,
              //prototypeItem: Text(logs.last),
              controller: _logsScrollController,
              // With reverse, index 0 is the bottom line, so the newest log is read first.
              // BUG: trim() is called twice, and the second call does nothing.
              itemBuilder: (context, index) => Text(logs[logs.length - 1 - index].trim().trim()),
            ),
          ),
        ),
        // OUT OF DATE: this Stop button is commented out. To use it again, the provider and connection imports are needed.
        // BUG: the Help page says the Logs tab has a stop button. Check that one exists elsewhere, or update the Help page.
        /*
        Positioned(
          top: 48.5,
          right: 48.5,
          child: TextButton(
            style: const ButtonStyle(
              backgroundColor: WidgetStatePropertyAll(Colors.red),
            ),
            onPressed: () {
              showDialog(
                context: context,
                builder: (BuildContext context) {
                  return AlertDialog(
                    title: const Text(
                      "Stop the server",
                    ),
                    actions: [
                      TextButton(
                        onPressed: () {
                          Navigator.of(context).pop();
                        },
                        child: const Text("Cancel"),
                      ),
                      TextButton(
                        onPressed: () {
                          context.read<Connection>().stopServer();
                          //_connection.send("STOP");
                          Navigator.of(context).pop();
                        },
                        child: const Text("Confirm"),
                      ),
                    ],
                  );
                },
              );
            },
            child: const Text("Stop",
                style: TextStyle(
                  color: Colors.white,
                )),
          ),
        )
        */
      ],
    );
  }
}
