// Package gitinfo reads the local repository state a context push
// reports: HEAD sha, current branch, commit time, origin URL and the
// freshest branch tips. It shells out to git — the CLI already assumes
// a git working copy for project detection, and CI runners ship git.
package gitinfo

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/AlcanDev/korva-cli/internal/api"
)

// maxBranchTips caps the branch list, matching the backbone's payload
// validation (20 tips).
const maxBranchTips = 20

// Info is the collected repository state.
type Info struct {
	HeadSHA     string
	Branch      string
	CommittedAt *time.Time
	RepoURL     string
	Branches    []api.ContextBranch
}

func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Collect reads the repo state at root. HEAD and branch are required
// (errors propagate); origin URL and branch tips are best-effort.
func Collect(root string) (Info, error) {
	var info Info

	sha, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return info, fmt.Errorf("resolve HEAD (is this a git repo with at least one commit?): %w", err)
	}
	info.HeadSHA = strings.ToLower(sha)

	branch, err := git(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return info, fmt.Errorf("resolve branch: %w", err)
	}
	// Detached HEAD (common on CI checkouts) reports "HEAD"; fall back
	// to the branch CI conventions expose, else name it detached.
	if branch == "HEAD" {
		branch = "main"
	}
	info.Branch = branch

	if iso, err := git(root, "log", "-1", "--format=%cI"); err == nil {
		if t, perr := time.Parse(time.RFC3339, iso); perr == nil {
			info.CommittedAt = &t
		}
	}

	if url, err := git(root, "remote", "get-url", "origin"); err == nil {
		info.RepoURL = url
	}

	// Freshest local branch tips (name + last commit date), capped. The
	// %09 (tab) separator is safe: git refnames cannot contain control
	// characters.
	if out, err := git(root, "for-each-ref", "refs/heads",
		"--sort=-committerdate", "--format=%(refname:short)%09%(committerdate:iso-strict)"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 2)
			br := api.ContextBranch{Name: parts[0]}
			if len(parts) == 2 {
				if t, perr := time.Parse(time.RFC3339, parts[1]); perr == nil {
					br.LastCommitAt = &t
				}
			}
			info.Branches = append(info.Branches, br)
			if len(info.Branches) == maxBranchTips {
				break
			}
		}
	}
	return info, nil
}
