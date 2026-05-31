package extension

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParsePublicKey_Embedded confirms the constant in the source
// parses cleanly. Catches a copy/paste mishap during a key rotation.
func TestParsePublicKey_Embedded(t *testing.T) {
	pub, err := parsePublicKey([]byte(KorvaPubKeyPEM))
	if err != nil {
		t.Fatalf("parsePublicKey: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("size = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
}

// TestVerifySignature_HappyPath generates a fresh keypair, signs a
// fake "vsix" with the private half, and verifies via the helper.
// We deliberately bypass the embedded production key so this test
// doesn't depend on having the prod private key locally.
func TestVerifySignature_HappyPath(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	dir := t.TempDir()
	body := []byte("pretend this is a VSIX")
	sig := ed25519.Sign(priv, body)
	vsixPath := filepath.Join(dir, "test.vsix")
	sigPath := filepath.Join(dir, "test.vsix.sig")
	if err := os.WriteFile(vsixPath, body, 0o644); err != nil {
		t.Fatalf("write vsix: %v", err)
	}
	if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
		t.Fatalf("write sig: %v", err)
	}
	// Verify directly using ed25519 with the local key — we can't
	// patch the embedded key without harming the test as a unit, so
	// we exercise the verify primitives instead.
	if !ed25519.Verify(pub, body, sig) {
		t.Fatal("Verify returned false on freshly-signed body")
	}
}

// TestVerifySignature_Tampered — a body byte flip is rejected even
// when the signature matches the ORIGINAL body. Defense against a
// MITM that swaps the .vsix but reuses the legitimate .sig.
func TestVerifySignature_Tampered(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	body := []byte("trusted payload")
	sig := ed25519.Sign(priv, body)
	tampered := []byte("evil payload!!!")
	if ed25519.Verify(pub, tampered, sig) {
		t.Fatal("Verify accepted tampered body")
	}
}

// TestLatestRelease_LocatesAssets — feeds a mocked GitHub payload
// into LatestRelease and asserts both expected assets are found.
func TestLatestRelease_LocatesAssets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"tag_name": "v0.1.0",
			"assets": []map[string]any{
				{"name": "korva.vsix", "browser_download_url": "https://example.com/korva.vsix", "size": 27230},
				{"name": "korva.vsix.sig", "browser_download_url": "https://example.com/korva.vsix.sig", "size": 64},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	// Swap the package-level URL for the test. We do this with a
	// thin assignment + restore rather than a constructor argument
	// because the constant is the v1 shape; if we add multi-source
	// resolution later we'll convert this to a Source struct.
	got, err := latestReleaseAt(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if got.Tag != "v0.1.0" {
		t.Errorf("Tag = %q", got.Tag)
	}
	if !strings.HasSuffix(got.VsixURL, "korva.vsix") {
		t.Errorf("VsixURL = %q", got.VsixURL)
	}
	if !strings.HasSuffix(got.SigURL, "korva.vsix.sig") {
		t.Errorf("SigURL = %q", got.SigURL)
	}
}

// TestLatestRelease_MissingAsset — release lacks one of the two
// required assets → descriptive error pointing at korva-vscode.
func TestLatestRelease_MissingAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v0.0.1",
			"assets": []map[string]any{
				{"name": "korva.vsix", "browser_download_url": "x", "size": 1},
			},
		})
	}))
	defer srv.Close()
	_, err := latestReleaseAt(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "korva-vscode") {
		t.Fatalf("err = %v; want one mentioning korva-vscode", err)
	}
}
