package cmds

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/frpcimport"
	"github.com/teacat99/FrpDeck/internal/model"
)

// NewImportCmd builds the `frpdeck import` command. It mirrors the
// internal/api/handleImportTunnelsCommit behaviour (P5-D import
// pipeline) so a CLI-driven import lands in the same shape as a
// UI-driven one — including the "tunnels are tagged Source=imported"
// audit hint.
//
// We deliberately do NOT create new endpoints from the file: every
// import targets an explicit --endpoint <ref>. The reasoning is the
// same as the API: an endpoint represents a security boundary
// (tokens, TLS), and silently inferring one from a config file
// invites supply-chain confusion ("which token did we end up using?").
func NewImportCmd(opts *GlobalOptions) *cobra.Command {
	var endpointRef string
	var defaultOnConflict string
	var dryRun bool
	c := &cobra.Command{
		Use:   "import <file>",
		Short: "Import tunnels from a frpc.toml/yaml/json file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			plan, err := frpcimport.Parse(data, filepath.Base(path))
			if err != nil {
				return err
			}
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			ep, err := resolveEndpoint(st, endpointRef)
			if err != nil {
				return err
			}
			strategy := normaliseStrategy(defaultOnConflict)
			existing, err := st.ListTunnelsByEndpoint(ep.ID)
			if err != nil {
				return err
			}
			taken := make(map[string]struct{}, len(existing))
			for _, t := range existing {
				taken[strings.ToLower(strings.TrimSpace(t.Name))] = struct{}{}
			}

			results := make([]map[string]any, 0, len(plan.Tunnels))
			created := 0
			for _, draft := range plan.Tunnels {
				name := strings.TrimSpace(draft.Name)
				row := map[string]any{
					"name":     name,
					"type":     draft.Type,
					"status":   "",
					"id":       uint(0),
					"renamed":  "",
					"warnings": strings.Join(draft.Warnings, "; "),
				}
				key := strings.ToLower(name)
				if _, hit := taken[key]; hit {
					switch strategy {
					case "skip":
						row["status"] = "skip"
						results = append(results, row)
						continue
					case "rename":
						newName := uniquify(name, taken)
						row["renamed"] = newName
						name = newName
						key = strings.ToLower(newName)
					case "error", "":
						row["status"] = "error: name conflict"
						results = append(results, row)
						return errExitCode{code: 1, msg: fmt.Sprintf("import aborted on conflict %q (use --default-on-conflict skip|rename to override)", draft.Name)}
					}
				}
				if dryRun {
					row["status"] = "would-create"
					taken[key] = struct{}{}
					results = append(results, row)
					continue
				}
				t := tunnelFromDraft(&draft)
				t.Name = name
				t.EndpointID = ep.ID
				t.Source = model.TunnelSourceImported
				now := time.Now()
				t.CreatedAt = now
				t.UpdatedAt = now
				if t.Status == "" {
					t.Status = model.StatusPending
				}
				if err := st.CreateTunnel(t); err != nil {
					row["status"] = "error: " + err.Error()
					results = append(results, row)
					continue
				}
				row["id"] = t.ID
				row["status"] = "created"
				created++
				taken[key] = struct{}{}
				results = append(results, row)
			}
			if !dryRun && created > 0 {
				if err := pingReconcile(opts.SocketClient); err != nil {
					return err
				}
			}
			cols := []output.Column{
				{Title: "NAME", Key: "name"},
				{Title: "TYPE", Key: "type"},
				{Title: "STATUS", Key: "status"},
				{Title: "ID", Key: "id"},
				{Title: "RENAMED", Key: "renamed"},
				{Title: "WARNINGS", Key: "warnings"},
			}
			if err := output.Render(cmd.OutOrStdout(), opts.Format, cols, results, opts.RenderOpts()...); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "import: %d/%d created (format=%s, endpoint=%s, dry-run=%v)\n", created, len(plan.Tunnels), plan.Format, ep.Name, dryRun)
			return nil
		},
	}
	c.Flags().StringVar(&endpointRef, "endpoint", "", "Target endpoint id or name (required)")
	c.Flags().StringVar(&defaultOnConflict, "default-on-conflict", "error", "Conflict strategy: error | skip | rename")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Print the plan without writing tunnels")
	_ = c.MarkFlagRequired("endpoint")
	return c
}

func normaliseStrategy(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "skip":
		return "skip"
	case "rename":
		return "rename"
	case "", "error":
		return "error"
	default:
		return "error"
	}
}

// uniquify mirrors the API rename path: append "-2", "-3", … until a
// free slot opens up. Map mutation is intentional — we want both the
// caller's "taken" view and our local view to advance.
func uniquify(base string, taken map[string]struct{}) string {
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, hit := taken[strings.ToLower(candidate)]; !hit {
			return candidate
		}
	}
	return base + "-x"
}

// tunnelFromDraft maps a parsed TunnelDraft into a model.Tunnel.
// Only fields the importer is known to populate are copied; the rest
// stay zero-value so the GORM defaults (e.g. Enabled=true) win.
func tunnelFromDraft(d *frpcimport.TunnelDraft) *model.Tunnel {
	return &model.Tunnel{
		Name:              strings.TrimSpace(d.Name),
		Type:              strings.TrimSpace(d.Type),
		Role:              strings.TrimSpace(d.Role),
		LocalIP:           strings.TrimSpace(d.LocalIP),
		LocalPort:         d.LocalPort,
		RemotePort:        d.RemotePort,
		CustomDomains:     strings.TrimSpace(d.CustomDomains),
		Subdomain:         strings.TrimSpace(d.Subdomain),
		Locations:         strings.TrimSpace(d.Locations),
		HTTPUser:          strings.TrimSpace(d.HTTPUser),
		HTTPPassword:      d.HTTPPassword,
		HostHeaderRewrite: strings.TrimSpace(d.HostHeaderRewrite),
		SK:                d.SK,
		AllowUsers:        strings.TrimSpace(d.AllowUsers),
		ServerName:        strings.TrimSpace(d.ServerName),
		Encryption:        d.Encryption,
		Compression:       d.Compression,
		BandwidthLimit:    strings.TrimSpace(d.BandwidthLimit),
		Group:             strings.TrimSpace(d.Group),
		GroupKey:          strings.TrimSpace(d.GroupKey),
		HealthCheckType:   strings.TrimSpace(d.HealthCheckType),
		HealthCheckURL:    strings.TrimSpace(d.HealthCheckURL),
		Plugin:            strings.TrimSpace(d.Plugin),
		PluginConfig:      d.PluginConfig,
		Enabled:           d.Enabled,
		AutoStart:         d.AutoStart,
	}
}
