package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListPackagesParsesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/team/packages" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"team_id":"t1",
			"packages":[
				{"id":"p1","name":"epsdtavao","display_name":"EPSDTAVAO","version":3,"status":"approved"}
			]
		}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok").ListPackages(context.Background())
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(got) != 1 || got[0].Name != "epsdtavao" || got[0].Version != 3 {
		t.Errorf("unexpected payload: %+v", got)
	}
}

func TestInstallPackagePostsCorrectBody(t *testing.T) {
	var seenPath, seenAuth string
	var seenBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		seenBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{
			"id":"p1","name":"ci-helpers","version":1,"status":"approved",
			"commands":[{"name":"dev","body":"hi"}]
		}`))
	}))
	defer srv.Close()

	pkg, err := New(srv.URL, "ignored-token").
		InstallPackage(context.Background(), "kvp_abcdef0123456789", "demo", "0.1.0",
			[]string{"copilot", "claude-code"}, 2)
	if err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if seenPath != "/v1/packages/install/kvp_abcdef0123456789" {
		t.Errorf("path = %q", seenPath)
	}
	if seenAuth != "" {
		t.Errorf("install must be unauthenticated; got Authorization=%q", seenAuth)
	}
	var payload map[string]any
	if err := json.Unmarshal(seenBody, &payload); err != nil {
		t.Fatalf("body parse: %v", err)
	}
	if payload["project"] != "demo" || payload["cli_version"] != "0.1.0" {
		t.Errorf("payload = %v", payload)
	}
	if pkg.Name != "ci-helpers" || len(pkg.Commands) != 1 {
		t.Errorf("response decoded wrong: %+v", pkg)
	}
}

func TestInstallPackage_410MapsToHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":"install code revoked"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, "").
		InstallPackage(context.Background(), "kvp_anything", "p", "v", nil, 0)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "410") || !strings.Contains(err.Error(), "revoked") {
		t.Errorf("error = %v", err)
	}
}

func TestRecordPackageRunPostsAuth(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	if err := New(srv.URL, "tok").
		RecordPackageRun(context.Background(), "", "", "demo", "dev", "claude-code", true); err != nil {
		t.Fatalf("RecordPackageRun: %v", err)
	}
	if seenAuth != "Bearer tok" {
		t.Errorf("auth = %q", seenAuth)
	}
}
