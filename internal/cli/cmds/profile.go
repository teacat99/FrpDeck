package cmds

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

// NewProfileCmd builds the `frpdeck profile` command tree.
//
// Activate is the only command with reconciler-relevant side effects:
// store.ActivateProfile() flips Endpoint.Enabled / Tunnel.Enabled to
// match the bound set, then we ping the daemon so the change takes
// effect immediately. add/update/remove without activate only mutate
// metadata so reconcile is unnecessary, but we ping anyway — costs
// nothing and keeps the contract uniform.
func NewProfileCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "profile",
		Short: "Manage tunnel-set profiles",
	}
	c.AddCommand(
		newProfileListCmd(opts),
		newProfileGetCmd(opts),
		newProfileAddCmd(opts),
		newProfileUpdateCmd(opts),
		newProfileRemoveCmd(opts),
		newProfileActivateCmd(opts),
		newProfileDeactivateCmd(opts),
	)
	return c
}

func newProfileListCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all profiles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			profs, err := st.ListProfiles()
			if err != nil {
				return err
			}
			rows := make([]map[string]any, len(profs))
			for i, p := range profs {
				bindings, err := st.ListProfileBindings(p.ID)
				if err != nil {
					return err
				}
				rows[i] = map[string]any{
					"id":         p.ID,
					"name":       p.Name,
					"active":     p.Active,
					"bindings":   len(bindings),
					"created_at": p.CreatedAt.Format(time.RFC3339),
				}
			}
			cols := []output.Column{
				{Title: "ID", Key: "id"},
				{Title: "NAME", Key: "name"},
				{Title: "ACTIVE", Key: "active"},
				{Title: "BINDINGS", Key: "bindings"},
				{Title: "CREATED", Key: "created_at"},
			}
			return output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...)
		},
	}
}

func newProfileGetCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id|name>",
		Short: "Show profile details + bound endpoints/tunnels",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			p, err := resolveProfile(st, args[0])
			if err != nil {
				return err
			}
			bindings, err := st.ListProfileBindings(p.ID)
			if err != nil {
				return err
			}
			endpointNames, err := buildEndpointNameMap(st)
			if err != nil {
				return err
			}
			tunnels, err := st.ListTunnels()
			if err != nil {
				return err
			}
			tunnelNames := map[uint]string{}
			for _, t := range tunnels {
				tunnelNames[t.ID] = t.Name
			}
			rendered := make([]string, 0, len(bindings))
			for _, b := range bindings {
				epLabel := endpointNames[b.EndpointID]
				if epLabel == "" {
					epLabel = fmt.Sprintf("ep#%d", b.EndpointID)
				}
				if b.TunnelID == 0 {
					rendered = append(rendered, epLabel+"/*")
					continue
				}
				name := tunnelNames[b.TunnelID]
				if name == "" {
					name = fmt.Sprintf("t#%d", b.TunnelID)
				}
				rendered = append(rendered, epLabel+"/"+name)
			}
			row := map[string]any{
				"id":         p.ID,
				"name":       p.Name,
				"active":     p.Active,
				"bindings":   strings.Join(rendered, ", "),
				"created_at": p.CreatedAt.Format(time.RFC3339),
				"updated_at": p.UpdatedAt.Format(time.RFC3339),
			}
			cols := []output.Column{
				{Title: "ID", Key: "id"},
				{Title: "Name", Key: "name"},
				{Title: "Active", Key: "active"},
				{Title: "Bindings", Key: "bindings"},
				{Title: "Created", Key: "created_at"},
				{Title: "Updated", Key: "updated_at"},
			}
			return output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row)
		},
	}
}

// profileBindingFlags carries the comma-separated lists used by add /
// update. Endpoint-scoped (wildcard) bindings live in --bind-endpoint
// because they semantically differ from per-tunnel ones.
type profileBindingFlags struct {
	name     string
	bindEps  []string // each value: <endpoint id|name>
	bindTuns []string // each value: <tunnel id|[endpoint/]name>
}

func bindProfileFlags(c *cobra.Command, f *profileBindingFlags, requireName bool) {
	c.Flags().StringVar(&f.name, "name", "", "Profile name")
	c.Flags().StringSliceVar(&f.bindEps, "bind-endpoint", nil, "Wildcard binding: include all tunnels under this endpoint (repeatable)")
	c.Flags().StringSliceVar(&f.bindTuns, "bind-tunnel", nil, "Per-tunnel binding (repeatable)")
	if requireName {
		_ = c.MarkFlagRequired("name")
	}
}

// resolveBindings turns the user-supplied refs into a fully-resolved
// []model.ProfileBinding the store layer accepts. Empty input is a
// valid configuration ("create empty profile, add bindings later").
func resolveBindings(st *store.Store, eps, tuns []string) ([]model.ProfileBinding, error) {
	out := make([]model.ProfileBinding, 0, len(eps)+len(tuns))
	for _, ref := range eps {
		ep, err := resolveEndpoint(st, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, model.ProfileBinding{EndpointID: ep.ID, TunnelID: 0})
	}
	for _, ref := range tuns {
		t, err := resolveTunnel(st, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, model.ProfileBinding{EndpointID: t.EndpointID, TunnelID: t.ID})
	}
	return out, nil
}

func newProfileAddCmd(opts *GlobalOptions) *cobra.Command {
	f := &profileBindingFlags{}
	c := &cobra.Command{
		Use:   "add",
		Short: "Create a new profile (optionally with seed bindings)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			bindings, err := resolveBindings(st, f.bindEps, f.bindTuns)
			if err != nil {
				return err
			}
			now := time.Now()
			p := &model.Profile{
				Name:      strings.TrimSpace(f.name),
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := st.CreateProfile(p, bindings); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile: created %s (id=%d, %d binding(s))\n", p.Name, p.ID, len(bindings))
			return nil
		},
	}
	bindProfileFlags(c, f, true)
	return c
}

func newProfileUpdateCmd(opts *GlobalOptions) *cobra.Command {
	f := &profileBindingFlags{}
	c := &cobra.Command{
		Use:   "update <id|name>",
		Short: "Rename a profile and/or replace its bindings",
		Long: `Replaces (not merges) bindings: omitting --bind-endpoint /
--bind-tunnel clears the binding set. Pass them again with the desired
final state to keep / change them.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			p, err := resolveProfile(st, args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				p.Name = strings.TrimSpace(f.name)
			}
			p.UpdatedAt = time.Now()
			bindings, err := resolveBindings(st, f.bindEps, f.bindTuns)
			if err != nil {
				return err
			}
			if err := st.UpdateProfile(p, bindings); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile: updated %s (id=%d, %d binding(s))\n", p.Name, p.ID, len(bindings))
			return nil
		},
	}
	bindProfileFlags(c, f, false)
	return c
}

func newProfileRemoveCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id|name>",
		Short: "Delete a profile (refuses if currently active)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			p, err := resolveProfile(st, args[0])
			if err != nil {
				return err
			}
			if !opts.Yes {
				if err := confirm(cmd.OutOrStdout(), fmt.Sprintf("Remove profile %s (id=%d)? [y/N]: ", p.Name, p.ID)); err != nil {
					return err
				}
			}
			if err := st.DeleteProfile(p.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile: removed %s (id=%d)\n", p.Name, p.ID)
			return nil
		},
	}
}

func newProfileActivateCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "activate <id|name>",
		Short: "Activate a profile (flips endpoint/tunnel enabled flags + reconcile)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			p, err := resolveProfile(st, args[0])
			if err != nil {
				return err
			}
			activated, err := st.ActivateProfile(p.ID)
			if err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile: activated %s (id=%d) — daemon will reconcile\n", activated.Name, activated.ID)
			return nil
		},
	}
}

func newProfileDeactivateCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "deactivate",
		Short: "Mark every profile as inactive (does NOT change tunnel state)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			if err := st.DeactivateAllProfiles(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile: all profiles deactivated; existing endpoint/tunnel enabled flags preserved\n")
			return nil
		},
	}
}

