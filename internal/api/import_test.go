package api

import "testing"

// TestNormaliseConflictStrategy verifies the loose-but-not-too-loose
// matcher: anything we don't explicitly accept becomes "" so the caller
// falls through to the strictest default, never silently skipping.
func TestNormaliseConflictStrategy(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"  ":       "",
		"skip":     importConflictSkip,
		"SKIP":     importConflictSkip,
		" Rename ": importConflictRename,
		"error":    importConflictError,
		"ignore":   "",
		"abort":    "",
	}
	for input, want := range cases {
		if got := normaliseConflictStrategy(input); got != want {
			t.Errorf("normaliseConflictStrategy(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestUniqueImportName confirms the helper picks the lowest unused
// `name-N` suffix so the renamed entry sits next to the original in
// alphabetical UI sorts.
func TestUniqueImportName(t *testing.T) {
	taken := map[string]struct{}{
		"web":   {},
		"web-2": {},
		"web-3": {},
	}
	got := uniqueImportName("web", taken)
	if got != "web-4" {
		t.Errorf("uniqueImportName(web) = %q, want web-4", got)
	}

	got = uniqueImportName("api", taken)
	if got != "api-2" {
		t.Errorf("uniqueImportName(api) = %q, want api-2", got)
	}

	// Empty base falls back to "imported".
	got = uniqueImportName("", taken)
	if got != "imported-2" {
		t.Errorf("uniqueImportName(\"\") = %q, want imported-2", got)
	}
}
