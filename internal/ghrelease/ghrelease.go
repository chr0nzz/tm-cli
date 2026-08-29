package ghrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chr0nzz/tm-cli/internal/host"
)

const (
	Repo        = "chr0nzz/tm-cli"
	AgentRepo   = "chr0nzz/traefik-manager"
	TraefikRepo = "traefik/traefik"
	SumsFile    = "SHA256SUMS"
)

var (
	UserAgent = "tm"
	BaseURL   = "https://github.com"

	timeout = 60 * time.Second
)

func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "latest" {
		return "latest"
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func AssetURL(repo, version, asset string) string {
	version = NormalizeVersion(version)
	if version == "latest" {
		return fmt.Sprintf("%s/%s/releases/latest/download/%s", BaseURL, repo, asset)
	}
	return fmt.Sprintf("%s/%s/releases/download/%s/%s", BaseURL, repo, version, asset)
}

func transport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
}

func newRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	return req, nil
}

func LatestVersion(ctx context.Context, repo string) (string, error) {
	url := fmt.Sprintf("%s/%s/releases/latest", BaseURL, repo)
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport(),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for hop := 0; hop < 5; hop++ {
		req, err := newRequest(ctx, http.MethodHead, url)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("fetch %s: %w", url, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("no releases found for %s", repo)
		}
		loc := resp.Header.Get("Location")
		if resp.StatusCode < 300 || resp.StatusCode > 399 || loc == "" {
			return "", fmt.Errorf("fetch %s: unexpected response %s", url, resp.Status)
		}
		if _, tag, ok := strings.Cut(loc, "/tag/"); ok {
			tag = strings.TrimSuffix(strings.TrimSpace(tag), "/")
			if tag == "" {
				return "", fmt.Errorf("fetch %s: unexpected redirect to %s", url, loc)
			}
			return tag, nil
		}
		next, err := resolveRedirect(url, loc)
		if err != nil {
			return "", fmt.Errorf("fetch %s: unexpected redirect to %s", url, loc)
		}
		url = next
	}
	return "", fmt.Errorf("too many redirects looking up the latest release of %s", repo)
}

func resolveRedirect(from, loc string) (string, error) {
	base, err := neturl.Parse(from)
	if err != nil {
		return "", err
	}
	next, err := base.Parse(loc)
	if err != nil {
		return "", err
	}
	return next.String(), nil
}

var errAssetNotFound = errors.New("asset not found")

func Download(ctx context.Context, repo, version, asset, dest string, mode os.FileMode) error {
	_, err := download(ctx, repo, version, asset, dest, mode, false)
	return err
}

func DownloadAllowUnverified(ctx context.Context, repo, version, asset, dest string, mode os.FileMode) (bool, error) {
	return download(ctx, repo, version, asset, dest, mode, true)
}

func download(ctx context.Context, repo, version, asset, dest string, mode os.FileMode, allowMissingSums bool) (bool, error) {
	tmpDir, err := os.MkdirTemp("", "tm-download-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmpDir)
	sumsPath := filepath.Join(tmpDir, SumsFile)
	var sums []byte
	if err := fetchToFile(ctx, AssetURL(repo, version, SumsFile), sumsPath); err != nil {
		if !allowMissingSums || !errors.Is(err, errAssetNotFound) {
			return false, err
		}
	} else {
		sums, err = os.ReadFile(sumsPath)
		if err != nil {
			return false, err
		}
		if _, err := expectedSum(sums, asset); err != nil {
			return false, err
		}
	}
	tmp := filepath.Join(tmpDir, asset)
	if err := fetchToFile(ctx, AssetURL(repo, version, asset), tmp); err != nil {
		return false, err
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return false, err
	}
	if sums != nil {
		if err := Verify(sums, asset, data); err != nil {
			return false, err
		}
	}
	if err := host.WriteFile(dest, data, mode); err != nil {
		return false, fmt.Errorf("install %s: %w", dest, err)
	}
	if sums == nil {
		return false, nil
	}
	return true, nil
}

func fetchToFile(ctx context.Context, url, path string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := &http.Client{Transport: transport()}
	req, err := newRequest(ctx, http.MethodGet, url)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", errAssetNotFound, url)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	body := newIdleReader(resp.Body, timeout, cancel)
	defer body.stop()
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		if body.stalled() {
			return fmt.Errorf("download %s: stalled for %s", url, timeout)
		}
		return fmt.Errorf("download %s: %w", url, err)
	}
	return f.Close()
}

type idleReader struct {
	r     io.Reader
	d     time.Duration
	timer *time.Timer
	fired atomic.Bool
}

func newIdleReader(r io.Reader, d time.Duration, cancel func()) *idleReader {
	ir := &idleReader{r: r, d: d}
	ir.timer = time.AfterFunc(d, func() {
		ir.fired.Store(true)
		cancel()
	})
	return ir
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.timer.Reset(ir.d)
	}
	return n, err
}

func (ir *idleReader) stop()         { ir.timer.Stop() }
func (ir *idleReader) stalled() bool { return ir.fired.Load() }

func Verify(sums []byte, asset string, data []byte) error {
	want, err := expectedSum(sums, asset)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", asset, want, got)
	}
	return nil
}

func expectedSum(sums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		hash, name, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		name = strings.TrimPrefix(name, "*")
		name = strings.TrimPrefix(name, "./")
		if name != asset {
			continue
		}
		hash = strings.ToLower(strings.TrimSpace(hash))
		if len(hash) != sha256.Size*2 {
			return "", fmt.Errorf("malformed checksum for %s in the checksums file", asset)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return "", fmt.Errorf("malformed checksum for %s in the checksums file", asset)
		}
		return hash, nil
	}
	return "", errors.New("no checksum for " + asset + " in the checksums file")
}

func TraefikAsset(version, arch string) string {
	return "traefik_" + NormalizeVersion(version) + "_linux_" + arch + ".tar.gz"
}

func TraefikChecksums(version string) string {
	return "traefik_" + NormalizeVersion(version) + "_checksums.txt"
}

func FetchTraefikBinary(ctx context.Context, version, arch string) ([]byte, error) {
	version = NormalizeVersion(version)
	if version == "latest" {
		return nil, errors.New("a pinned traefik version is required: resolve the latest tag first")
	}
	asset := TraefikAsset(version, arch)
	tmpDir, err := os.MkdirTemp("", "tm-download-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	sumsPath := filepath.Join(tmpDir, TraefikChecksums(version))
	if err := fetchToFile(ctx, AssetURL(TraefikRepo, version, TraefikChecksums(version)), sumsPath); err != nil {
		return nil, err
	}
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return nil, err
	}
	if _, err := expectedSum(sums, asset); err != nil {
		return nil, err
	}
	tarPath := filepath.Join(tmpDir, asset)
	if err := fetchToFile(ctx, AssetURL(TraefikRepo, version, asset), tarPath); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(tarPath)
	if err != nil {
		return nil, err
	}
	if err := Verify(sums, asset, data); err != nil {
		return nil, err
	}
	bin, err := extractTarMember(data, "traefik")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", asset, err)
	}
	return bin, nil
}

func extractTarMember(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.TrimPrefix(path.Clean(hdr.Name), "./") != name {
			continue
		}
		member, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s from tar: %w", name, err)
		}
		return member, nil
	}
	return nil, fmt.Errorf("no %s member in the archive", name)
}
