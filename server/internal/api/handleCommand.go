// Package api lets the CLI and the GUI control the gateway.
// This file has the helper functions that do the work for some commands in api.go.
package api

import (
	"encoding/json"
	"errors"

	"github.com/jukuly/ss_machmos/server/internal/model"
	"github.com/jukuly/ss_machmos/server/internal/server"
)

// pairEnable lets new sensors ask to join (PAIR-ENABLE).
func pairEnable() {
	server.EnablePairing()
}

// pairDisable stops new sensors from asking to join (PAIR-DISABLE).
func pairDisable() {
	server.DisablePairing()
}

// pairAccept approves the waiting sensor with this address (PAIR-ACCEPT).
// BUG: always returns nil, so it says OK even if pairing is off or the sensor isn't waiting.
func pairAccept(mac string) error {
	m, err := model.StringToMac(mac)
	if err != nil {
		return err
	}
	server.Pair(m)
	return nil
}

// pairListPending returns the sensors waiting for approval, as JSON (PAIR-LIST).
func pairListPending() (string, error) {
	jsonStr, err := json.Marshal(server.ListDevicesPendingPairing())
	return string(jsonStr), err
}

// list returns all approved sensors and their settings, as JSON (LIST).
func list() (string, error) {
	jsonStr, err := json.Marshal(model.Sensors)
	return string(jsonStr), err
}

// listConnected returns each sensor and whether it's connected right now, as JSON (LIST-CONNECTED).
func listConnected() (string, error) {
	res, err := json.Marshal(server.ConnectedDevices())
	return string(res), err
}

// deviceCollect asks one sensor to start measuring now (COLLECT).
// BUG: never checks the address is valid, so it always returns nil.
func deviceCollect(address string) error {
	server.TriggerCollection(address)
	return nil
}

// pendingUploads returns how many uploads failed and are waiting to retry, as JSON (LIST-PENDING-UPLOADS).
func pendingUploads() (string, error) {
	pending := server.PendingUploads()
	// anonymous struct yay
	res, err := json.Marshal(struct {
		Count   int                      `json:"count"`
		Pending []server.UnsentDataError `json:"pending"`
	}{
		Count:   len(pending),
		Pending: pending,
	})
	return string(res), err
}

// view returns one sensor's details, as JSON (VIEW).
func view(mac string) (string, error) {
	for _, sensor := range model.Sensors {
		if sensor.IsMacEqual(mac) {
			jsonStr, err := json.Marshal(sensor)
			return string(jsonStr), err
		}
	}
	return "", errors.New("Sensor with MAC address " + mac + " not found")
}

// forget removes a sensor from the approved list (FORGET).
// BUG: says OK even if no sensor has that address.
func forget(mac string) error {
	m, err := model.StringToMac(mac)
	if err != nil {
		return err
	}
	err = model.RemoveSensor(m)
	if err != nil {
		return err
	}
	return nil
}

// getGateway returns the gateway's settings, as JSON (GET-GATEWAY).
// BUG: this includes the password, so any CLI/GUI user can read it.
func getGateway() (string, error) {
	jsonStr, err := json.Marshal(*server.Gateway)
	return string(jsonStr), err
}

// stop shuts down the gateway (STOP). The program exits right away.
func stop() {
	server.StopAdvertising()
}