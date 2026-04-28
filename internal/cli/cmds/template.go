package cmds

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/templates"
)

// NewTemplateCmd builds the `frpdeck template` command tree.
//
// Templates are the same compile-time YAML the Web UI wizard reads:
// applying one creates a tunnel pre-filled with the template's
// `defaults` map, then layered with whatever per-invocation flags
// the operator passed. The CLI flags here intentionally mirror
// `frpdeck tunnel add` so swapping `tunnel add` -> `template apply`
// is a one-token edit.
func NewTemplateCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "template",
		Short: "Scenario templates (ssh / web / db-share / etc.)",
	}
	c.AddCommand(newTemplateListCmd(opts), newTemplateApplyCmd(opts))
	return c
}

func newTemplateListCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the embedded scenario templates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			all, err := templates.All()
			if err != nil {
				return err
			}
			rows := make([]map[string]any, len(all))
			for i, t := range all {
				rows[i] = map[string]any{
					"id":     t.ID,
					"icon":   t.Icon,
					"name":   t.NameKey,
					"desc":   t.DescKey,
					"prereq": strings.Join(t.PrereqKeys, ", "),
				}
			}
			cols := []output.Column{
				{Title: "ID", Key: "id"},
				{Title: "ICON", Key: "icon"},
				{Title: "NAME KEY", Key: "name"},
				{Title: "DESC KEY", Key: "desc"},
				{Title: "PREREQ", Key: "prereq"},
			}
			return output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...)
		},
	}
}

func newTemplateApplyCmd(opts *GlobalOptions) *cobra.Command {
	f := &tunnelFlags{}
	c := &cobra.Command{
		Use:   "apply <template-id>",
		Short: "Create a new tunnel pre-filled from a scenario template",
		Long: `The chosen template's defaults seed every field; --name,
--endpoint, --local-port, --remote-port etc. then override per-invocation.
Example:

    frpdeck template apply ssh --endpoint nas --name homelab-ssh \
        --local-port 22 --remote-port 12022`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tpl, err := templates.FindByID(args[0])
			if err != nil {
				return err
			}
			if tpl == nil {
				return fmt.Errorf("template %q not found (use `frpdeck template list`)", args[0])
			}
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			ep, err := resolveEndpoint(st, f.endpoint)
			if err != nil {
				return err
			}
			t, err := tunnelFromTemplate(tpl)
			if err != nil {
				return err
			}
			t.EndpointID = ep.ID
			t.TemplateID = tpl.ID
			t.Source = model.TunnelSourceTemplate
			applyTunnelFlagsOver(t, f, cmd.Flags().Changed)
			now := time.Now()
			t.CreatedAt = now
			t.UpdatedAt = now
			if t.Status == "" {
				t.Status = model.StatusPending
			}
			if err := st.CreateTunnel(t); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "template: %s applied → tunnel %s/%s (id=%d)\n", tpl.ID, ep.Name, t.Name, t.ID)
			return nil
		},
	}
	bindTunnelFlags(c, f, false)
	_ = c.MarkFlagRequired("endpoint")
	_ = c.MarkFlagRequired("name")
	return c
}

// tunnelFromTemplate seeds a model.Tunnel from a Template.Defaults
// map. Only the fields we actually project to UI / store are read;
// anything else in defaults (or anything new added later) is silently
// ignored — matching the Web UI's behaviour where extra defaults are
// passed through to the create-tunnel form and dropped if the form
// has no field for them.
func tunnelFromTemplate(t *templates.Template) (*model.Tunnel, error) {
	if t == nil {
		return nil, errors.New("nil template")
	}
	tu := &model.Tunnel{}
	d := t.Defaults
	if v, ok := d["type"].(string); ok {
		tu.Type = v
	}
	if v, ok := d["role"].(string); ok {
		tu.Role = v
	}
	if v, ok := d["local_ip"].(string); ok {
		tu.LocalIP = v
	}
	if v, ok := intFromAny(d["local_port"]); ok {
		tu.LocalPort = v
	}
	if v, ok := intFromAny(d["remote_port"]); ok {
		tu.RemotePort = v
	}
	if v, ok := d["custom_domains"].(string); ok {
		tu.CustomDomains = v
	}
	if v, ok := d["subdomain"].(string); ok {
		tu.Subdomain = v
	}
	if v, ok := d["locations"].(string); ok {
		tu.Locations = v
	}
	if v, ok := d["http_user"].(string); ok {
		tu.HTTPUser = v
	}
	if v, ok := d["http_password"].(string); ok {
		tu.HTTPPassword = v
	}
	if v, ok := d["host_header_rewrite"].(string); ok {
		tu.HostHeaderRewrite = v
	}
	if v, ok := d["sk"].(string); ok {
		tu.SK = v
	}
	if v, ok := d["allow_users"].(string); ok {
		tu.AllowUsers = v
	}
	if v, ok := d["server_name"].(string); ok {
		tu.ServerName = v
	}
	if v, ok := d["encryption"].(bool); ok {
		tu.Encryption = v
	}
	if v, ok := d["compression"].(bool); ok {
		tu.Compression = v
	}
	if v, ok := d["bandwidth_limit"].(string); ok {
		tu.BandwidthLimit = v
	}
	if v, ok := d["group"].(string); ok {
		tu.Group = v
	}
	if v, ok := d["group_key"].(string); ok {
		tu.GroupKey = v
	}
	if v, ok := d["plugin"].(string); ok {
		tu.Plugin = v
	}
	if v, ok := d["plugin_config"].(string); ok {
		tu.PluginConfig = v
	}
	if v, ok := d["enabled"].(bool); ok {
		tu.Enabled = v
	} else {
		tu.Enabled = true
	}
	if v, ok := d["auto_start"].(bool); ok {
		tu.AutoStart = v
	} else {
		tu.AutoStart = true
	}
	return tu, nil
}

// intFromAny accepts both YAML's int decoder (returns int) and JSON's
// float64 fallback (in case some template ships numbers via a JSON
// pipeline later), so the apply command keeps working as the data
// pipeline evolves.
func intFromAny(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	}
	return 0, false
}

// applyTunnelFlagsOver mutates the seeded Tunnel with whichever
// per-invocation flags the operator actually passed. Reuses the
// `cmd.Flags().Changed("...")` predicate so the override semantics
// match `tunnel update`.
func applyTunnelFlagsOver(t *model.Tunnel, f *tunnelFlags, changed func(string) bool) {
	if changed("name") {
		t.Name = strings.TrimSpace(f.name)
	}
	if changed("type") {
		t.Type = strings.TrimSpace(f.ttype)
	}
	if changed("role") {
		t.Role = strings.TrimSpace(f.role)
	}
	if changed("local-ip") {
		t.LocalIP = strings.TrimSpace(f.localIP)
	}
	if changed("local-port") {
		t.LocalPort = f.localPort
	}
	if changed("remote-port") {
		t.RemotePort = f.remotePort
	}
	if changed("custom-domains") {
		t.CustomDomains = strings.TrimSpace(f.customDomains)
	}
	if changed("subdomain") {
		t.Subdomain = strings.TrimSpace(f.subdomain)
	}
	if changed("locations") {
		t.Locations = strings.TrimSpace(f.locations)
	}
	if changed("http-user") {
		t.HTTPUser = strings.TrimSpace(f.httpUser)
	}
	if changed("http-password") {
		t.HTTPPassword = f.httpPassword
	}
	if changed("host-header-rewrite") {
		t.HostHeaderRewrite = strings.TrimSpace(f.hostHeaderRew)
	}
	if changed("sk") {
		t.SK = f.sk
	}
	if changed("allow-users") {
		t.AllowUsers = strings.TrimSpace(f.allowUsers)
	}
	if changed("server-name") {
		t.ServerName = strings.TrimSpace(f.serverName)
	}
	if changed("server-user") {
		t.ServerUser = strings.TrimSpace(f.serverUser)
	}
	if changed("encryption") {
		t.Encryption = f.encryption
	}
	if changed("compression") {
		t.Compression = f.compression
	}
	if changed("bandwidth-limit") {
		t.BandwidthLimit = strings.TrimSpace(f.bandwidthLimit)
	}
	if changed("group") {
		t.Group = strings.TrimSpace(f.group)
	}
	if changed("group-key") {
		t.GroupKey = strings.TrimSpace(f.groupKey)
	}
	if changed("health-check-type") {
		t.HealthCheckType = strings.TrimSpace(f.healthType)
	}
	if changed("health-check-url") {
		t.HealthCheckURL = strings.TrimSpace(f.healthURL)
	}
	if changed("plugin") {
		t.Plugin = strings.TrimSpace(f.plugin)
	}
	if changed("plugin-config") {
		t.PluginConfig = f.pluginConfig
	}
	if changed("enabled") {
		t.Enabled = f.enabled
	}
	if changed("auto-start") {
		t.AutoStart = f.autoStart
	}
	if expire, err := resolveExpireAt(f.expireDuration, f.expireUntilStr); err == nil && expire != nil {
		t.ExpireAt = expire
	}
}
