// Package output centralises how the CLI renders its data. Three
// formats are supported:
//
//   - "table" (default): aligned columns with a header row, optimised
//     for human reading and copy/paste from a terminal.
//   - "json": newline-terminated JSON, two-space indented, suitable
//     for piping into jq.
//   - "yaml": YAML, suitable for hand-editing into config files.
//
// The package is intentionally tiny: every renderer takes a slice of
// rows + a slice of column definitions and writes to an io.Writer.
// We chose this shape over reflection-based "look at struct tags"
// helpers because the column set per command is rarely 1:1 with the
// underlying model — we want to render derived columns ("status",
// "expires in") that don't exist on disk.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format enumerates the supported output formats. Use ParseFormat
// to convert a CLI flag string into one of these so the validation
// is centralised.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// ParseFormat converts the user-supplied --output flag into a
// validated Format. Empty defaults to table for friendly TTY
// output; everything else returns an error so the CLI can refuse
// the request before doing any work.
func ParseFormat(raw string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "table":
		return FormatTable, nil
	case "json":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	default:
		return "", fmt.Errorf("unsupported output format %q (expected table | json | yaml)", raw)
	}
}

// Column declares one renderable column of a table. Title is shown
// in the header; key matches the field name in the row map.
type Column struct {
	Title string
	Key   string
}

// Render writes rows to out in the chosen format. JSON / YAML
// preserve the row maps verbatim (so consumers see the same field
// names regardless of which format is requested); table format
// uses the column definitions for header + ordering.
func Render(out io.Writer, format Format, columns []Column, rows []map[string]any, opts ...Option) error {
	cfg := newConfig(opts...)
	switch format {
	case FormatTable:
		return renderTable(out, columns, rows, cfg)
	case FormatJSON:
		return renderJSON(out, rows)
	case FormatYAML:
		return renderYAML(out, rows)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

// RenderSingle is sugar for "I have a single object, render it as a
// vertical key:value list". JSON / YAML behave identically to a
// one-row Render call; table format uses Key:\tValue layout instead
// of header + row.
func RenderSingle(out io.Writer, format Format, columns []Column, row map[string]any, opts ...Option) error {
	cfg := newConfig(opts...)
	switch format {
	case FormatTable:
		return renderKeyValue(out, columns, row, cfg)
	case FormatJSON:
		return renderJSON(out, row)
	case FormatYAML:
		return renderYAML(out, row)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

// RenderRaw emits an arbitrary value (typically a typed struct) in
// JSON or YAML form. Table format is rejected — callers must
// project into a row map and use Render / RenderSingle for tabular
// output. Useful when the daemon returns a strongly-typed RPC
// payload (e.g. remoteops.InvitationResult) and the CLI wants to
// pass it through to JSON/YAML consumers without fudging its
// shape.
func RenderRaw(out io.Writer, format Format, v any) error {
	switch format {
	case FormatJSON:
		return renderJSON(out, v)
	case FormatYAML:
		return renderYAML(out, v)
	case FormatTable:
		return fmt.Errorf("RenderRaw does not support table format; project into rows first")
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

// Option configures a Render call. Currently the only knob is
// "skip the header row" so pipelines like
//
//	frpdeck endpoint list --no-headers | awk '{print $2}'
//
// stay clean.
type Option func(*config)

// WithoutHeaders disables the table-format header row.
func WithoutHeaders() Option { return func(c *config) { c.noHeaders = true } }

type config struct {
	noHeaders bool
}

func newConfig(opts ...Option) config {
	var c config
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

func renderTable(out io.Writer, columns []Column, rows []map[string]any, cfg config) error {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if !cfg.noHeaders {
		titles := make([]string, len(columns))
		for i, c := range columns {
			titles[i] = c.Title
		}
		if _, err := fmt.Fprintln(tw, strings.Join(titles, "\t")); err != nil {
			return err
		}
	}
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cells[i] = stringify(row[c.Key])
		}
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func renderKeyValue(out io.Writer, columns []Column, row map[string]any, _ config) error {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for _, c := range columns {
		if _, err := fmt.Fprintf(tw, "%s:\t%s\n", c.Title, stringify(row[c.Key])); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func renderJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func renderYAML(out io.Writer, v any) error {
	enc := yaml.NewEncoder(out)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(v)
}

// stringify maps the typed `any` values from a row map into the
// strings table/key-value renderers ultimately want. It is
// intentionally narrow: bool / int / string / time.Time-style
// duration strings cover every column in the FrpDeck CLI today.
// Adding more types here is fine; do NOT add fmt.Sprintf("%+v") as
// a fallback because that prints quotes around strings and breaks
// tabular alignment.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", t)
	case int32:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case uint:
		return fmt.Sprintf("%d", t)
	case uint32:
		return fmt.Sprintf("%d", t)
	case uint64:
		return fmt.Sprintf("%d", t)
	case float32:
		return fmt.Sprintf("%g", t)
	case float64:
		return fmt.Sprintf("%g", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// Stdout is the io.Writer most commands target. Centralising it
// makes tests easier (swap in a bytes.Buffer in tests) and lets us
// add colour gating in one place if we ever want to.
func Stdout() io.Writer { return os.Stdout }
