//go:build !wails

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frpdeck.env")

	in := map[string]string{
		"FRPDECK_LISTEN":         "127.0.0.1:18080",
		"FRPDECK_DATA_DIR":       "/var/lib/frpdeck",
		"FRPDECK_AUTH_MODE":      "password",
		"FRPDECK_ADMIN_USERNAME": "admin",
		"FRPDECK_ADMIN_PASSWORD": "p@ss with space",
		"FRPDECK_JWT_SECRET":     `quoted"value`,
	}
	if err := renderEnvFile(path, in); err != nil {
		t.Fatalf("renderEnvFile: %v", err)
	}

	// Re-read to confirm quoting + unquoting are symmetric. Wipe
	// existing process env first so the non-overwrite policy
	// doesn't mask bugs.
	for k := range in {
		os.Unsetenv(k)
	}
	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	for k, want := range in {
		if got := os.Getenv(k); got != want {
			t.Errorf("env[%s] = %q, want %q", k, got, want)
		}
	}

	// Verify file permission is 0600 (we ship secrets here).
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("env file mode = %o, want 0600", mode)
	}
}

func TestEnvFileNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frpdeck.env")
	if err := os.WriteFile(path, []byte("FRPDECK_LISTEN=:9000\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	os.Setenv("FRPDECK_LISTEN", ":7777") // pre-existing env wins
	defer os.Unsetenv("FRPDECK_LISTEN")

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if got := os.Getenv("FRPDECK_LISTEN"); got != ":7777" {
		t.Errorf("FRPDECK_LISTEN = %q, want :7777 (pre-existing should win)", got)
	}
}

func TestEnvFileQuotingRules(t *testing.T) {
	cases := []struct {
		val      string
		quoted   bool
		whyShort string
	}{
		{"plain", false, "no special chars"},
		{"with space", true, "whitespace"},
		{"has#hash", true, "comment char"},
		{`has"quote`, true, "double quote"},
		{`has\backslash`, true, "backslash"},
		{"", false, "empty"},
	}
	for _, c := range cases {
		got := needsQuoting(c.val)
		if got != c.quoted {
			t.Errorf("needsQuoting(%q) = %v, want %v (%s)", c.val, got, c.quoted, c.whyShort)
		}
	}
}

func TestEnvFileSkipsCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := strings.Join([]string{
		"# leading comment",
		"",
		"FRPDECK_FOO=bar",
		"   # indented comment",
		"FRPDECK_BAZ=\"qux quux\"",
		"= invalid line with no key",
		"NOT_A_FRPDECK_VAR=ignored",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("FRPDECK_FOO")
	os.Unsetenv("FRPDECK_BAZ")
	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if got := os.Getenv("FRPDECK_FOO"); got != "bar" {
		t.Errorf("FRPDECK_FOO = %q, want bar", got)
	}
	if got := os.Getenv("FRPDECK_BAZ"); got != "qux quux" {
		t.Errorf("FRPDECK_BAZ = %q, want %q", got, "qux quux")
	}
}
