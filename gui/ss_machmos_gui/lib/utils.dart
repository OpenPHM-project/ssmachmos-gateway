// Helper functions shared by the screens.

import 'package:flutter/material.dart';

/// Shows a short message at the bottom of the screen.
// BUG: this crashes if the context is no longer in the widget tree, which can happen when it is called after a delay (as in the save callbacks).
// The 4 seconds is Flutter's own default for a SnackBar.
void showMessage(String message, BuildContext context, {Duration duration = const Duration(seconds: 4)}) {
  // Messages wait in a queue, so several calls show one after another.
  ScaffoldMessenger.of(context).showSnackBar(
    SnackBar(
      content: Text(message),
      duration: duration,
    ),
  );
}
