package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	cases := []struct {
		in   string
		want Format
		err  bool
	}{
		{"", FormatTable, false},
		{"table", FormatTable, false},
		{"TABLE", FormatTable, false},
		{"json", FormatJSON, false},
		{"yaml", FormatYAML, false},
		{"yml", FormatYAML, false},
		{"toml", "", true},
	}
	for _, tc := range cases {
		got, err := ParseFormat(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("ParseFormat(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseFormat(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseFormat(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	cols := []Column{{Title: "ID", Key: "id"}, {Title: "Name", Key: "name"}}
	rows := []map[string]any{
		{"id": uint(1), "name": "alpha"},
		{"id": uint(2), "name": "beta"},
	}
	if err := Render(&buf, FormatTable, cols, rows); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "Name") {
		t.Errorf("missing header: %q", out)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("missing rows: %q", out)
	}
}

func TestRenderTableWithoutHeaders(t *testing.T) {
	var buf bytes.Buffer
	cols := []Column{{Title: "ID", Key: "id"}}
	rows := []map[string]any{{"id": uint(1)}}
	if err := Render(&buf, FormatTable, cols, rows, WithoutHeaders()); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "ID") {
		t.Errorf("unexpected header: %q", out)
	}
	if !strings.Contains(out, "1") {
		t.Errorf("missing row: %q", out)
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	cols := []Column{{Title: "Name", Key: "name"}}
	rows := []map[string]any{{"name": "alpha"}, {"name": "beta"}}
	if err := Render(&buf, FormatJSON, cols, rows); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "\"name\"") || !strings.Contains(out, "alpha") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRenderYAML(t *testing.T) {
	var buf bytes.Buffer
	cols := []Column{{Title: "Name", Key: "name"}}
	rows := []map[string]any{{"name": "alpha"}}
	if err := Render(&buf, FormatYAML, cols, rows); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "alpha") {
		t.Errorf("missing payload: %q", buf.String())
	}
}

func TestRenderSingleKeyValue(t *testing.T) {
	var buf bytes.Buffer
	cols := []Column{{Title: "ID", Key: "id"}, {Title: "Name", Key: "name"}}
	row := map[string]any{"id": uint(1), "name": "alpha"}
	if err := RenderSingle(&buf, FormatTable, cols, row); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ID:") || !strings.Contains(out, "Name:") {
		t.Errorf("missing key labels: %q", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("missing value: %q", out)
	}
}
