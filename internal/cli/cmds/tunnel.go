package cmds

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

// NewTunnelCmd builds the `frpdeck tunnel` command tree.
//
// Lifecycle subcommands (start / stop / extend) intentionally only
// touch the SQLite row + ping the daemon to reconcile. The actual
// frpc proxy is brought up / torn down by lifecycle.Manager — same
// path the Web UI uses, so Web-driven and CLI-driven changes converge
// on the same audit trail and event stream.
func NewTunnelCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "tunnel",
		Short: "Manage tunnels (proxies / visitors)",
	}
	c.AddCommand(
		newTunnelListCmd(opts),
		newTunnelGetCmd(opts),
		newTunnelAddCmd(opts),
		newTunnelUpdateCmd(opts),
		newTunnelLifecycleCmd(opts, "start"),
		newTunnelLifecycleCmd(opts, "stop"),
		newTunnelExtendCmd(opts),
		newTunnelRemoveCmd(opts),
	)
	return c
}

func newTunnelListCmd(opts *GlobalOptions) *cobra.Command {
	var endpointRef string
	c := &cobra.Command{
		Use:   "list",
		Short: "List tunnels (optionally filtered by endpoint)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			var tunnels []model.Tunnel
			if endpointRef != "" {
				ep, err := resolveEndpoint(st, endpointRef)
				if err != nil {
					return err
				}
				tunnels, err = st.ListTunnelsByEndpoint(ep.ID)
				if err != nil {
					return err
				}
			} else {
				tunnels, err = st.ListTunnels()
				if err != nil {
					return err
				}
			}
			endpointNames, err := buildEndpointNameMap(st)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, len(tunnels))
			for i, t := range tunnels {
				rows[i] = tunnelRow(&t, endpointNames)
			}
			cols := []output.Column{
				{Title: "ID", Key: "id"},
				{Title: "ENDPOINT", Key: "endpoint"},
				{Title: "NAME", Key: "name"},
				{Title: "TYPE", Key: "type"},
				{Title: "LOCAL", Key: "local"},
				{Title: "REMOTE", Key: "remote"},
				{Title: "STATUS", Key: "status"},
				{Title: "ENABLED", Key: "enabled"},
				{Title: "EXPIRE", Key: "expire_at"},
			}
			return output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...)
		},
	}
	c.Flags().StringVar(&endpointRef, "endpoint", "", "Filter to tunnels under this endpoint (id or name)")
	return c
}

func newTunnelGetCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id|[<endpoint>/]<name>>",
		Short: "Show a single tunnel in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			t, err := resolveTunnel(st, args[0])
			if err != nil {
				return err
			}
			endpointNames, err := buildEndpointNameMap(st)
			if err != nil {
				return err
			}
			row := tunnelDetail(t, endpointNames)
			return output.RenderSingle(cmd.OutOrStdout(), opts.Format, tunnelDetailCols(), row)
		},
	}
}

// tunnelFlags holds the writable knobs shared by add/update.
// Pointer fields are unnecessary because cobra exposes
// flag.Changed(name) — we use that to distinguish "user passed"
// from "default".
type tunnelFlags struct {
	endpoint        string
	name            string
	ttype           string
	role            string
	localIP         string
	localPort       int
	remotePort      int
	customDomains   string
	subdomain       string
	locations       string
	httpUser        string
	httpPassword    string
	hostHeaderRew   string
	sk              string
	allowUsers      string
	serverName      string
	serverUser      string
	encryption      bool
	compression     bool
	bandwidthLimit  string
	group           string
	groupKey        string
	healthType      string
	healthURL       string
	plugin          string
	pluginConfig    string
	enabled         bool
	autoStart       bool
	expireDuration  time.Duration
	expireUntilStr  string
}

func bindTunnelFlags(c *cobra.Command, f *tunnelFlags, requireEndpoint bool) {
	c.Flags().StringVar(&f.endpoint, "endpoint", "", "Owning endpoint (id or name)")
	c.Flags().StringVar(&f.name, "name", "", "Tunnel name")
	c.Flags().StringVar(&f.ttype, "type", "tcp", "Proxy type: tcp | udp | http | https | stcp | sudp | xtcp | tcpmux")
	c.Flags().StringVar(&f.role, "role", "", "stcp/sudp/xtcp role: server | visitor (omit for plain proxies)")
	c.Flags().StringVar(&f.localIP, "local-ip", "127.0.0.1", "Local IP to forward")
	c.Flags().IntVar(&f.localPort, "local-port", 0, "Local port to forward")
	c.Flags().IntVar(&f.remotePort, "remote-port", 0, "Remote port on frps (tcp/udp)")
	c.Flags().StringVar(&f.customDomains, "custom-domains", "", "Comma-separated http/https custom domains")
	c.Flags().StringVar(&f.subdomain, "subdomain", "", "http/https subdomain")
	c.Flags().StringVar(&f.locations, "locations", "", "http locations (comma-separated paths)")
	c.Flags().StringVar(&f.httpUser, "http-user", "", "Basic auth user for http/https")
	c.Flags().StringVar(&f.httpPassword, "http-password", "", "Basic auth password for http/https")
	c.Flags().StringVar(&f.hostHeaderRew, "host-header-rewrite", "", "Override Host header on proxied requests")
	c.Flags().StringVar(&f.sk, "sk", "", "Shared secret for stcp / sudp / xtcp")
	c.Flags().StringVar(&f.allowUsers, "allow-users", "", "Comma-separated visitor user allow-list (server role)")
	c.Flags().StringVar(&f.serverName, "server-name", "", "Visitor: server-side tunnel name")
	c.Flags().StringVar(&f.serverUser, "server-user", "", "Visitor: server-side tunnel owner user")
	c.Flags().BoolVar(&f.encryption, "encryption", false, "Enable encryption")
	c.Flags().BoolVar(&f.compression, "compression", false, "Enable compression")
	c.Flags().StringVar(&f.bandwidthLimit, "bandwidth-limit", "", "Bandwidth limit per frp client (e.g. 10MB)")
	c.Flags().StringVar(&f.group, "group", "", "Load-balancing group")
	c.Flags().StringVar(&f.groupKey, "group-key", "", "Load-balancing group key")
	c.Flags().StringVar(&f.healthType, "health-check-type", "", "Health check type: tcp | http")
	c.Flags().StringVar(&f.healthURL, "health-check-url", "", "Health check URL")
	c.Flags().StringVar(&f.plugin, "plugin", "", "frpc plugin name")
	c.Flags().StringVar(&f.pluginConfig, "plugin-config", "", "Plugin parameters (raw frpc-style)")
	c.Flags().BoolVar(&f.enabled, "enabled", true, "Enable tunnel")
	c.Flags().BoolVar(&f.autoStart, "auto-start", true, "Start at boot / endpoint enable")
	c.Flags().DurationVar(&f.expireDuration, "duration", 0, "Temporary tunnel: lifetime from now (e.g. 90m)")
	c.Flags().StringVar(&f.expireUntilStr, "until", "", "Temporary tunnel: absolute expiry (RFC3339, e.g. 2026-04-30T20:00:00+08:00)")
	if requireEndpoint {
		_ = c.MarkFlagRequired("endpoint")
		_ = c.MarkFlagRequired("name")
	}
}

func newTunnelAddCmd(opts *GlobalOptions) *cobra.Command {
	f := &tunnelFlags{}
	c := &cobra.Command{
		Use:   "add",
		Short: "Create a new tunnel",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			ep, err := resolveEndpoint(st, f.endpoint)
			if err != nil {
				return err
			}
			expireAt, err := resolveExpireAt(f.expireDuration, f.expireUntilStr)
			if err != nil {
				return err
			}
			now := time.Now()
			t := &model.Tunnel{
				EndpointID:        ep.ID,
				Name:              strings.TrimSpace(f.name),
				Type:              strings.TrimSpace(f.ttype),
				Role:              strings.TrimSpace(f.role),
				LocalIP:           strings.TrimSpace(f.localIP),
				LocalPort:         f.localPort,
				RemotePort:        f.remotePort,
				CustomDomains:     strings.TrimSpace(f.customDomains),
				Subdomain:         strings.TrimSpace(f.subdomain),
				Locations:         strings.TrimSpace(f.locations),
				HTTPUser:          strings.TrimSpace(f.httpUser),
				HTTPPassword:      f.httpPassword,
				HostHeaderRewrite: strings.TrimSpace(f.hostHeaderRew),
				SK:                f.sk,
				AllowUsers:        strings.TrimSpace(f.allowUsers),
				ServerName:        strings.TrimSpace(f.serverName),
				ServerUser:        strings.TrimSpace(f.serverUser),
				Encryption:        f.encryption,
				Compression:       f.compression,
				BandwidthLimit:    strings.TrimSpace(f.bandwidthLimit),
				Group:             strings.TrimSpace(f.group),
				GroupKey:          strings.TrimSpace(f.groupKey),
				HealthCheckType:   strings.TrimSpace(f.healthType),
				HealthCheckURL:    strings.TrimSpace(f.healthURL),
				Plugin:            strings.TrimSpace(f.plugin),
				PluginConfig:      f.pluginConfig,
				Enabled:           f.enabled,
				AutoStart:         f.autoStart,
				Status:            model.StatusPending,
				ExpireAt:          expireAt,
				Source:            model.TunnelSourceManual,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if err := st.CreateTunnel(t); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tunnel: created %s/%s (id=%d)\n", ep.Name, t.Name, t.ID)
			return nil
		},
	}
	bindTunnelFlags(c, f, true)
	return c
}

func newTunnelUpdateCmd(opts *GlobalOptions) *cobra.Command {
	f := &tunnelFlags{}
	c := &cobra.Command{
		Use:   "update <id|[<endpoint>/]<name>>",
		Short: "Update specific fields of a tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			t, err := resolveTunnel(st, args[0])
			if err != nil {
				return err
			}
			changed := cmd.Flags().Changed
			if changed("endpoint") {
				ep, err := resolveEndpoint(st, f.endpoint)
				if err != nil {
					return err
				}
				t.EndpointID = ep.ID
			}
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
			t.UpdatedAt = time.Now()
			if err := st.UpdateTunnel(t); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tunnel: updated %s (id=%d)\n", t.Name, t.ID)
			return nil
		},
	}
	bindTunnelFlags(c, f, false)
	return c
}

// newTunnelLifecycleCmd produces the start / stop subcommands. They
// only differ in the target Status value, so collapsing the factory
// keeps the surface honest.
func newTunnelLifecycleCmd(opts *GlobalOptions, action string) *cobra.Command {
	desc := "Mark a tunnel as active and ask the daemon to bring it up"
	target := model.StatusActive
	if action == "stop" {
		desc = "Mark a tunnel as stopped and ask the daemon to tear it down"
		target = model.StatusStopped
	}
	return &cobra.Command{
		Use:   action + " <id|[<endpoint>/]<name>>",
		Short: desc,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			t, err := resolveTunnel(st, args[0])
			if err != nil {
				return err
			}
			t.Status = target
			t.UpdatedAt = time.Now()
			if err := st.UpdateTunnel(t); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tunnel: %s requested for %s (id=%d) — daemon will reconcile\n", action, t.Name, t.ID)
			return nil
		},
	}
}

func newTunnelExtendCmd(opts *GlobalOptions) *cobra.Command {
	var dur time.Duration
	var until string
	c := &cobra.Command{
		Use:   "extend <id|[<endpoint>/]<name>>",
		Short: "Extend (or set) the auto-stop deadline of a temporary tunnel",
		Long: `Set or extend a tunnel's expiry timestamp. Two forms:

  --duration <go duration>     extend by this much from the existing
                               ExpireAt (or from now if not set)
  --until <RFC3339 timestamp>  set ExpireAt to an absolute time

Use --duration 0 (or omit both flags) to clear the expiry, turning a
temporary tunnel into a permanent one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dur != 0 && until != "" {
				return errors.New("--duration and --until are mutually exclusive")
			}
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			t, err := resolveTunnel(st, args[0])
			if err != nil {
				return err
			}
			switch {
			case until != "":
				ts, err := time.Parse(time.RFC3339, until)
				if err != nil {
					return fmt.Errorf("--until: %w", err)
				}
				t.ExpireAt = &ts
			case dur > 0:
				base := time.Now()
				if t.ExpireAt != nil && t.ExpireAt.After(base) {
					base = *t.ExpireAt
				}
				ts := base.Add(dur)
				t.ExpireAt = &ts
			default:
				t.ExpireAt = nil
			}
			t.UpdatedAt = time.Now()
			if err := st.UpdateTunnel(t); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			if t.ExpireAt == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "tunnel: cleared expiry on %s (id=%d) — now permanent\n", t.Name, t.ID)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "tunnel: %s (id=%d) expires at %s\n", t.Name, t.ID, t.ExpireAt.Format(time.RFC3339))
			}
			return nil
		},
	}
	c.Flags().DurationVar(&dur, "duration", 0, "Extend by this go-duration (e.g. 1h30m)")
	c.Flags().StringVar(&until, "until", "", "Set absolute expiry (RFC3339)")
	return c
}

func newTunnelRemoveCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id|[<endpoint>/]<name>>",
		Short: "Delete a tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			t, err := resolveTunnel(st, args[0])
			if err != nil {
				return err
			}
			if !opts.Yes {
				if err := confirm(cmd.OutOrStdout(), fmt.Sprintf("Remove tunnel %s (id=%d)? [y/N]: ", t.Name, t.ID)); err != nil {
					return err
				}
			}
			if err := st.DeleteTunnel(t.ID); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tunnel: removed %s (id=%d)\n", t.Name, t.ID)
			return nil
		},
	}
}

// resolveExpireAt unifies --duration / --until into a single
// optional pointer. Empty/zero on both => no expiry.
func resolveExpireAt(dur time.Duration, until string) (*time.Time, error) {
	if dur != 0 && until != "" {
		return nil, errors.New("--duration and --until are mutually exclusive")
	}
	if until != "" {
		ts, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return nil, fmt.Errorf("--until: %w", err)
		}
		return &ts, nil
	}
	if dur > 0 {
		ts := time.Now().Add(dur)
		return &ts, nil
	}
	return nil, nil
}

// buildEndpointNameMap pre-loads endpoint id -> name pairs so the
// list / detail renderers can show "nas" instead of "1" without
// querying once per tunnel.
func buildEndpointNameMap(st *store.Store) (map[uint]string, error) {
	eps, err := st.ListEndpoints()
	if err != nil {
		return nil, err
	}
	out := make(map[uint]string, len(eps))
	for _, e := range eps {
		out[e.ID] = e.Name
	}
	return out, nil
}

func tunnelRow(t *model.Tunnel, endpointNames map[uint]string) map[string]any {
	expire := ""
	if t.ExpireAt != nil {
		expire = t.ExpireAt.Format(time.RFC3339)
	}
	return map[string]any{
		"id":          t.ID,
		"endpoint":    endpointNames[t.EndpointID],
		"endpoint_id": t.EndpointID,
		"name":        t.Name,
		"type":        t.Type,
		"local":       fmt.Sprintf("%s:%d", t.LocalIP, t.LocalPort),
		"remote":      tunnelRemoteString(t),
		"status":      t.Status,
		"enabled":     t.Enabled,
		"expire_at":   expire,
	}
}

// tunnelRemoteString picks the most informative "remote" cell for the
// list view. tcp/udp use remote_port; http/https prefer custom_domains
// or subdomain; stcp/sudp/xtcp surface the role + sk fingerprint
// because their "remote" is logical, not an open port.
func tunnelRemoteString(t *model.Tunnel) string {
	switch t.Type {
	case "tcp", "udp", "tcpmux":
		if t.RemotePort > 0 {
			return fmt.Sprintf(":%d", t.RemotePort)
		}
	case "http", "https":
		if t.CustomDomains != "" {
			return t.CustomDomains
		}
		if t.Subdomain != "" {
			return t.Subdomain + ".*"
		}
	case "stcp", "sudp", "xtcp":
		role := t.Role
		if role == "" {
			role = "server"
		}
		return role
	}
	return ""
}

func tunnelDetail(t *model.Tunnel, endpointNames map[uint]string) map[string]any {
	row := tunnelRow(t, endpointNames)
	row["role"] = t.Role
	row["http_user"] = t.HTTPUser
	row["host_header_rewrite"] = t.HostHeaderRewrite
	row["allow_users"] = t.AllowUsers
	row["server_name"] = t.ServerName
	row["server_user"] = t.ServerUser
	row["encryption"] = t.Encryption
	row["compression"] = t.Compression
	row["bandwidth_limit"] = t.BandwidthLimit
	row["group"] = t.Group
	row["group_key"] = t.GroupKey
	row["health_check_type"] = t.HealthCheckType
	row["health_check_url"] = t.HealthCheckURL
	row["plugin"] = t.Plugin
	row["plugin_config"] = t.PluginConfig
	row["auto_start"] = t.AutoStart
	row["source"] = t.Source
	row["template_id"] = t.TemplateID
	row["last_error"] = t.LastError
	row["created_at"] = t.CreatedAt.Format(time.RFC3339)
	row["updated_at"] = t.UpdatedAt.Format(time.RFC3339)
	return row
}

func tunnelDetailCols() []output.Column {
	return []output.Column{
		{Title: "ID", Key: "id"},
		{Title: "Endpoint", Key: "endpoint"},
		{Title: "Name", Key: "name"},
		{Title: "Type", Key: "type"},
		{Title: "Role", Key: "role"},
		{Title: "Local", Key: "local"},
		{Title: "Remote", Key: "remote"},
		{Title: "Status", Key: "status"},
		{Title: "Enabled", Key: "enabled"},
		{Title: "Auto Start", Key: "auto_start"},
		{Title: "Expire", Key: "expire_at"},
		{Title: "HTTP User", Key: "http_user"},
		{Title: "Host Header", Key: "host_header_rewrite"},
		{Title: "Allow Users", Key: "allow_users"},
		{Title: "Server Name", Key: "server_name"},
		{Title: "Server User", Key: "server_user"},
		{Title: "Encryption", Key: "encryption"},
		{Title: "Compression", Key: "compression"},
		{Title: "Bandwidth", Key: "bandwidth_limit"},
		{Title: "Group", Key: "group"},
		{Title: "Group Key", Key: "group_key"},
		{Title: "Health Type", Key: "health_check_type"},
		{Title: "Health URL", Key: "health_check_url"},
		{Title: "Plugin", Key: "plugin"},
		{Title: "Plugin Config", Key: "plugin_config"},
		{Title: "Source", Key: "source"},
		{Title: "Template ID", Key: "template_id"},
		{Title: "Last Error", Key: "last_error"},
		{Title: "Created", Key: "created_at"},
		{Title: "Updated", Key: "updated_at"},
	}
}
