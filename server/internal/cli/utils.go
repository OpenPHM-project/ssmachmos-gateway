// package cli provides helper utilities for parsing and formatting CLI data structures.
package cli

import (
	"encoding/json"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

// sensorJSONToString unmarshals raw JSON byte slices into a Sensor model and returns its formatted string representation.
func sensorJSONToString(jsonStr []byte) (string, error) {
	s := model.Sensor{}
	err := json.Unmarshal(jsonStr, &s)
	if err != nil {
		return "", err
	}

	return s.ToString(), nil
}