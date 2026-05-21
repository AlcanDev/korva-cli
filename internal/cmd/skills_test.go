package cmd

import (
	"reflect"
	"testing"

	"github.com/AlcanDev/korva-cli/internal/api"
)

func TestParseInputFlag(t *testing.T) {
	tests := []struct {
		in      string
		want    api.SkillInput
		wantErr bool
	}{
		{"ticket", api.SkillInput{Name: "ticket"}, false},
		{"ticket:Linear id", api.SkillInput{Name: "ticket", Description: "Linear id"}, false},
		{"ticket:Linear id:required", api.SkillInput{Name: "ticket", Description: "Linear id", Required: true}, false},
		// Trailing colon allowed — keeps the parser permissive.
		{"ticket:Linear id:", api.SkillInput{Name: "ticket", Description: "Linear id"}, false},
		// Description may itself contain colons; only the first one is a separator.
		{"url:base:9090:required", api.SkillInput{Name: "url", Description: "base:9090", Required: true}, false},
		// Unknown trailing word is a typo.
		{"ticket:Linear id:optional", api.SkillInput{}, true},

		// Cut 6 — typed inputs. Order of trailing tokens is flexible.
		{"pct:percentage:required:number",
			api.SkillInput{Name: "pct", Description: "percentage", Required: true, Type: "number"}, false},
		{"flag::boolean",
			api.SkillInput{Name: "flag", Type: "boolean"}, false},
		{"env:Environment:required:enum=staging,prod",
			api.SkillInput{Name: "env", Description: "Environment", Required: true,
				Type: "enum", Enum: []string{"staging", "prod"}}, false},
		// Token order doesn't matter — enum then required.
		{"env::enum=a,b:required",
			api.SkillInput{Name: "env", Required: true, Type: "enum", Enum: []string{"a", "b"}}, false},
		// Repeated `required` is rejected so a typo doesn't silently
		// accumulate state.
		{"x:y:required:required", api.SkillInput{}, true},
		// Two types in one shorthand is a misuse.
		{"x:y:number:boolean", api.SkillInput{}, true},
		// Empty enum value.
		{"x::enum=", api.SkillInput{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseInputFlag(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseInputFlag(%q) returned no error, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseInputFlag(%q): %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseInputFlag(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestInputFlagsAccumulates(t *testing.T) {
	var flags inputFlags
	for _, raw := range []string{"a", "b:desc", "c::required"} {
		if err := flags.Set(raw); err != nil {
			t.Fatalf("flags.Set(%q): %v", raw, err)
		}
	}
	if len(flags) != 3 {
		t.Fatalf("flags len = %d, want 3", len(flags))
	}
	if !flags[2].Required {
		t.Errorf("third input should be required: %+v", flags[2])
	}
}

func TestInputFlagsRejectsInvalidShorthand(t *testing.T) {
	var flags inputFlags
	if err := flags.Set("ok"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := flags.Set("bad:desc:optional"); err == nil {
		t.Error("flags.Set should reject invalid third segment")
	}
	if len(flags) != 1 {
		t.Errorf("invalid Set should not append: len = %d, want 1", len(flags))
	}
}

func TestCmdSkillUnknownSubcommandFails(t *testing.T) {
	if got := cmdSkill([]string{"frobnicate"}); got != 1 {
		t.Errorf("cmdSkill(frobnicate) = %d, want 1", got)
	}
}

func TestCmdSkillHelpPrintsZero(t *testing.T) {
	if got := cmdSkill([]string{"help"}); got != 0 {
		t.Errorf("cmdSkill(help) = %d, want 0", got)
	}
	if got := cmdSkill(nil); got != 0 {
		t.Errorf("cmdSkill(nil) = %d, want 0 (usage)", got)
	}
}

func TestCmdSkillAddNoArgsFails(t *testing.T) {
	if got := cmdSkillAdd(nil); got != 1 {
		t.Errorf("cmdSkillAdd(nil) = %d, want 1", got)
	}
	if got := cmdSkillAdd([]string{"--body", "x"}); got != 1 {
		t.Errorf("cmdSkillAdd(--body x without name) = %d, want 1", got)
	}
}

func TestCmdSkillRmNoArgsFails(t *testing.T) {
	if got := cmdSkillRm(nil); got != 1 {
		t.Errorf("cmdSkillRm(nil) = %d, want 1", got)
	}
}

func TestCmdSkillProposeNoArgsFails(t *testing.T) {
	if got := cmdSkillPropose(nil); got != 1 {
		t.Errorf("cmdSkillPropose(nil) = %d, want 1", got)
	}
}

func TestCmdSkillApproveNoArgsFails(t *testing.T) {
	if got := cmdSkillApprove(nil); got != 1 {
		t.Errorf("cmdSkillApprove(nil) = %d, want 1", got)
	}
	if got := cmdSkillReject(nil); got != 1 {
		t.Errorf("cmdSkillReject(nil) = %d, want 1", got)
	}
}
