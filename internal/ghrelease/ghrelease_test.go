package ghrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"1.2.3":     "v1.2.3",
		"v1.2.3":    "v1.2.3",
		"latest":    "latest",
		"":          "latest",
		" 1.12.0 ":  "v1.12.0",
		"1.12.0-1":  "v1.12.0-1",
		"v1.12.0-1": "v1.12.0-1",
	}
	for in, want := range cases {
		if got := NormalizeVersion(in); got != want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssetURL(t *testing.T) {
	cases := []struct {
		repo, version, asset, want string
	}{
		{Repo, "latest", "tm-linux-amd64", "https://github.com/chr0nzz/tm-cli/releases/latest/download/tm-linux-amd64"},
		{Repo, "v1.12.0", "SHA256SUMS", "https://github.com/chr0nzz/tm-cli/releases/download/v1.12.0/SHA256SUMS"},
		{Repo, "1.12.0", "tm-linux-arm64", "https://github.com/chr0nzz/tm-cli/releases/download/v1.12.0/tm-linux-arm64"},
		{AgentRepo, "", "tma-linux-armv7", "https://github.com/chr0nzz/traefik-manager/releases/latest/download/tma-linux-armv7"},
	}
	for _, c := range cases {
		if got := AssetURL(c.repo, c.version, c.asset); got != c.want {
			t.Errorf("AssetURL(%q, %q, %q) = %q, want %q", c.repo, c.version, c.asset, got, c.want)
		}
	}
}

func sumOf(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func TestVerify(t *testing.T) {
	data := []byte("binary contents")
	other := []byte("other binary")
	sums := []byte(sumOf(other) + "  tm-linux-arm64\n" + sumOf(data) + "  tm-linux-amd64\n")
	if err := Verify(sums, "tm-linux-amd64", data); err != nil {
		t.Errorf("good sum rejected: %v", err)
	}
	if err := Verify(sums, "tm-linux-arm64", other); err != nil {
		t.Errorf("good sum rejected: %v", err)
	}
	err := Verify(sums, "tm-linux-amd64", other)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch for tm-linux-amd64") {
		t.Errorf("bad sum accepted: %v", err)
	}
	err = Verify(sums, "tm-linux-armv7", data)
	if err == nil || !strings.Contains(err.Error(), "no checksum for tm-linux-armv7") {
		t.Errorf("missing entry: %v", err)
	}
	binary := []byte(strings.ToUpper(sumOf(data)) + " *tm-linux-amd64\r\n")
	if err := Verify(binary, "tm-linux-amd64", data); err != nil {
		t.Errorf("binary marker and uppercase hex rejected: %v", err)
	}
	if err := Verify([]byte("zz  tm-linux-amd64\n"), "tm-linux-amd64", data); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Errorf("malformed hash accepted: %v", err)
	}
	if err := Verify(nil, "tm-linux-amd64", data); err == nil {
		t.Errorf("empty sums accepted")
	}
}

func useServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	orig := BaseURL
	BaseURL = srv.URL
	t.Cleanup(func() {
		BaseURL = orig
		srv.Close()
	})
	return srv
}

func TestLatestVersion(t *testing.T) {
	var gotUA, gotMethod string
	useServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotMethod = r.Method
		switch r.URL.Path {
		case "/chr0nzz/tm-cli/releases/latest":
			w.Header().Set("Location", "https://github.com/chr0nzz/tm-cli/releases/tag/v1.12.0")
			w.WriteHeader(http.StatusFound)
		case "/none/none/releases/latest":
			w.WriteHeader(http.StatusNotFound)
		case "/odd/odd/releases/latest":
			w.Header().Set("Location", "https://github.com/odd/odd/releases")
			w.WriteHeader(http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	UserAgent = "tm/test"
	ctx := context.Background()
	v, err := LatestVersion(ctx, Repo)
	if err != nil || v != "v1.12.0" {
		t.Errorf("LatestVersion = %q, %v", v, err)
	}
	if gotUA != "tm/test" || gotMethod != http.MethodHead {
		t.Errorf("request ua=%q method=%q", gotUA, gotMethod)
	}
	if _, err := LatestVersion(ctx, "none/none"); err == nil || !strings.Contains(err.Error(), "no releases found for none/none") {
		t.Errorf("404 error = %v", err)
	}
	if _, err := LatestVersion(ctx, "odd/odd"); err == nil {
		t.Errorf("redirect without tag accepted")
	}
	if _, err := LatestVersion(ctx, "plain/plain"); err == nil {
		t.Errorf("200 without redirect accepted")
	}
}

func TestDownload(t *testing.T) {
	data := []byte("#!/bin/sh\necho tm\n")
	sums := sumOf(data) + "  tm-linux-amd64\n" + sumOf([]byte("x")) + "  tm-linux-arm64\n"
	var hits []string
	useServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		switch r.URL.Path {
		case "/chr0nzz/tm-cli/releases/download/v1.0.0/tm-linux-amd64":
			w.Write(data)
		case "/chr0nzz/tm-cli/releases/download/v1.0.0/tm-linux-arm64":
			w.Write([]byte("corrupted"))
		case "/chr0nzz/tm-cli/releases/download/v1.0.0/SHA256SUMS", "/chr0nzz/tm-cli/releases/latest/download/SHA256SUMS":
			w.Write([]byte(sums))
		case "/chr0nzz/tm-cli/releases/latest/download/tm-linux-amd64":
			w.Write(data)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	ctx := context.Background()
	dir := t.TempDir()
	dest := filepath.Join(dir, "bin", "tm")
	if err := Download(ctx, Repo, "1.0.0", "tm-linux-amd64", dest, 0o755); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(data) {
		t.Fatalf("installed content = %q, %v", got, err)
	}
	fi, _ := os.Stat(dest)
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %o", fi.Mode().Perm())
	}
	if len(hits) != 2 || !strings.HasSuffix(hits[0], "/SHA256SUMS") {
		t.Errorf("expected sums first then asset, got %v", hits)
	}
	dest2 := filepath.Join(dir, "tm-latest")
	if err := Download(ctx, Repo, "latest", "tm-linux-amd64", dest2, 0o700); err != nil {
		t.Errorf("latest download: %v", err)
	}
	err = Download(ctx, Repo, "v1.0.0", "tm-linux-arm64", filepath.Join(dir, "bad"), 0o755)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch for tm-linux-arm64") {
		t.Errorf("mismatch error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "bad")); statErr == nil {
		t.Errorf("corrupted asset was installed")
	}
	err = Download(ctx, Repo, "v1.0.0", "tm-linux-armv7", filepath.Join(dir, "missing"), 0o755)
	if err == nil || !strings.Contains(err.Error(), "no checksum for tm-linux-armv7") {
		t.Errorf("missing entry error = %v", err)
	}
	err = Download(ctx, Repo, "v9.9.9", "tm-linux-amd64", filepath.Join(dir, "nover"), 0o755)
	if err == nil || !strings.Contains(err.Error(), "asset not found: ") || !strings.Contains(err.Error(), "/releases/download/v9.9.9/SHA256SUMS") {
		t.Errorf("404 error = %v", err)
	}
	entries, _ := os.ReadDir(os.TempDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "tm-download-") {
			info, err := e.Info()
			if err == nil && time.Since(info.ModTime()) < time.Minute {
				t.Errorf("temp dir %s not cleaned up", e.Name())
			}
		}
	}
}

func TestIdleReaderStall(t *testing.T) {
	origTimeout := timeout
	timeout = 200 * time.Millisecond
	t.Cleanup(func() { timeout = origTimeout })
	useServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			w.Write([]byte(sumOf([]byte("never")) + "  tm-linux-amd64\n"))
			return
		}
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(600 * time.Millisecond)
	}))
	start := time.Now()
	err := Download(context.Background(), Repo, "v1.0.0", "tm-linux-amd64", filepath.Join(t.TempDir(), "tm"), 0o755)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Errorf("expected stall error, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("stall detection took %s", time.Since(start))
	}
}

func TestDownloadAllowUnverified(t *testing.T) {
	data := []byte("#!/bin/sh\necho tma\n")
	useServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chr0nzz/traefik-manager/releases/latest/download/tma-linux-amd64":
			w.Write(data)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	dest := filepath.Join(t.TempDir(), "tma")
	verified, err := DownloadAllowUnverified(context.Background(), AgentRepo, "latest", "tma-linux-amd64", dest, 0o755)
	if err != nil {
		t.Fatalf("DownloadAllowUnverified: %v", err)
	}
	if verified {
		t.Fatal("expected unverified download when SHA256SUMS is missing")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(data) {
		t.Fatalf("installed content = %q, %v", got, err)
	}
	if err := Download(context.Background(), AgentRepo, "latest", "tma-linux-amd64", dest, 0o755); err == nil {
		t.Fatal("strict Download must fail without SHA256SUMS")
	}
}
