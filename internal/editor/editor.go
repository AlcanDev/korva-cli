// Package editor configures AI editors and CLIs to use the Korva remote MCP
// server. Each supported target (VS Code, Claude Code, Claude Desktop, Cursor,
// Windsurf) is represented by a Target value with the platform-specific path
// to its MCP config file and the JSON schema key it expects.
package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ServerName is the name under which the Korva MCP server is registered in
// every target's config file.
const ServerName = "korva"

// Target represents one editor/CLI that can be wired to the Korva MCP server.
type Target struct {
	// Name is the stable kebab-case identifier used on the CLI (--target).
	Name string
	// DisplayName is the human-friendly name printed in messages.
	DisplayName string
	// schemaKey is the top-level JSON key used by this target to hold MCP
	// server definitions. VS Code uses "servers"; every other target uses
	// "mcpServers".
	schemaKey string
	// pathFn returns the absolute path to the target's config file on the
	// current OS, or an error if the platform is unsupported.
	pathFn func() (string, error)
	// detectFn reports whether the target appears to be installed. Default
	// is "config file or its parent directory exists".
	detectFn func() bool
}

// ConfigPath returns the absolute path to the target's MCP config file.
func (t Target) ConfigPath() (string, error) { return t.pathFn() }

// Detected reports whether the target appears installed on this machine.
// Always false on unsupported platforms.
func (t Target) Detected() bool {
	if t.detectFn != nil {
		return t.detectFn()
	}
	return defaultDetect(t.pathFn)
}

// Configure upserts the Korva MCP server into this target's config file,
// preserving any other entries and any unrelated keys in the file.
func (t Target) Configure(serverURL, token string) error {
	path, err := t.pathFn()
	if err != nil {
		return fmt.Errorf("%s: %w", t.DisplayName, err)
	}
	return writeServer(path, t.schemaKey, ServerName, mcpServerPayload(serverURL, token))
}

// Remove deletes the Korva entry from this target's config file. A missing
// file is not an error.
func (t Target) Remove() error {
	path, err := t.pathFn()
	if err != nil {
		return fmt.Errorf("%s: %w", t.DisplayName, err)
	}
	return removeServer(path, t.schemaKey, ServerName)
}

// WriteServer upserts the Korva MCP entry into path using the schema of t.
// This is the escape hatch used by `korva setup --path` when an editor is
// installed in a non-standard location: the file is written at the caller's
// path while the JSON shape still matches the target.
func WriteServer(path string, t Target, serverURL, token string) error {
	return writeServer(path, t.schemaKey, ServerName, mcpServerPayload(serverURL, token))
}

// All returns the registered targets in a stable order.
func All() []Target {
	out := make([]Target, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ByName looks up a target by its --target identifier. Aliases like "claude"
// resolve to "claude-code".
func ByName(name string) (Target, error) {
	if alias, ok := aliases[name]; ok {
		name = alias
	}
	if t, ok := registry[name]; ok {
		return t, nil
	}
	return Target{}, fmt.Errorf("unknown target %q (known: %s)", name, knownNames())
}

// Detected returns the subset of All() that appears installed.
func Detected() []Target {
	out := make([]Target, 0)
	for _, t := range All() {
		if t.Detected() {
			out = append(out, t)
		}
	}
	return out
}

func knownNames() string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// mcpServerPayload returns the body used to describe the Korva MCP server.
// All supported targets accept the same shape: an HTTP server at <baseURL>/mcp
// with a Bearer Authorization header.
func mcpServerPayload(serverURL, token string) map[string]any {
	return map[string]any{
		"type": "http",
		"url":  strings.TrimRight(serverURL, "/") + "/mcp",
		"headers": map[string]string{
			"Authorization": "Bearer " + token,
		},
	}
}

// writeServer upserts payload at root[schemaKey][name] preserving every other
// key in the file (including unrelated top-level keys, which is critical for
// files like ~/.claude.json that hold much more than MCP config).
func writeServer(path, schemaKey, name string, payload map[string]any) error {
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers, _ := root[schemaKey].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = payload
	root[schemaKey] = servers

	return writeJSONObject(path, root)
}

func removeServer(path, schemaKey, name string) error {
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers, ok := root[schemaKey].(map[string]any)
	if !ok || len(servers) == 0 {
		return nil
	}
	if _, present := servers[name]; !present {
		return nil
	}
	delete(servers, name)
	if len(servers) == 0 {
		delete(root, schemaKey)
	} else {
		root[schemaKey] = servers
	}
	return writeJSONObject(path, root)
}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		// fall through
	case errors.Is(err, os.ErrNotExist):
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out := map[string]any{}
	if len(data) > 0 {
		// Best-effort: a malformed file is treated as empty so a corrupt
		// config never blocks `korva setup`. The unit test
		// TestConfigureRecoversFromCorruptFile pins this behavior.
		_ = json.Unmarshal(data, &out)
	}
	return out, nil
}

func writeJSONObject(path string, root map[string]any) error {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

func defaultDetect(pathFn func() (string, error)) bool {
	path, err := pathFn()
	if err != nil {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	_, err = os.Stat(filepath.Dir(path))
	return err == nil
}

// envOrEmpty returns os.Getenv(name) (kept as a thin wrapper so tests can
// inject env via t.Setenv).
func envOrEmpty(name string) string { return os.Getenv(name) }

// currentGOOS is overridable in tests for path resolution that branches on OS.
var currentGOOS = func() string { return runtime.GOOS }

func homeDir() (string, error) {
	if h := envOrEmpty("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}
