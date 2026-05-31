// Package extension installs the sideloaded Korva VS Code extension.
//
// Per ADR 0020 we do NOT publish to the public VS Code Marketplace.
// Instead this package fetches the signed .vsix from the latest
// release on github.com/AlcanDev/korva-vscode, verifies the ed25519
// signature against the pinned public key, and runs
// `code --install-extension` to install it.
//
// The public key is embedded at build time so verification never
// depends on a network hop. Rotating the key is a CLI release.
package extension

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// KorvaPubKeyPEM is the team's ed25519 public key, half of the keypair
// kept in AlcanDev/korva-vscode (the private half lives ONLY in that
// repo's SIGNING_PRIVATE_KEY GitHub Secret). Embedding it here means
// the CLI verifies the .vsix without trusting any HTTPS hop — even if
// GitHub or our release pipeline were compromised the unsigned blob
// would be rejected.
//
// When this key is rotated, bump the constant and ship a new CLI
// release; older CLIs will refuse to install newer VSIX builds (the
// correct failure mode).
const KorvaPubKeyPEM = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAY6JWcqRnsGMIC0HNytkJ75IKomlsxUnc8Uaso0d+IJg=
-----END PUBLIC KEY-----`

const (
	// releasesAPI is the GitHub REST endpoint for the latest release.
	// Public repo so no auth is needed; the URL itself is the
	// trust root (combined with the embedded pubkey).
	releasesAPI = "https://api.github.com/repos/AlcanDev/korva-vscode/releases/latest"

	// downloadTimeout caps the assembled download flow.
	downloadTimeout = 90 * time.Second
)

// Release is the subset of the GitHub release payload the installer
// needs: tag + the two expected assets (the .vsix and its .sig).
type Release struct {
	Tag      string
	VsixURL  string
	SigURL   string
	VsixSize int64
}

// ErrNoVSCode means we couldn't find a usable `code` or `code-insiders`
// binary on $PATH. Surfaces a friendly install message to the user.
var ErrNoVSCode = errors.New("VS Code not found on $PATH (install it from https://code.visualstudio.com first)")

// LatestRelease fetches the latest tagged release from
// AlcanDev/korva-vscode and locates the .vsix + .sig assets. Returns
// a descriptive error when the release exists but is missing one of
// the two required assets — that's a release-process bug, not a CLI
// bug, so the wording points at the right repo.
func LatestRelease(ctx context.Context) (Release, error) {
	return latestReleaseAt(ctx, releasesAPI)
}

// latestReleaseAt is LatestRelease parameterised on the source URL,
// so the test suite can point at httptest.NewServer.
func latestReleaseAt(ctx context.Context, url string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github returned %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}

	out := Release{Tag: payload.TagName}
	for _, a := range payload.Assets {
		switch a.Name {
		case "korva.vsix":
			out.VsixURL = a.BrowserDownloadURL
			out.VsixSize = a.Size
		case "korva.vsix.sig":
			out.SigURL = a.BrowserDownloadURL
		}
	}
	if out.VsixURL == "" || out.SigURL == "" {
		return Release{}, fmt.Errorf(
			"release %s is missing korva.vsix or korva.vsix.sig — open an issue at github.com/AlcanDev/korva-vscode",
			out.Tag)
	}
	return out, nil
}

// DownloadAndVerify fetches the .vsix + .sig from the release into a
// temp dir, verifies the ed25519 signature against KorvaPubKeyPEM,
// and returns the local .vsix path. A failed signature deletes the
// temp file before returning so a tampered blob can't linger on disk
// for a confused user to install manually later.
func DownloadAndVerify(ctx context.Context, rel Release) (string, error) {
	dir, err := os.MkdirTemp("", "korva-vsix-*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	vsixPath := filepath.Join(dir, "korva.vsix")
	sigPath := filepath.Join(dir, "korva.vsix.sig")
	if err := downloadFile(ctx, rel.VsixURL, vsixPath); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if err := downloadFile(ctx, rel.SigURL, sigPath); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if err := verifySignature(vsixPath, sigPath); err != nil {
		// Bury the bad bytes so a curious user can't `code
		// --install-extension` it themselves and bypass the check.
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("signature verification failed: %w", err)
	}
	return vsixPath, nil
}

// InstallVSIX runs `code --install-extension <path>`, preferring
// stable VS Code over Insiders when both are present. Returns
// ErrNoVSCode when neither binary is on $PATH so the caller can
// surface a friendly "install VS Code first" message.
func InstallVSIX(vsixPath string) error {
	bin, err := resolveVSCodeBinary()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "--install-extension", vsixPath, "--force")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install via %s: %w", bin, err)
	}
	return nil
}

// UninstallExtension removes the extension. Idempotent: returns nil
// when the extension wasn't installed.
func UninstallExtension() error {
	bin, err := resolveVSCodeBinary()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "--uninstall-extension", "alcandev.korva")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run() // ignore exit code — code returns non-zero when missing
	return nil
}

// --- helpers ----------------------------------------------------------------

func resolveVSCodeBinary() (string, error) {
	for _, name := range []string{"code", "code-insiders"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	// macOS bundles install `code` into a fixed path that isn't always on $PATH.
	if runtime.GOOS == "darwin" {
		guess := "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"
		if _, err := os.Stat(guess); err == nil {
			return guess, nil
		}
	}
	return "", ErrNoVSCode
}

func downloadFile(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer func() { _ = out.Close() }()
	// 32 MiB cap protects against an absurdly oversized release asset
	// (the real .vsix is ~30 KB). io.LimitReader keeps memory bounded.
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 32<<20)); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

// verifySignature checks the ed25519 signature in sigPath against the
// embedded public key. Reads the entire VSIX into memory — at ~30 KB
// today and with a 32 MiB cap above, this is bounded enough that we
// don't bother with streaming.
func verifySignature(vsixPath, sigPath string) error {
	pubKey, err := parsePublicKey([]byte(KorvaPubKeyPEM))
	if err != nil {
		return fmt.Errorf("parse embedded pubkey: %w", err)
	}
	body, err := os.ReadFile(vsixPath)
	if err != nil {
		return fmt.Errorf("read vsix: %w", err)
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return fmt.Errorf("read sig: %w", err)
	}
	if !ed25519.Verify(pubKey, body, sig) {
		return errors.New("ed25519.Verify returned false")
	}
	return nil
}

func parsePublicKey(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM data found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX: %w", err)
	}
	ed, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected ed25519 public key, got %T", pub)
	}
	return ed, nil
}
