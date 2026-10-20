// Gateway tab: a form to set the gateway ID, password and HTTP endpoint,
// then save and test them.

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'package:ss_machmos_gui/connection.dart';
import 'package:ss_machmos_gui/utils.dart';

/// Shows the form with the gateway login and endpoint settings.
class GatewayView extends StatefulWidget {
  const GatewayView({super.key});

  /// Creates the state that holds the form values.
  @override
  State<GatewayView> createState() => _GatewayViewState();
}

/// Holds the form values for GatewayView and handles saving them.
class _GatewayViewState extends State<GatewayView> {
  // FormState is a flutter class
  // The key lets submitGateway ask the form to validate itself.
  final _formKey = GlobalKey<FormState>();

  // late because they need the context, so they are created in initState.
  late TextEditingController _idController;
  late TextEditingController _passwordController;
  late TextEditingController _httpController;

  /// Frees the text controllers when the screen is closed.
  @override
  void dispose() {
    _idController.dispose();
    _passwordController.dispose();
    _httpController.dispose();
    super.dispose();
  }

  /// Fills the form with the gateway values that are currently saved.
  @override
  void initState() {
    // Default values on init state
    // read (not watch) is used because initState runs only once.
    Connection conn = context.read<Connection>();
    _idController = TextEditingController(text: conn.gateway.id);
    _passwordController = TextEditingController(text: conn.gateway.password);
    _httpController = TextEditingController(text: conn.gateway.httpEndpoint);
    // BUG: super.initState() should be called first, not last.
    super.initState();
  }

  /// Checks the form, sends each setting to the backend, then tests the gateway.
  void submitGateway() async {
    // validate() runs the validators below and shows their error messages.
    if (_formKey.currentState!.validate()) {
      Connection conn = context.read<Connection>();
      // Form data is valid, send to backend
      // Each setting is sent as its own request.
      // The success messages are turned off (see the commented-out code), so only failures are shown.
      // BUG: a callback may run after this screen is gone, and there is no mounted check before using context.
      conn.setGatewayID(_idController.text, (_, err) {
        if (err != null) {
          showMessage("Failed to save Gateway ID", context);
        } // else {
        //   showMessage("Gateway ID saved", context,
        //       duration: Duration(seconds: 1));
        // }
        return true;
      });

      conn.setGatewayPassword(_passwordController.text,
          (_, err) {
        if (err != null) {
          showMessage("Failed to save Gateway Password", context);
        } // else {
        //   showMessage("Gateway Password saved", context,
        //       duration: Duration(seconds: 1));
        // }
        return true;
      });

      conn.setGatewayHttpEndpoint(_httpController.text,
          (_, err) {
        if (err != null) {
          showMessage("Failed to save Gateway HTTP Endpoint", context);
        } // else {
        //   showMessage("Gateway HTTP Endpoint saved", context,
        //       duration: Duration(seconds: 1));
        // }
        return true;
      });

      // Checks that the login and endpoint work.
      // BUG: this is sent without waiting for the saves above to finish, so it may test the old values.
      conn.testGateway((_, err) {
          if (err != null) {
            print(err);
            showMessage("Gateway invalid: $err", context);
          }
          return false;
      });
    }
  }

  /// Builds the form with the three fields and the Save button.
  @override
  Widget build(BuildContext context) {
    return Form(
      key: _formKey,
      child: Center(
        child: SizedBox(
          // 450 keeps the form narrow and easy to read in a wide window.
          width: 450,
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              TextFormField(
                  controller: _idController,
                  decoration: const InputDecoration(labelText: "Gateway ID"),
                  validator: (value) {
                    if (value == null || value.isEmpty) {
                      return "Missing gateway login ID";
                    }
                    return null;
                  }),
              const SizedBox(height: 10),
              TextFormField(
                  controller: _passwordController,
                  // Hide the password while typing.
                  obscureText: true,
                  // Off so the password is not kept in keyboard suggestions or changed by autocorrect.
                  enableSuggestions: false,
                  autocorrect: false,
                  decoration:
                      const InputDecoration(labelText: "Gateway password"),
                  validator: (value) {
                    if (value == null || value.isEmpty) {
                      return "Missing gateway login password";
                    }
                    return null;
                  }),
              const SizedBox(height: 10),
              // BUG: this field has no validator, so an empty or invalid endpoint can be saved.
              TextFormField(
                  controller: _httpController,
                  decoration: InputDecoration(
                      labelText: "HTTP Endpoint",
                      suffixIcon: IconButton(
                        icon: Icon(Icons.restore),
                        tooltip: "Restore http endpoint to default value",
                        onPressed: () {
                          // Replace endpoint with default
                          // Only fills the field. The user still has to press Save.
                          // This is the openMachMoS cloud address. If the default changes, change it here too.
                          _httpController.text =
                              "https://openphm.org/gateway_data";
                        },
                      ))),
              const SizedBox(height: 10),
              Align(
                alignment: Alignment.centerRight,
                // Save settings button
                child: TextButton(
                    onPressed: submitGateway, child: const Text("Save")),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
