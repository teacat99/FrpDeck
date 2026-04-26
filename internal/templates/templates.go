// Package templates exposes the 10 scenario templates documented in
// plan.md §7.2. Each template is a YAML file under data/ that is
// embedded into the Go binary at compile time, so adding/changing
// templates only requires editing YAML and rebuilding — no code
// edits needed for content updates.
//
// The loader is intentionally read-only: callers receive an
// immutable slice; the wizard frontend then merges the chosen
// template's `defaults` map into the create-tunnel payload.
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed data/*.yaml
var dataFS embed.FS

// Template is the in-memory shape of a single scenario template.
//
// Fields ending in "Key" carry an i18n key suffix (rendered as
// `template.<id>.<key>` on the frontend) instead of localised text;
// this keeps zh-CN and en-US translation in the standard catalog
// next to every other UI string.
//
// Defaults is a free-form map mirroring `internal/api/dto.go`
// `tunnelReq` field names. Anything the frontend doesn't recognise
// is silently ignored; this lets us add new template knobs without
// shipping a backend change.
type Template struct {
	ID          string                 `yaml:"id" json:"id"`
	Icon        string                 `yaml:"icon" json:"icon,omitempty"`
	NameKey     string                 `yaml:"name" json:"name_key"`
	DescKey     string                 `yaml:"description" json:"description_key"`
	AudienceKey string                 `yaml:"audience" json:"audience_key"`
	PrereqKeys  []string               `yaml:"prereq" json:"prereq_keys,omitempty"`
	Defaults    map[string]interface{} `yaml:"defaults" json:"defaults"`
}

// All returns every embedded template, sorted by ID for stable
// rendering. The slice is a fresh copy on each call; callers may
// mutate it freely (templates themselves are values).
//
// On corrupt/invalid YAML this returns an error rather than a
// partial list — better to fail the API call than silently drop
// the template the operator just added.
func All() ([]Template, error) {
	entries, err := fs.ReadDir(dataFS, "data")
	if err != nil {
		return nil, fmt.Errorf("templates: read embed dir: %w", err)
	}
	out := make([]Template, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := fs.ReadFile(dataFS, "data/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("templates: read %s: %w", e.Name(), err)
		}
		var t Template
		if err := yaml.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("templates: parse %s: %w", e.Name(), err)
		}
		if t.ID == "" {
			return nil, fmt.Errorf("templates: %s missing id", e.Name())
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// FindByID returns the template with the matching ID, or nil if it
// doesn't exist. Used for tests and for any future "apply template
// to existing tunnel" feature.
func FindByID(id string) (*Template, error) {
	all, err := All()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, nil
}
