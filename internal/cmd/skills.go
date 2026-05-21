package cmd

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AlcanDev/korva-cli/internal/api"
	"github.com/AlcanDev/korva-cli/internal/config"
)

// cmdSkill dispatches `korva skill <subcommand>`. It's small enough to
// keep parsing inline instead of pulling in a real subcommand library.
func cmdSkill(args []string) int {
	if len(args) == 0 {
		skillUsage(os.Stdout)
		return 0
	}
	switch args[0] {
	case "list", "ls":
		return cmdSkillList()
	case "show", "get":
		return cmdSkillShow(args[1:])
	case "add", "put":
		return cmdSkillAdd(args[1:])
	case "rm", "delete":
		return cmdSkillRm(args[1:])
	case "propose":
		return cmdSkillPropose(args[1:])
	case "approve":
		return cmdSkillApprove(args[1:])
	case "reject":
		return cmdSkillReject(args[1:])
	case "help", "--help", "-h":
		skillUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown skill subcommand: %s\n\n", args[0])
		skillUsage(os.Stderr)
		return 1
	}
}

func skillUsage(w io.Writer) {
	fmt.Fprint(w, `korva skill — manage team skills (MCP tools backed by the Korva backbone)

Usage:
  korva skill list                                List every skill on your team
  korva skill show <name>                         Print a skill's full body and declared inputs
  korva skill add <name> [flags]                  Create or replace a skill
  korva skill rm <name> [--yes]                   Delete a skill (asks for confirmation by default)
  korva skill propose <name> [flags]              Submit a skill draft (members; lands as "pending")
  korva skill approve <name>                      Approve a pending skill (Team Lead)
  korva skill reject <name>                       Reject a pending skill (Team Lead)

Flags for `+"`add` and `propose`"+`:
  --description STR        Short text shown to the editor agent (max 240 chars)
  --body STR               Skill body. Use {{var}} placeholders for declared inputs.
  --body-file PATH         Read body from PATH (use "-" for stdin); takes precedence over --body
  --input NAME[:DESC][:required][:TYPE]
                          Declare an input. Repeatable. Trailing tokens can be combined
                          in any order:
                            :required               - the agent must supply this arg
                            :string|number|boolean  - input type (default: string)
                            :enum=val1,val2,...     - input is one of these values
                          E.g. --input pct:percentage:required:number
                               --input env:Environment:required:enum=staging,prod

Examples:
  korva skill list
  korva skill add release_note \
    --description "Draft a release note" \
    --body-file ./release_note.tmpl \
    --input service:"Service name":required \
    --input version::required \
    --input summary::required \
    --input author:"Lead engineer"
  korva skill rm release_note --yes
`)
}

// --- Subcommands ------------------------------------------------------------

func cmdSkillList() int {
	client, ok := authedClient()
	if !ok {
		return 1
	}
	skills, err := client.ListSkills(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "list failed: %v\n", err)
		return 1
	}
	if len(skills) == 0 {
		fmt.Println("No skills yet. Use `korva skill add <name>` to create one.")
		return 0
	}
	// Plain text columns keep the output diff-friendly when scripts pipe it.
	maxName := len("NAME")
	for _, sk := range skills {
		if len(sk.Name) > maxName {
			maxName = len(sk.Name)
		}
	}
	fmt.Printf("%-*s  %-9s  %-7s  %s\n", maxName, "NAME", "STATUS", "INPUTS", "DESCRIPTION")
	for _, sk := range skills {
		fmt.Printf("%-*s  %-9s  %-7d  %s\n",
			maxName, sk.Name, sk.Status, len(sk.Inputs), sk.Description)
	}
	return 0
}

func cmdSkillShow(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: korva skill show <name>")
		return 1
	}
	client, ok := authedClient()
	if !ok {
		return 1
	}
	name := args[0]
	skills, err := client.ListSkills(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "show failed: %v\n", err)
		return 1
	}
	for _, sk := range skills {
		if sk.Name == name {
			printSkill(sk)
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "skill %q not found\n", name)
	return 1
}

func cmdSkillAdd(args []string) int {
	return runSkillSubmit("skill add", "save", "Saved", args, func(client *api.Client, name, description, body string, inputs []api.SkillInput) (api.Skill, error) {
		return client.PutSkill(context.Background(), name, description, body, inputs)
	})
}

func cmdSkillPropose(args []string) int {
	return runSkillSubmit("skill propose", "propose", "Proposed", args, func(client *api.Client, name, description, body string, inputs []api.SkillInput) (api.Skill, error) {
		return client.ProposeSkill(context.Background(), name, description, body, inputs)
	})
}

// runSkillSubmit factors the shared flag-parsing and body-resolution
// path between `add` (lead, status=approved) and `propose` (any member,
// status=pending). errorVerb is used for the failure line; pastTense
// is the user-facing word on success ("Saved" / "Proposed").
func runSkillSubmit(
	flagSetName, errorVerb, pastTense string,
	args []string,
	submit func(*api.Client, string, string, string, []api.SkillInput) (api.Skill, error),
) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(os.Stderr, "usage: korva %s <name> [flags]\n", flagSetName)
		return 1
	}
	name := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet(flagSetName, flag.ContinueOnError)
	description := fs.String("description", "", "short text shown to the editor agent")
	body := fs.String("body", "", "skill body (use {{var}} for declared inputs)")
	bodyFile := fs.String("body-file", "", "read body from file (- for stdin)")
	var inputs inputFlags
	fs.Var(&inputs, "input", "declare an input: NAME[:DESC[:required]] (repeatable)")
	if err := fs.Parse(rest); err != nil {
		return 1
	}

	resolvedBody, err := resolveBody(*body, *bodyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if resolvedBody == "" {
		fmt.Fprintln(os.Stderr, "body is required (use --body or --body-file)")
		return 1
	}

	client, ok := authedClient()
	if !ok {
		return 1
	}
	sk, err := submit(client, name, *description, resolvedBody, inputs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s failed: %v\n", errorVerb, err)
		return 1
	}
	fmt.Printf("%s %q (status: %s).\n", pastTense, sk.Name, sk.Status)
	return 0
}

func cmdSkillApprove(args []string) int {
	return runSkillTransition("approve", "approving", args, func(c *api.Client, name string) (api.Skill, error) {
		return c.ApproveSkill(context.Background(), name)
	})
}

func cmdSkillReject(args []string) int {
	return runSkillTransition("reject", "rejecting", args, func(c *api.Client, name string) (api.Skill, error) {
		return c.RejectSkill(context.Background(), name)
	})
}

func runSkillTransition(
	verb, gerund string,
	args []string,
	call func(*api.Client, string) (api.Skill, error),
) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: korva skill %s <name>\n", verb)
		return 1
	}
	client, ok := authedClient()
	if !ok {
		return 1
	}
	sk, err := call(client, args[0])
	if err != nil {
		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) {
			switch httpErr.Status {
			case 404:
				fmt.Fprintf(os.Stderr, "skill %q not found\n", args[0])
				return 1
			case 403:
				fmt.Fprintf(os.Stderr, "only a Team Lead can %s skills\n", verb)
				return 1
			}
		}
		fmt.Fprintf(os.Stderr, "%s failed: %v\n", gerund, err)
		return 1
	}
	fmt.Printf("%s is now %s.\n", sk.Name, sk.Status)
	return 0
}

func cmdSkillRm(args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		// Allow `--yes <name>` too, but the documented form is name-first.
		fs := flag.NewFlagSet("skill rm", flag.ContinueOnError)
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		if err := fs.Parse(args); err != nil {
			return 1
		}
		if fs.NArg() == 0 {
			fmt.Fprintln(os.Stderr, "usage: korva skill rm <name> [--yes]")
			return 1
		}
		return removeSkill(fs.Arg(0), *yes)
	}
	name := args[0]
	fs := flag.NewFlagSet("skill rm", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	return removeSkill(name, *yes)
}

func removeSkill(name string, yes bool) int {
	if !yes {
		fmt.Printf("Delete skill %q? Agents will lose access immediately. [y/N] ", name)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("Aborted.")
			return 1
		}
	}

	client, ok := authedClient()
	if !ok {
		return 1
	}
	if err := client.DeleteSkill(context.Background(), name); err != nil {
		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) && httpErr.Status == 404 {
			fmt.Fprintf(os.Stderr, "skill %q not found\n", name)
			return 1
		}
		fmt.Fprintf(os.Stderr, "delete failed: %v\n", err)
		return 1
	}
	fmt.Printf("Deleted %q.\n", name)
	return 0
}

// --- Helpers ----------------------------------------------------------------

// inputFlags collects repeated --input flags. The shorthand
// `NAME[:DESC[:required]]` is parsed eagerly so a typo blocks the
// command before it hits the network.
type inputFlags []api.SkillInput

func (f *inputFlags) String() string { return "" }

func (f *inputFlags) Set(raw string) error {
	in, err := parseInputFlag(raw)
	if err != nil {
		return err
	}
	*f = append(*f, in)
	return nil
}

// Trailing tokens recognized by parseInputFlag. Order doesn't matter
// — we keep stripping known suffixes until none match, then what's
// left is `name[:description]`. The `enum=` prefix is special because
// it carries data.
var simpleTypeTokens = map[string]string{
	"string":  "string",
	"number":  "number",
	"boolean": "boolean",
}

func parseInputFlag(raw string) (api.SkillInput, error) {
	required := false
	skType := ""
	var enumVals []string

	// Peel off recognized suffixes one at a time. The loop stops when
	// no suffix matches anymore. A bare trailing `:` is treated as an
	// empty no-op token so the parser stays permissive. The typo
	// guard only fires when NO known suffix has been consumed yet —
	// otherwise a description like `url:base:9090` (3 colons, last
	// segment is a word) would be mistakenly flagged.
	rest := raw
	consumedAny := false
	for {
		idx := strings.LastIndex(rest, ":")
		if idx < 0 {
			break
		}
		tail := rest[idx+1:]
		switch {
		case tail == "":
			rest = rest[:idx] // trailing colon, strip and keep going
			consumedAny = true
		case tail == "required":
			if required {
				return api.SkillInput{}, fmt.Errorf("`required` repeated in --input %q", raw)
			}
			required = true
			rest = rest[:idx]
			consumedAny = true
		case simpleTypeTokens[tail] != "":
			if skType != "" {
				return api.SkillInput{}, fmt.Errorf("type repeated in --input %q", raw)
			}
			skType = simpleTypeTokens[tail]
			rest = rest[:idx]
			consumedAny = true
		case strings.HasPrefix(tail, "enum="):
			if skType != "" {
				return api.SkillInput{}, fmt.Errorf("type repeated in --input %q", raw)
			}
			skType = "enum"
			for _, v := range strings.Split(strings.TrimPrefix(tail, "enum="), ",") {
				v = strings.TrimSpace(v)
				if v == "" {
					return api.SkillInput{}, fmt.Errorf(
						"empty enum value in --input %q", raw)
				}
				enumVals = append(enumVals, v)
			}
			if len(enumVals) == 0 {
				return api.SkillInput{}, fmt.Errorf(
					"enum= needs at least one value in --input %q", raw)
			}
			rest = rest[:idx]
			consumedAny = true
		default:
			// Typo guard (carried from Cut 3): only fire when the
			// caller hasn't successfully matched any known suffix yet
			// AND the description appears to actually be a single
			// word — a strong signal they meant `required` /
			// `number` etc. but misspelled it. Descriptions with
			// spaces / colons / digits-only legitimately reach here.
			if !consumedAny && isWordLike(tail) &&
				strings.Count(raw, ":") >= 2 && !isAllDigits(tail) {
				return api.SkillInput{}, fmt.Errorf(
					`unknown suffix %q in --input %q (allowed: required, string, number, boolean, enum=a,b,c)`,
					tail, raw)
			}
			goto done
		}
	}
done:
	parts := strings.SplitN(rest, ":", 2)
	in := api.SkillInput{Name: parts[0], Required: required, Type: skType, Enum: enumVals}
	if len(parts) == 2 {
		in.Description = parts[1]
	}
	return in, nil
}

// isAllDigits returns true when s is purely numeric (digits only).
// A purely numeric trailing segment like `9090` is almost certainly
// part of a description (a port, year, etc.), never a typo'd keyword.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isWordLike returns true for a non-empty ASCII identifier — the kind
// of suffix that's almost certainly a misspelled keyword rather than
// the tail of a real description (which would contain spaces).
func isWordLike(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func resolveBody(body, bodyFile string) (string, error) {
	if bodyFile == "" {
		return body, nil
	}
	if bodyFile == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", bodyFile, err)
	}
	return string(b), nil
}

// authedClient builds a *api.Client from the saved config, writing a
// useful error if the user has not run `korva login`.
func authedClient() (*api.Client, bool) {
	cfg, err := config.Load()
	if err != nil || !cfg.LoggedIn() {
		fmt.Fprintln(os.Stderr, "not logged in; run `korva login`")
		return nil, false
	}
	return api.New(cfg.ServerURL, cfg.Token), true
}

func printSkill(sk api.Skill) {
	fmt.Printf("Name:        %s\n", sk.Name)
	fmt.Printf("Status:      %s\n", sk.Status)
	if sk.Description != "" {
		fmt.Printf("Description: %s\n", sk.Description)
	}
	if len(sk.Inputs) > 0 {
		fmt.Println("Inputs:")
		for _, in := range sk.Inputs {
			typ := in.Type
			if typ == "" {
				typ = "string"
			}
			req := ""
			if in.Required {
				req = " (required)"
			}
			fmt.Printf("  - %s: %s%s\n", in.Name, typ, req)
			if in.Type == "enum" {
				fmt.Printf("      values: %s\n", strings.Join(in.Enum, ", "))
			}
			if in.Description != "" {
				fmt.Printf("      %s\n", in.Description)
			}
		}
	}
	fmt.Println("Body:")
	for _, line := range strings.Split(sk.Body, "\n") {
		fmt.Printf("  %s\n", line)
	}
}
