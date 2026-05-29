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

func TestStartDeviceLogin(t *testing.T) {
	var gotLabel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/device/start" {
			t.Errorf("path = %s", r.URL.Path)
		}
		// The login sends its machine label so the backbone can name the
		// token after it.
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			var body struct {
				Label string `json:"label"`
			}
			_ = json.Unmarshal(raw, &body)
			gotLabel = body.Label
		}
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"AB-CD","verification_uri":"u","interval":2,"expires_in":900}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "").StartDeviceLogin(context.Background(), "felipe-macbook")
	if err != nil {
		t.Fatalf("StartDeviceLogin: %v", err)
	}
	if got.DeviceCode != "dc" || got.UserCode != "AB-CD" || got.Interval != 2 {
		t.Errorf("unexpected response: %+v", got)
	}
	if gotLabel != "felipe-macbook" {
		t.Errorf("label sent = %q, want %q", gotLabel, "felipe-macbook")
	}
}

func TestPollDeviceLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"approved","token":"api-tok","user":{"email":"u@korva.dev"}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "").PollDeviceLogin(context.Background(), "dc")
	if err != nil {
		t.Fatalf("PollDeviceLogin: %v", err)
	}
	if got.Status != "approved" || got.Token != "api-tok" {
		t.Errorf("unexpected response: %+v", got)
	}
}

func TestMeSendsBearerToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"user":{"id":"1","email":"u@korva.dev"}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "my-token").Me(context.Background())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if got.Email != "u@korva.dev" {
		t.Errorf("email = %q", got.Email)
	}
}

func TestNon2xxYieldsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "bad").Me(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %v", err)
	}
	if httpErr.Status != http.StatusUnauthorized || httpErr.Message != "invalid token" {
		t.Errorf("unexpected error: %+v", httpErr)
	}
}
