package cmds

import (
	"testing"

	"github.com/teacat99/FrpDeck/internal/templates"
)

func TestTunnelFromTemplate_SSH(t *testing.T) {
	tpl, err := templates.FindByID("ssh")
	if err != nil || tpl == nil {
		t.Fatalf("FindByID(ssh): %v / %v", err, tpl)
	}
	tu, err := tunnelFromTemplate(tpl)
	if err != nil {
		t.Fatalf("tunnelFromTemplate: %v", err)
	}
	if tu.Type != "tcp" {
		t.Errorf("type = %q, want tcp", tu.Type)
	}
	if tu.LocalIP != "127.0.0.1" {
		t.Errorf("local_ip = %q, want 127.0.0.1", tu.LocalIP)
	}
	if tu.LocalPort != 22 || tu.RemotePort != 22022 {
		t.Errorf("ports = %d/%d, want 22/22022", tu.LocalPort, tu.RemotePort)
	}
	if !tu.Enabled || !tu.AutoStart {
		t.Errorf("expected enabled+auto_start true, got %v/%v", tu.Enabled, tu.AutoStart)
	}
}

func TestIntFromAny(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{42, 42, true},
		{int64(7), 7, true},
		{float64(8), 8, true},
		{"9", 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := intFromAny(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("intFromAny(%v) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNormaliseStrategy(t *testing.T) {
	cases := map[string]string{
		"":         "error",
		"error":    "error",
		"SKIP":     "skip",
		"  rename": "rename",
		"bogus":    "error",
	}
	for in, want := range cases {
		if got := normaliseStrategy(in); got != want {
			t.Errorf("normaliseStrategy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniquify(t *testing.T) {
	taken := map[string]struct{}{
		"ssh":   {},
		"ssh-2": {},
	}
	got := uniquify("ssh", taken)
	if got != "ssh-3" {
		t.Errorf("uniquify(ssh) = %q, want ssh-3", got)
	}
	if got := uniquify("rdp", taken); got != "rdp-2" {
		t.Errorf("uniquify(rdp) = %q, want rdp-2", got)
	}
}
