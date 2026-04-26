// Package frpcd — frpc binary download / version probe (P8-A + P8-B).
//
// Two utilities live here:
//
//   - DownloadFrpc fetches a release tarball from github.com/fatedier/frp,
//     verifies a caller-supplied SHA256 hash, extracts the `frpc` binary,
//     and writes it to <dataDir>/bin/frpc-<version>. Verifying the hash
//     keeps "FrpDeck downloads frpc for me" honest about supply-chain
//     boundaries — a future revision will source the hash from a signed
//     manifest committed alongside FrpDeck releases.
//
//   - ProbeFrpcVersion runs `frpc -v` and returns the parsed version. The
//     UI calls this when the operator types a custom subprocess_path so a
//     <v0.52 binary (INI era) is rejected before it ever runs.

package frpcd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// MinimumFrpVersion gates the SubprocessDriver: anything older shipped
// only with the legacy INI config which FrpDeck does not support.
const MinimumFrpVersion = "v0.52.0"

// DownloadOptions parameterises the GitHub release fetch. Version must
// be a tag (e.g. "v0.68.1"); ExpectedSHA256 is the lower-case hex digest
// of the .tar.gz / .zip release archive.
type DownloadOptions struct {
	Version        string
	OS             string // "linux" / "darwin" / "windows"
	Arch           string // "amd64" / "arm64"
	ExpectedSHA256 string
	DataDir        string
	HTTPClient     *http.Client
}

// DownloadFrpc fetches and installs the frpc binary, returning the
// final on-disk path (e.g. <dataDir>/bin/frpc-v0.68.1). Empty fields in
// opts default to the running OS/arch and the caller's HTTP client.
func DownloadFrpc(ctx context.Context, opts DownloadOptions) (string, error) {
	if opts.Version == "" {
		return "", errors.New("download: version required")
	}
	if opts.DataDir == "" {
		return "", errors.New("download: data dir required")
	}
	if opts.OS == "" {
		opts.OS = runtime.GOOS
	}
	if opts.Arch == "" {
		opts.Arch = runtime.GOARCH
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}

	binDir := filepath.Join(opts.DataDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin dir: %w", err)
	}
	target := filepath.Join(binDir, "frpc-"+opts.Version)
	if opts.OS == "windows" {
		target += ".exe"
	}

	if _, err := os.Stat(target); err == nil {
		// Already installed for this version — caller can re-call this
		// safely as part of "ensure binary".
		return target, nil
	}

	url := releaseAssetURL(opts.Version, opts.OS, opts.Arch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d for %s", resp.StatusCode, url)
	}

	hasher := sha256.New()
	body := io.TeeReader(resp.Body, hasher)

	tmp, err := os.CreateTemp(binDir, "frpc-download-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := io.Copy(tmp, body); err != nil {
		return "", fmt.Errorf("download body: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	gotHash := hex.EncodeToString(hasher.Sum(nil))
	if expected := strings.TrimSpace(strings.ToLower(opts.ExpectedSHA256)); expected != "" {
		if gotHash != expected {
			return "", fmt.Errorf("download: sha256 mismatch (got %s want %s)", gotHash, expected)
		}
	}

	switch {
	case strings.HasSuffix(url, ".tar.gz"), strings.HasSuffix(url, ".tgz"):
		if err := extractFrpcFromTarGz(tmpPath, target); err != nil {
			return "", err
		}
	case strings.HasSuffix(url, ".zip"):
		return "", errors.New("download: zip extraction not implemented yet")
	default:
		return "", fmt.Errorf("download: unsupported archive %q", url)
	}

	if err := os.Chmod(target, 0o755); err != nil {
		return "", fmt.Errorf("chmod %q: %w", target, err)
	}
	return target, nil
}

// releaseAssetURL builds the canonical GitHub release URL for the
// requested OS/arch. We always prefer .tar.gz on Unix and .zip on
// Windows — same convention upstream uses.
func releaseAssetURL(version, goOS, goArch string) string {
	osPart := goOS
	if osPart == "windows" {
		return fmt.Sprintf(
			"https://github.com/fatedier/frp/releases/download/%s/frp_%s_%s_%s.zip",
			version, strings.TrimPrefix(version, "v"), osPart, goArch,
		)
	}
	return fmt.Sprintf(
		"https://github.com/fatedier/frp/releases/download/%s/frp_%s_%s_%s.tar.gz",
		version, strings.TrimPrefix(version, "v"), osPart, goArch,
	)
}

// extractFrpcFromTarGz pulls just the `frpc` (or `frpc.exe`) entry out
// of the upstream archive. The release tarball layout is:
//
//	frp_0.68.1_linux_amd64/
//	  frpc
//	  frps
//	  ...
//
// so we accept any path ending in `/frpc` or matching `frpc` exactly.
func extractFrpcFromTarGz(tarGzPath, dest string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base != "frpc" && base != "frpc.exe" {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.CopyN(out, tr, hdr.Size); err != nil {
			out.Close()
			return err
		}
		out.Close()
		return nil
	}
	return errors.New("download: frpc binary not found in archive")
}

// ProbeFrpcVersion runs `frpc -v` and returns the trimmed version string.
// frpc historically prints either `frpc version <ver>` or just the
// version on its own line; we accept both.
func ProbeFrpcVersion(ctx context.Context, binary string) (string, error) {
	if binary == "" {
		return "", errors.New("probe: binary path required")
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, binary, "-v")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("probe %q: %w (%s)", binary, err, strings.TrimSpace(string(out)))
	}
	return parseFrpcVersion(string(out))
}

// parseFrpcVersion extracts the leading "vX.Y.Z" token from `frpc -v`
// output. Tolerates both "frpc version 0.68.1" and "0.68.1\n".
func parseFrpcVersion(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("probe: empty output")
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		token := fields[len(fields)-1]
		if !strings.HasPrefix(token, "v") {
			token = "v" + token
		}
		if isSemverLike(token) {
			return token, nil
		}
	}
	return "", fmt.Errorf("probe: cannot parse version from %q", s)
}

// isSemverLike checks for a leading "vX.Y.Z" token without dragging in a
// full semver dependency for what is essentially a sanity probe.
func isSemverLike(s string) bool {
	if !strings.HasPrefix(s, "v") {
		return false
	}
	parts := strings.Split(s[1:], ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts[:2] {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// CompareVersion returns true if `have` >= `want`. Both are expected to
// be vMAJOR.MINOR.PATCH; any non-numeric tail (e.g. "-rc1") is ignored
// for the purposes of the comparison.
func CompareVersion(have, want string) bool {
	hv := parseSemver(have)
	wv := parseSemver(want)
	for i := 0; i < 3; i++ {
		if hv[i] != wv[i] {
			return hv[i] > wv[i]
		}
	}
	return true
}

func parseSemver(v string) [3]int {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	out := [3]int{0, 0, 0}
	parts := strings.SplitN(v, ".", 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		seg := parts[i]
		num := 0
		for _, r := range seg {
			if r < '0' || r > '9' {
				break
			}
			num = num*10 + int(r-'0')
		}
		out[i] = num
	}
	return out
}
