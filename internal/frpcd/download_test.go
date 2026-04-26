package frpcd

import (
	"strings"
	"testing"
)

func TestParseFrpcVersion(t *testing.T) {
	cases := map[string]string{
		"frpc version 0.68.1\n":      "v0.68.1",
		"v0.52.3":                    "v0.52.3",
		"v1.0.0\nGit: ...\n":         "v1.0.0",
		"frpc version v0.68.0-rc1\n": "v0.68.0-rc1",
	}
	for raw, want := range cases {
		got, err := parseFrpcVersion(raw)
		if err != nil {
			t.Errorf("parseFrpcVersion(%q) err: %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("parseFrpcVersion(%q)=%q want %q", raw, got, want)
		}
	}
}

func TestParseFrpcVersion_Bad(t *testing.T) {
	for _, raw := range []string{"", "  ", "no-version-here", "abc.def.ghi"} {
		if _, err := parseFrpcVersion(raw); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}

func TestCompareVersion(t *testing.T) {
	if !CompareVersion("v0.68.1", "v0.52.0") {
		t.Errorf("0.68.1 should be >= 0.52.0")
	}
	if !CompareVersion("v0.52.0", "v0.52.0") {
		t.Errorf("equal should compare >=")
	}
	if CompareVersion("v0.51.5", "v0.52.0") {
		t.Errorf("0.51.5 should be < 0.52.0")
	}
	if !CompareVersion("v1.0.0", "v0.99.99") {
		t.Errorf("1.0.0 should be >= 0.99.99")
	}
	// pre-release tail ignored
	if !CompareVersion("v0.68.0-rc1", "v0.52.0") {
		t.Errorf("rc tag should not break comparison")
	}
}

func TestReleaseAssetURL(t *testing.T) {
	if got := releaseAssetURL("v0.68.1", "linux", "amd64"); !strings.HasSuffix(got, "frp_0.68.1_linux_amd64.tar.gz") {
		t.Errorf("linux url %q", got)
	}
	if got := releaseAssetURL("v0.68.1", "windows", "amd64"); !strings.HasSuffix(got, "frp_0.68.1_windows_amd64.zip") {
		t.Errorf("windows url %q", got)
	}
}
