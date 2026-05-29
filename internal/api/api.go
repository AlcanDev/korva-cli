// Package api is a thin HTTP client for the Korva backbone.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the Korva backbone.
type Client struct {
	ServerURL string
	Token     string
	http      *http.Client
}

// New builds a Client for a backbone server.
func New(serverURL, token string) *Client {
	return &Client{
		ServerURL: strings.TrimRight(serverURL, "/"),
		Token:     token,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// HTTPError is returned for non-2xx responses.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("server returned %d: %s", e.Status, e.Message)
}

// User describes the authenticated account. TeamID and Role are
// populated by /v1/me when the caller belongs to a primary team;
// they stay empty during the device-flow PollDevice handshake.
type User struct {
	ID     string `json:"id"`
	OrgID  string `json:"org_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	TeamID string `json:"team_id,omitempty"`
	Role   string `json:"role,omitempty"`
}

// DeviceStart is the response of POST /v1/auth/device/start.
type DeviceStart struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
}

// DevicePoll is the response of POST /v1/auth/device/poll.
type DevicePoll struct {
	Status string `json:"status"` // pending | approved | expired
	Token  string `json:"token"`
	User   User   `json:"user"`
}

// StartDeviceLogin opens a device-flow grant. label is a human-meaningful
// name for this install (the machine hostname); the backbone uses it as the
// minted token's name so the web console can show which machine connected.
// An empty label is fine — the server falls back to a generic name.
func (c *Client) StartDeviceLogin(ctx context.Context, label string) (DeviceStart, error) {
	var out DeviceStart
	var body any
	if label != "" {
		body = map[string]string{"label": label}
	}
	err := c.do(ctx, http.MethodPost, "/v1/auth/device/start", body, false, &out)
	return out, err
}

// PollDeviceLogin checks whether a device grant has been approved.
func (c *Client) PollDeviceLogin(ctx context.Context, deviceCode string) (DevicePoll, error) {
	var out DevicePoll
	err := c.do(ctx, http.MethodPost, "/v1/auth/device/poll",
		map[string]string{"device_code": deviceCode}, false, &out)
	return out, err
}

// Me returns the authenticated user.
func (c *Client) Me(ctx context.Context) (User, error) {
	var out struct {
		User User `json:"user"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/me", nil, true, &out)
	return out.User, err
}

// SkillInput is a declared parameter of a skill. Mirrors the
// backend/web shape so a round-trip is identity. Type defaults to
// "string" on the wire (omitted) for older skills.
type SkillInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Type        string   `json:"type,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// Skill is a team-scoped MCP tool the backbone surfaces as
// `skill_<name>`. Inputs is non-nil after a server round-trip — old
// payloads that omit the field become an empty slice on the wire.
type Skill struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Body        string       `json:"body"`
	Status      string       `json:"status"`
	Inputs      []SkillInput `json:"inputs"`
	UpdatedAt   string       `json:"updated_at"`
}

// ListSkills returns every skill belonging to the caller's primary team.
func (c *Client) ListSkills(ctx context.Context) ([]Skill, error) {
	var out struct {
		TeamID string  `json:"team_id"`
		Skills []Skill `json:"skills"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/team/skills", nil, true, &out); err != nil {
		return nil, err
	}
	if out.Skills == nil {
		return []Skill{}, nil
	}
	return out.Skills, nil
}

// PutSkill creates or replaces a skill by name. The server returns the
// canonical record (with status assigned and inputs normalised).
func (c *Client) PutSkill(ctx context.Context, name, description, body string, inputs []SkillInput) (Skill, error) {
	if inputs == nil {
		inputs = []SkillInput{}
	}
	payload := map[string]any{
		"description": description,
		"body":        body,
		"inputs":      inputs,
	}
	var out Skill
	err := c.do(ctx, http.MethodPut, "/v1/team/skills/"+name, payload, true, &out)
	return out, err
}

// DeleteSkill removes a skill by name. Returns a 404 HTTPError when
// the skill does not exist on the server.
func (c *Client) DeleteSkill(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/team/skills/"+name, nil, true, nil)
}

// ProposeSkill submits a skill draft. Any team member may call this;
// the skill lands with status="pending" and stays invisible to MCP
// until a lead approves it.
func (c *Client) ProposeSkill(ctx context.Context, name, description, body string, inputs []SkillInput) (Skill, error) {
	if inputs == nil {
		inputs = []SkillInput{}
	}
	payload := map[string]any{
		"description": description,
		"body":        body,
		"inputs":      inputs,
	}
	var out Skill
	err := c.do(ctx, http.MethodPost, "/v1/team/skills/"+name+"/propose", payload, true, &out)
	return out, err
}

// ApproveSkill transitions a skill into status="approved". Lead-only;
// surfaces a 403 HTTPError if the caller lacks the role.
func (c *Client) ApproveSkill(ctx context.Context, name string) (Skill, error) {
	var out Skill
	err := c.do(ctx, http.MethodPost, "/v1/team/skills/"+name+"/approve", nil, true, &out)
	return out, err
}

// RejectSkill transitions a skill into status="rejected". Lead-only;
// surfaces a 403 HTTPError if the caller lacks the role.
func (c *Client) RejectSkill(ctx context.Context, name string) (Skill, error) {
	var out Skill
	err := c.do(ctx, http.MethodPost, "/v1/team/skills/"+name+"/reject", nil, true, &out)
	return out, err
}

func (c *Client) do(ctx context.Context, method, path string, reqBody any, auth bool, out any) error {
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.ServerURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		msg := resp.Status
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return &HTTPError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
