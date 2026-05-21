package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListSkillsParsesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/team/skills" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"team_id":"t1",
			"skills":[
				{"name":"a","description":"d","body":"b","status":"approved","inputs":[],"updated_at":"2026-05-20"},
				{"name":"b","description":"","body":"x","status":"pending","inputs":[{"name":"v","required":true}],"updated_at":"2026-05-20"}
			]
		}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok").ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Status != "pending" {
		t.Errorf("unexpected payload: %+v", got)
	}
	if len(got[1].Inputs) != 1 || !got[1].Inputs[0].Required {
		t.Errorf("inputs not parsed: %+v", got[1].Inputs)
	}
}

func TestListSkillsEmptyArrayIsNonNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"team_id":"t1","skills":[]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok").ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty non-nil slice, got %v", got)
	}
}

func TestPutSkillRoundTrip(t *testing.T) {
	var seenPath, seenMethod string
	var seenBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenMethod = r.Method
		seenBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"name":"pre_push","description":"d","body":"b","status":"approved","inputs":[{"name":"ticket","required":true}],"updated_at":"2026-05-20"}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok").PutSkill(context.Background(), "pre_push", "d", "b",
		[]SkillInput{{Name: "ticket", Required: true}})
	if err != nil {
		t.Fatalf("PutSkill: %v", err)
	}
	if seenMethod != http.MethodPut || seenPath != "/v1/team/skills/pre_push" {
		t.Errorf("unexpected %s %s", seenMethod, seenPath)
	}
	var sent map[string]any
	if err := json.Unmarshal(seenBody, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent["description"] != "d" || sent["body"] != "b" {
		t.Errorf("body fields lost in transit: %v", sent)
	}
	inputs, _ := sent["inputs"].([]any)
	if len(inputs) != 1 {
		t.Errorf("inputs not forwarded: %v", inputs)
	}
	if got.Name != "pre_push" || len(got.Inputs) != 1 {
		t.Errorf("unexpected response: %+v", got)
	}
}

func TestPutSkillNilInputsSendsEmptyArray(t *testing.T) {
	var seenBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"name":"x","description":"","body":"y","status":"approved","inputs":[],"updated_at":"2026-05-20"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok").PutSkill(context.Background(), "x", "", "y", nil)
	if err != nil {
		t.Fatalf("PutSkill: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(seenBody, &sent)
	inputs, ok := sent["inputs"].([]any)
	if !ok {
		t.Fatalf("inputs missing or wrong type: %v", sent)
	}
	if len(inputs) != 0 {
		t.Errorf("expected empty array, got %v", inputs)
	}
}

func TestProposeSkillPostsToProposePathAsPending(t *testing.T) {
	var seenPath, seenMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenMethod = r.Method
		_, _ = w.Write([]byte(`{"name":"x","description":"","body":"y","status":"pending","inputs":[],"updated_at":"2026-05-20"}`))
	}))
	defer srv.Close()

	sk, err := New(srv.URL, "tok").ProposeSkill(context.Background(), "x", "", "y", nil)
	if err != nil {
		t.Fatalf("ProposeSkill: %v", err)
	}
	if seenMethod != http.MethodPost || seenPath != "/v1/team/skills/x/propose" {
		t.Errorf("unexpected %s %s", seenMethod, seenPath)
	}
	if sk.Status != "pending" {
		t.Errorf("status = %q, want pending", sk.Status)
	}
}

func TestApproveSkillPostsToApprovePath(t *testing.T) {
	var seenPath, seenMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenMethod = r.Method
		_, _ = w.Write([]byte(`{"name":"x","description":"","body":"y","status":"approved","inputs":[],"updated_at":"2026-05-20"}`))
	}))
	defer srv.Close()

	sk, err := New(srv.URL, "tok").ApproveSkill(context.Background(), "x")
	if err != nil {
		t.Fatalf("ApproveSkill: %v", err)
	}
	if seenMethod != http.MethodPost || seenPath != "/v1/team/skills/x/approve" {
		t.Errorf("unexpected %s %s", seenMethod, seenPath)
	}
	if sk.Status != "approved" {
		t.Errorf("status = %q", sk.Status)
	}
}

func TestApproveSkill403IsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"only a Team Lead can change skills"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok").ApproveSkill(context.Background(), "x")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusForbidden {
		t.Errorf("expected 403 HTTPError, got %v", err)
	}
}

func TestRejectSkillPostsToRejectPath(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_, _ = w.Write([]byte(`{"name":"x","description":"","body":"y","status":"rejected","inputs":[],"updated_at":"2026-05-20"}`))
	}))
	defer srv.Close()

	sk, err := New(srv.URL, "tok").RejectSkill(context.Background(), "x")
	if err != nil {
		t.Fatalf("RejectSkill: %v", err)
	}
	if seenPath != "/v1/team/skills/x/reject" {
		t.Errorf("path = %s", seenPath)
	}
	if sk.Status != "rejected" {
		t.Errorf("status = %q", sk.Status)
	}
}

func TestDeleteSkillSurfaces404AsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"skill not found"}`))
	}))
	defer srv.Close()

	err := New(srv.URL, "tok").DeleteSkill(context.Background(), "nope")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 404 {
		t.Errorf("expected 404 HTTPError, got %v", err)
	}
}
