// Package config persists the Korva CLI's local state: the backbone server
// URL and the API token obtained from `korva login`.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultServerURL is the backbone URL used when none is configured.
const DefaultServerURL = "http://localhost:8080"

// Config is the CLI's persisted state.
type Config struct {
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
}

// Home returns the Korva config directory, honoring KORVA_HOME.
func Home() (string, error) {
	if h := os.Getenv("KORVA_HOME"); h != "" {
		return h, nil
	}
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(appData, "korva"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".korva"), nil
}

// Path returns the absolute path of the CLI config file.
func Path() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "config.json"), nil
}

// Load reads the config file. A missing file yields a zero Config (not an error).
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes the config file with owner-only permissions.
func Save(cfg Config) error {
	home, err := Home()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(home, "config.json")
	return os.WriteFile(path, data, 0o600)
}

// LoggedIn reports whether a token is stored.
func (c Config) LoggedIn() bool {
	return c.Token != ""
}
