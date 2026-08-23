package api

// Portfolio-context endpoints (F1). Wire shapes mirror contextJSON in
// the backbone's internal/portfolio package; the contract is fixed by
// the platform repo's docs/specs/portfolio-context.md.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ContextBranch is one branch tip reported by a context push.
type ContextBranch struct {
	Name         string     `json:"name"`
	LastCommitAt *time.Time `json:"last_commit_at,omitempty"`
}

// ProjectContext is one project's living source-of-truth record.
type ProjectContext struct {
	Project        string          `json:"project"`
	HeadSHA        string          `json:"head_sha,omitempty"`
	Branch         string          `json:"branch,omitempty"`
	CommittedAt    *time.Time      `json:"committed_at,omitempty"`
	RepoURL        string          `json:"repo_url,omitempty"`
	Branches       []ContextBranch `json:"branches"`
	PushedAt       *time.Time      `json:"pushed_at,omitempty"`
	BriefMD        string          `json:"brief_md"`
	BriefUpdatedAt *time.Time      `json:"brief_updated_at,omitempty"`
	BriefBy        string          `json:"brief_by,omitempty"`
	Stale          bool            `json:"stale"`
}

// ContextPush is the body of POST /v1/context/push.
type ContextPush struct {
	Project     string          `json:"project"`
	HeadSHA     string          `json:"head_sha"`
	Branch      string          `json:"branch"`
	CommittedAt *time.Time      `json:"committed_at,omitempty"`
	RepoURL     string          `json:"repo_url,omitempty"`
	Branches    []ContextBranch `json:"branches,omitempty"`
}

// PushContext records a CI push. The Client's Token must be a
// project-scoped kctx_ push token — not a user API token.
func (c *Client) PushContext(ctx context.Context, push ContextPush) (ProjectContext, error) {
	var out ProjectContext
	err := c.do(ctx, http.MethodPost, "/v1/context/push", push, true, &out)
	return out, err
}

// Portfolio returns every project's context record with freshness.
func (c *Client) Portfolio(ctx context.Context) ([]ProjectContext, error) {
	var out struct {
		Projects []ProjectContext `json:"projects"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/team/context", nil, true, &out)
	return out.Projects, err
}

// GetProjectContext returns one project's record. A project with no
// context yet surfaces as an *HTTPError with Status 404.
func (c *Client) GetProjectContext(ctx context.Context, name string) (ProjectContext, error) {
	var out ProjectContext
	err := c.do(ctx, http.MethodGet, "/v1/team/projects/"+url.PathEscape(name)+"/context", nil, true, &out)
	return out, err
}

// Observation is one vault entry (compact fields the context files need).
type Observation struct {
	ID        string   `json:"id"`
	Project   string   `json:"project,omitempty"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	Author    string   `json:"author,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// ListObservations returns a project's recent vault entries, newest
// first (backed by GET /v1/team/observations).
func (c *Client) ListObservations(ctx context.Context, project string, limit int) ([]Observation, error) {
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/v1/team/observations"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out struct {
		Entries []Observation `json:"entries"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, true, &out)
	return out.Entries, err
}
