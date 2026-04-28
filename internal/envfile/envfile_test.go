package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
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
	if err := Write(path, in); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for k, want := range in {
		if got[k] != want {
			t.Errorf("env[%s] = %q, want %q", k, got[k], want)
		}
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("env file mode = %o, want 0600", mode)
	}
}

func TestLoadIntoProcessRespectsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frpdeck.env")
	if err := os.WriteFile(path, []byte("FRPDECK_LISTEN=:9000\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FRPDECK_LISTEN", ":7777")
	if err := LoadIntoProcess(path); err != nil {
		t.Fatalf("LoadIntoProcess: %v", err)
	}
	if got := os.Getenv("FRPDECK_LISTEN"); got != ":7777" {
		t.Errorf("FRPDECK_LISTEN = %q, want :7777 (pre-existing should win)", got)
	}
}

func TestNeedsQuoting(t *testing.T) {
	cases := []struct {
		val    string
		quoted bool
		why    string
	}{
		{"plain", false, "no special chars"},
		{"with space", true, "whitespace"},
		{"has#hash", true, "comment char"},
		{`has"quote`, true, "double quote"},
		{`has\backslash`, true, "backslash"},
		{"", false, "empty"},
	}
	for _, c := range cases {
		got := NeedsQuoting(c.val)
		if got != c.quoted {
			t.Errorf("NeedsQuoting(%q) = %v, want %v (%s)", c.val, got, c.quoted, c.why)
		}
	}
}

func TestReadSkipsCommentsAndBlanks(t *testing.T) {
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
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got["FRPDECK_FOO"] != "bar" {
		t.Errorf("FOO = %q, want bar", got["FRPDECK_FOO"])
	}
	if got["FRPDECK_BAZ"] != "qux quux" {
		t.Errorf("BAZ = %q, want %q", got["FRPDECK_BAZ"], "qux quux")
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty key should be skipped")
	}
}
