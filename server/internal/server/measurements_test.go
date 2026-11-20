package server

import (
	"os"
	"path"
	"testing"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

func TestSendMeasurements(t *testing.T) {
	_, err := sendMeasurements([]byte("[]"), &model.Gateway{HTTPEndpoint: "http://127.0.0.1:1"})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestBadFileMovedToBadData(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // 1. empty cache folder for this test

	name := "bad.json"

	os.WriteFile(path.Join(unsentDataDir(), name), []byte("not json"), 0644) // 2. put an broken file in unsent_data

	sendUnsentMeasurements() // 3. run the function

	_, err := os.Stat(path.Join(badDataDir(), name)) // 4. does the file exist in the new folder?
	if err != nil {
		t.Fatal("expected file in bad_data, got", err)
	}
}
