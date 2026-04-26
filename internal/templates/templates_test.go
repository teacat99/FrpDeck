package templates

import (
	"testing"
)

// expectedIDs is the canonical 10 scenarios from plan.md §7.2. The
// test fails the build if anyone removes one without updating the
// design doc — this keeps marketing claims and reality in sync.
var expectedIDs = []string{
	"db-share",
	"frpdeck-self",
	"http-proxy",
	"nas-p2p",
	"rdp",
	"socks5",
	"ssh",
	"static-file",
	"web-http",
	"web-https",
}

func TestAll_HasExactlyTenTemplates(t *testing.T) {
	got, err := All()
	if err != nil {
		t.Fatalf("All() error: %v", err)
	}
	if len(got) != len(expectedIDs) {
		t.Fatalf("expected %d templates, got %d", len(expectedIDs), len(got))
	}
	for i, want := range expectedIDs {
		if got[i].ID != want {
			t.Errorf("templates[%d].ID = %q; want %q", i, got[i].ID, want)
		}
	}
}

func TestAll_AllTemplatesHaveRequiredFields(t *testing.T) {
	got, err := All()
	if err != nil {
		t.Fatalf("All() error: %v", err)
	}
	for _, tpl := range got {
		if tpl.NameKey == "" {
			t.Errorf("%s: missing name", tpl.ID)
		}
		if tpl.DescKey == "" {
			t.Errorf("%s: missing description", tpl.ID)
		}
		if tpl.AudienceKey == "" {
			t.Errorf("%s: missing audience", tpl.ID)
		}
		if tpl.Defaults == nil {
			t.Errorf("%s: nil defaults", tpl.ID)
			continue
		}
		// Every template must declare a `type` so the frontend can
		// pre-route the user to the correct form section.
		if _, ok := tpl.Defaults["type"]; !ok {
			t.Errorf("%s: defaults missing 'type'", tpl.ID)
		}
	}
}

func TestFindByID(t *testing.T) {
	tpl, err := FindByID("ssh")
	if err != nil {
		t.Fatalf("FindByID(ssh) error: %v", err)
	}
	if tpl == nil {
		t.Fatalf("FindByID(ssh) returned nil")
	}
	if tpl.Defaults["type"] != "tcp" {
		t.Errorf("ssh template should use tcp, got %v", tpl.Defaults["type"])
	}
	if tpl.Defaults["remote_port"] != 22022 {
		t.Errorf("ssh template should pre-fill remote_port=22022, got %v (%T)",
			tpl.Defaults["remote_port"], tpl.Defaults["remote_port"])
	}
}

func TestFindByID_NotFound(t *testing.T) {
	tpl, err := FindByID("does-not-exist")
	if err != nil {
		t.Fatalf("FindByID error: %v", err)
	}
	if tpl != nil {
		t.Errorf("expected nil for unknown id, got %+v", tpl)
	}
}

func TestVisitorTemplateUsesVisitorRole(t *testing.T) {
	tpl, err := FindByID("nas-p2p")
	if err != nil || tpl == nil {
		t.Fatalf("nas-p2p template missing: %v", err)
	}
	if tpl.Defaults["role"] != "visitor" {
		t.Errorf("nas-p2p must default to visitor role, got %v", tpl.Defaults["role"])
	}
}

func TestDBShareTemplateCarriesExpireSeconds(t *testing.T) {
	tpl, err := FindByID("db-share")
	if err != nil || tpl == nil {
		t.Fatalf("db-share template missing: %v", err)
	}
	v, ok := tpl.Defaults["expire_in_seconds"]
	if !ok {
		t.Fatalf("db-share should pre-fill expire_in_seconds")
	}
	if n, ok := v.(int); !ok || n <= 0 {
		t.Errorf("expire_in_seconds should be a positive int, got %v (%T)", v, v)
	}
}
