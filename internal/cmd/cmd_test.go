package cmd

import (
	"strings"
	"testing"

	"github.com/AlcanDev/korva-cli/internal/config"
)

func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no args shows usage", nil, 0},
		{"version", []string{"version"}, 0},
		{"help", []string{"help"}, 0},
		{"unknown command", []string{"frobnicate"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Run(tt.args); got != tt.want {
				t.Errorf("Run(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestResolveServerPrecedence(t *testing.T) {
	t.Setenv("KORVA_SERVER", "")
	if got := resolveServer("https://flag", config.Config{}); got != "https://flag" {
		t.Errorf("flag should win: got %q", got)
	}

	t.Setenv("KORVA_SERVER", "https://env")
	if got := resolveServer("", config.Config{}); got != "https://env" {
		t.Errorf("env should win when no flag: got %q", got)
	}
	if got := resolveServer("https://flag", config.Config{}); got != "https://flag" {
		t.Errorf("flag should beat env: got %q", got)
	}

	t.Setenv("KORVA_SERVER", "")
	if got := resolveServer("", config.Config{ServerURL: "https://cfg"}); got != "https://cfg" {
		t.Errorf("config should win when no flag/env: got %q", got)
	}
	if got := resolveServer("", config.Config{}); got != config.DefaultServerURL {
		t.Errorf("default should be the fallback: got %q", got)
	}
}

func TestResolveTargets(t *testing.T) {
	// "all" expands to every registered target.
	all, err := resolveTargets("all")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) < 5 {
		t.Errorf("expected ≥5 targets, got %d", len(all))
	}

	// Explicit list resolves each name (aliases included).
	got, err := resolveTargets("claude,cursor,vscode")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	if names[0] != "claude-code" || names[1] != "cursor" || names[2] != "vscode" {
		t.Errorf("names = %v, want [claude-code cursor vscode]", names)
	}

	// Unknown target is reported with the list of valid names.
	_, err = resolveTargets("nano")
	if err == nil || !strings.Contains(err.Error(), "vscode") {
		t.Errorf("unknown target should list known names, got: %v", err)
	}

	// Empty value defers to auto-detect; we can't assert detection here
	// because it depends on the host, but the function must not panic.
	_, _ = resolveTargets("")
}
