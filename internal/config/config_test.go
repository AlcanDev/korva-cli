package config

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Setenv("KORVA_HOME", t.TempDir())

	want := Config{ServerURL: "https://api.korva.dev", Token: "tok-123"}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
	if !got.LoggedIn() {
		t.Error("LoggedIn = false, want true")
	}
}

func TestLoadMissingFileIsZero(t *testing.T) {
	t.Setenv("KORVA_HOME", t.TempDir())

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != (Config{}) {
		t.Errorf("Load of missing file = %+v, want zero Config", got)
	}
	if got.LoggedIn() {
		t.Error("LoggedIn = true for empty config")
	}
}

func TestPathInsideHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KORVA_HOME", dir)

	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if path != filepath.Join(dir, "config.json") {
		t.Errorf("Path = %q, want %q", path, filepath.Join(dir, "config.json"))
	}
}
