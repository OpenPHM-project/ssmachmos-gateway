package model

import (
	"os"
	"path"
	"testing"
)

// TestLoadSettingsBrokenFile checks that if gateway.json is broken,
// LoadSettings returns an error and empties the gateway's settings
// instead of keeping the old ones.
func TestLoadSettingsBrokenFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(path.Join(dir, "ss_machmos"), 0755)
	os.WriteFile(path.Join(dir, "ss_machmos", "gateway.json"), []byte("not json"), 0600)

	gateway := Gateway{Id: "old"}
	err := LoadSettings(&gateway, "gateway.json")

	if err == nil {
		t.Fatal("expected an error, got", nil)

	}
	if gateway.Id != "" {
		t.Fatal("Gateway's settings expected to be empty, but gateway settings is full")
	}
}
