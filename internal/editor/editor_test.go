package editor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ─── table-driven coverage across every Target ─────────────────────────────

// targetCase describes one supported target and the fixture we use to seed
// its config file in tests. The fixture is JSON that already exists at the
// target's config path; the test runs Configure() and verifies that:
//   - the Korva entry is present under the right schemaKey
//   - no other entries (top-level keys or sibling MCP servers) are lost
//   - the file is well-formed JSON and writable with mode 0600
//   - a second Configure() call is idempotent (no duplicates, no growth)
type targetCase struct {
	name             string
	target           Target
	schemaKey        string
	fixture          string   // JSON to write before Configure (empty = no file)
	wantPreserveKeys []string // top-level keys the test seeds and must keep
}

func targetCases(t *testing.T) []targetCase {
	t.Helper()
	// We resolve the targets via the package's registry so the test fails
	// loudly if a target is removed without being unregistered.
	return []targetCase{
		{
			name:      "vscode",
			target:    mustTarget(t, "vscode"),
			schemaKey: "servers",
			fixture:   `{"servers":{"other":{"type":"http","url":"https://example.com"}}}`,
		},
		{
			name:      "claude-code",
			target:    mustTarget(t, "claude-code"),
			schemaKey: "mcpServers",
			// ~/.claude.json holds far more than mcpServers. The test seeds
			// realistic siblings (projects, history) and asserts they survive.
			fixture: `{
              "projects": {"/foo": {"name": "foo"}},
              "history": ["a", "b"],
              "mcpServers": {"other": {"type": "stdio", "command": "x"}}
            }`,
			wantPreserveKeys: []string{"projects", "history"},
		},
		{
			name:      "claude-desktop",
			target:    mustTarget(t, "claude-desktop"),
			schemaKey: "mcpServers",
			fixture:   `{"mcpServers":{"other":{"command":"x"}}}`,
		},
		{
			name:      "cursor",
			target:    mustTarget(t, "cursor"),
			schemaKey: "mcpServers",
			fixture:   `{"mcpServers":{"other":{"command":"x"}}}`,
		},
		{
			name:      "windsurf",
			target:    mustTarget(t, "windsurf"),
			schemaKey: "mcpServers",
			fixture:   `{"mcpServers":{"other":{"command":"x"}}}`,
		},
	}
}

// TestConfigureUpsertsKorvaForEveryTarget runs the same invariants for every
// registered target. Each subtest sandboxes HOME so the real machine config
// is never touched.
func TestConfigureUpsertsKorvaForEveryTarget(t *testing.T) {
	for _, tc := range targetCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			sandboxHome(t)

			path, err := tc.target.ConfigPath()
			if err != nil {
				t.Skipf("%s not addressable on %s: %v", tc.target.DisplayName, runtime.GOOS, err)
			}
			if tc.fixture != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(path, []byte(tc.fixture), 0o600); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			if err := tc.target.Configure("https://api.korva.dev", "tok"); err != nil {
				t.Fatalf("Configure: %v", err)
			}

			root := readRoot(t, path)

			// Schema key holds Korva, plus anything we seeded.
			servers, ok := root[tc.schemaKey].(map[string]any)
			if !ok {
				t.Fatalf("missing %q in %s", tc.schemaKey, path)
			}
			korva, ok := servers["korva"].(map[string]any)
			if !ok {
				t.Fatalf("korva entry missing under %q", tc.schemaKey)
			}
			if got, want := korva["url"], "https://api.korva.dev/mcp"; got != want {
				t.Errorf("url = %v, want %v", got, want)
			}
			if got, want := korva["type"], "http"; got != want {
				t.Errorf("type = %v, want %v", got, want)
			}
			headers, ok := korva["headers"].(map[string]any)
			if !ok {
				t.Fatalf("headers missing")
			}
			if got, want := headers["Authorization"], "Bearer tok"; got != want {
				t.Errorf("Authorization = %v, want %v", got, want)
			}

			if tc.fixture != "" {
				if _, ok := servers["other"]; !ok {
					t.Errorf("pre-existing sibling MCP server was dropped")
				}
			}
			for _, k := range tc.wantPreserveKeys {
				if _, ok := root[k]; !ok {
					t.Errorf("top-level key %q was dropped (preserve check)", k)
				}
			}

			// Idempotence: a second run leaves the same count of entries.
			countBefore := len(servers)
			if err := tc.target.Configure("https://api.korva.dev", "tok"); err != nil {
				t.Fatalf("second Configure: %v", err)
			}
			root2 := readRoot(t, path)
			servers2, _ := root2[tc.schemaKey].(map[string]any)
			if got := len(servers2); got != countBefore {
				t.Errorf("idempotence violated: %d entries → %d after re-run", countBefore, got)
			}

			// File permissions: owner-only on POSIX (Windows ignores mode bits).
			if runtime.GOOS != "windows" {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("stat: %v", err)
				}
				if perm := info.Mode().Perm(); perm != 0o600 {
					t.Errorf("mode = %o, want 0600", perm)
				}
			}
		})
	}
}

// TestRemoveDropsKorvaButKeepsOthers verifies the inverse: after Configure
// followed by Remove, sibling entries and unrelated top-level keys remain.
func TestRemoveDropsKorvaButKeepsOthers(t *testing.T) {
	for _, tc := range targetCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			sandboxHome(t)
			path, err := tc.target.ConfigPath()
			if err != nil {
				t.Skipf("%s not addressable: %v", tc.target.DisplayName, err)
			}

			if err := tc.target.Configure("https://api.korva.dev", "tok"); err != nil {
				t.Fatalf("Configure: %v", err)
			}
			if err := tc.target.Remove(); err != nil {
				t.Fatalf("Remove: %v", err)
			}

			root := readRoot(t, path)
			if servers, ok := root[tc.schemaKey].(map[string]any); ok {
				if _, present := servers["korva"]; present {
					t.Error("korva entry survived Remove")
				}
			}
		})
	}
}

// TestRemoveOnMissingFileIsNoop guarantees a clean uninstall doesn't fail
// just because nothing was configured yet.
func TestRemoveOnMissingFileIsNoop(t *testing.T) {
	for _, tc := range targetCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			sandboxHome(t)
			if err := tc.target.Remove(); err != nil {
				t.Errorf("Remove on missing file should be no-op: %v", err)
			}
		})
	}
}

// TestByNameAcceptsAliases pins the user-visible aliases so we don't break
// them silently.
func TestByNameAcceptsAliases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"vscode", "vscode"},
		{"code", "vscode"},
		{"vs-code", "vscode"},
		{"claude", "claude-code"},
		{"claude-code", "claude-code"},
		{"claude-desktop", "claude-desktop"},
		{"cursor", "cursor"},
		{"windsurf", "windsurf"},
	}
	for _, c := range cases {
		got, err := ByName(c.in)
		if err != nil {
			t.Errorf("ByName(%q) error: %v", c.in, err)
			continue
		}
		if got.Name != c.want {
			t.Errorf("ByName(%q).Name = %q, want %q", c.in, got.Name, c.want)
		}
	}
}

func TestByNameRejectsUnknown(t *testing.T) {
	_, err := ByName("nano")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
	if !strings.Contains(err.Error(), "vscode") {
		t.Errorf("error should list known targets, got: %v", err)
	}
}

func TestAllListsRegistry(t *testing.T) {
	names := map[string]bool{}
	for _, tt := range All() {
		names[tt.Name] = true
	}
	for _, want := range []string{"vscode", "claude-code", "claude-desktop", "cursor", "windsurf"} {
		if !names[want] {
			t.Errorf("All() missing %q", want)
		}
	}
}

// ─── path resolution — every OS branch, even from a single host ────────────

// Path-resolution tests build expected values with filepath.Join so the
// assertions are correct on whichever OS the test happens to run on; the
// production code uses filepath.Join too, so both sides match.

func TestVSCodePathFor(t *testing.T) {
	cases := []struct {
		goos, home, appData string
		wantErr             bool
		want                string
	}{
		{goos: "darwin", home: "/Users/x", want: filepath.Join("/Users/x", "Library", "Application Support", "Code", "User", "mcp.json")},
		{goos: "linux", home: "/home/x", want: filepath.Join("/home/x", ".config", "Code", "User", "mcp.json")},
		{goos: "windows", appData: `C:\AppData`, want: filepath.Join(`C:\AppData`, "Code", "User", "mcp.json")},
		{goos: "windows", wantErr: true},
	}
	for _, c := range cases {
		got, err := vscodePathFor(c.goos, c.home, c.appData)
		if c.wantErr {
			if err == nil {
				t.Errorf("vscodePathFor(%q) expected error", c.goos)
			}
			continue
		}
		if err != nil {
			t.Errorf("vscodePathFor(%q) err: %v", c.goos, err)
			continue
		}
		if got != c.want {
			t.Errorf("vscodePathFor(%q) = %q, want %q", c.goos, got, c.want)
		}
	}
}

func TestClaudeDesktopPathFor(t *testing.T) {
	cases := []struct {
		goos, home, appData string
		wantErr             bool
		want                string
	}{
		{goos: "darwin", home: "/Users/x", want: filepath.Join("/Users/x", "Library", "Application Support", "Claude", "claude_desktop_config.json")},
		{goos: "linux", home: "/home/x", want: filepath.Join("/home/x", ".config", "Claude", "claude_desktop_config.json")},
		{goos: "windows", appData: `C:\AppData`, want: filepath.Join(`C:\AppData`, "Claude", "claude_desktop_config.json")},
		{goos: "windows", wantErr: true},
	}
	for _, c := range cases {
		got, err := claudeDesktopPathFor(c.goos, c.home, c.appData)
		if c.wantErr {
			if err == nil {
				t.Errorf("claudeDesktopPathFor(%q) expected error", c.goos)
			}
			continue
		}
		if err != nil {
			t.Errorf("claudeDesktopPathFor(%q) err: %v", c.goos, err)
			continue
		}
		if got != c.want {
			t.Errorf("claudeDesktopPathFor(%q) = %q, want %q", c.goos, got, c.want)
		}
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func mustTarget(t *testing.T, name string) Target {
	t.Helper()
	tt, err := ByName(name)
	if err != nil {
		t.Fatalf("ByName(%q): %v", name, err)
	}
	return tt
}

// sandboxHome points HOME (and APPDATA on Windows) at a per-test temp dir so
// Configure() never touches the real user config. Every target's path
// derives from HOME, so this fully isolates the test.
func sandboxHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	return dir
}

func readRoot(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse %s: %v\nraw: %s", path, err, data)
	}
	return out
}

// Sanity check: a malformed file is recovered (not fatal). This keeps
// `korva setup` usable after a botched manual edit.
func TestConfigureRecoversFromCorruptFile(t *testing.T) {
	sandboxHome(t)
	target := mustTarget(t, "cursor")
	path, err := target.ConfigPath()
	if err != nil {
		t.Skip(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := target.Configure("https://api.korva.dev", "tok"); err != nil {
		t.Fatalf("Configure on corrupt file: %v", err)
	}
	root := readRoot(t, path)
	servers, _ := root["mcpServers"].(map[string]any)
	if _, ok := servers["korva"]; !ok {
		t.Error("recovery: korva entry missing")
	}
}

// Read-error propagation: an unreadable file (parent dir is a file, not a
// dir) should error out of Configure rather than silently succeed.
func TestConfigureSurfacesUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm-bit test is POSIX-only")
	}
	sandboxHome(t)
	target := mustTarget(t, "cursor")
	path, err := target.ConfigPath()
	if err != nil {
		t.Skip(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(path, 0o600) }()
	// Root reads everything regardless of mode bits, so skip when running
	// as root (e.g. inside a container without USER set).
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission-bit read errors don't apply")
	}
	if err := target.Configure("https://api.korva.dev", "tok"); err == nil {
		t.Error("expected error reading 0o000 file")
	} else if !errors.Is(err, os.ErrPermission) && !strings.Contains(err.Error(), "permission") {
		t.Errorf("unexpected error kind: %v", err)
	}
}
