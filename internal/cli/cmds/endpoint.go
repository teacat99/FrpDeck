package cmds

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/model"
)

// NewEndpointCmd builds the `frpdeck endpoint` command tree.
//
// All mutating subcommands trigger a best-effort socket Reconcile()
// after committing the change so a running daemon picks up the new
// state immediately. When the daemon is not up the CLI completes
// silently — Direct-DB writes are durable on their own.
func NewEndpointCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "endpoint",
		Short: "Manage frps endpoints",
	}
	c.AddCommand(
		newEndpointListCmd(opts),
		newEndpointGetCmd(opts),
		newEndpointAddCmd(opts),
		newEndpointUpdateCmd(opts),
		newEndpointEnableCmd(opts, true),
		newEndpointEnableCmd(opts, false),
		newEndpointRemoveCmd(opts),
	)
	return c
}

func newEndpointListCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all endpoints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			eps, err := st.ListEndpoints()
			if err != nil {
				return err
			}
			rows := make([]map[string]any, len(eps))
			for i, e := range eps {
				rows[i] = endpointRow(&e)
			}
			cols := []output.Column{
				{Title: "ID", Key: "id"},
				{Title: "NAME", Key: "name"},
				{Title: "ADDR", Key: "addr"},
				{Title: "DRIVER", Key: "driver_mode"},
				{Title: "ENABLED", Key: "enabled"},
				{Title: "AUTO", Key: "auto_start"},
				{Title: "GROUP", Key: "group"},
			}
			return output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...)
		},
	}
}

func newEndpointGetCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id|name>",
		Short: "Show one endpoint in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			ep, err := resolveEndpoint(st, args[0])
			if err != nil {
				return err
			}
			row := endpointDetail(ep)
			cols := endpointDetailCols()
			return output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row)
		},
	}
}

// endpointFlags is the shared flag definition used by `add` and
// `update` so the two stay in lockstep. Pointer fields let `update`
// distinguish "user did not pass" (nil) from "user passed empty
// string" (non-nil pointer to ""), which is exactly the partial-update
// semantic operators expect.
type endpointFlags struct {
	name              string
	group             string
	addr              string
	port              int
	protocol          string
	user              string
	token             string
	tlsEnable         bool
	driverMode        string
	subprocessPath    string
	subprocessVersion string
	enabled           bool
	autoStart         bool
	poolCount         int
	heartbeatInterval int
	heartbeatTimeout  int
}

func bindEndpointFlags(c *cobra.Command, f *endpointFlags) {
	c.Flags().StringVar(&f.name, "name", "", "Endpoint name (unique label)")
	c.Flags().StringVar(&f.group, "group", "", "Optional group label for UI bucketing")
	c.Flags().StringVar(&f.addr, "addr", "", "frps host (DNS or IP)")
	c.Flags().IntVar(&f.port, "port", 7000, "frps port")
	c.Flags().StringVar(&f.protocol, "protocol", "tcp", "frps connection protocol: tcp | kcp | quic | wss | websocket")
	c.Flags().StringVar(&f.user, "user", "", "frps user (frpc.user)")
	c.Flags().StringVar(&f.token, "token", "", "frps auth token")
	c.Flags().BoolVar(&f.tlsEnable, "tls", false, "Enable TLS towards frps")
	c.Flags().StringVar(&f.driverMode, "driver", model.DriverModeEmbedded, "frpc driver: embedded | subprocess")
	c.Flags().StringVar(&f.subprocessPath, "subprocess-path", "", "External frpc binary path (driver=subprocess)")
	c.Flags().StringVar(&f.subprocessVersion, "subprocess-version", "", "External frpc version (auto-probed if empty)")
	c.Flags().BoolVar(&f.enabled, "enabled", true, "Enable this endpoint")
	c.Flags().BoolVar(&f.autoStart, "auto-start", true, "Start the endpoint at boot")
	c.Flags().IntVar(&f.poolCount, "pool-count", 0, "frps connection pool size")
	c.Flags().IntVar(&f.heartbeatInterval, "heartbeat-interval", 0, "Heartbeat interval (seconds)")
	c.Flags().IntVar(&f.heartbeatTimeout, "heartbeat-timeout", 0, "Heartbeat timeout (seconds)")
}

func newEndpointAddCmd(opts *GlobalOptions) *cobra.Command {
	f := &endpointFlags{}
	c := &cobra.Command{
		Use:   "add",
		Short: "Create a new endpoint",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(f.name) == "" || strings.TrimSpace(f.addr) == "" {
				return errors.New("--name and --addr are required")
			}
			if f.driverMode != model.DriverModeEmbedded && f.driverMode != model.DriverModeSubprocess {
				return fmt.Errorf("invalid --driver %q (expected embedded | subprocess)", f.driverMode)
			}
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			now := time.Now()
			ep := &model.Endpoint{
				Name:              strings.TrimSpace(f.name),
				Group:             strings.TrimSpace(f.group),
				Addr:              strings.TrimSpace(f.addr),
				Port:              f.port,
				Protocol:          strings.TrimSpace(f.protocol),
				User:              strings.TrimSpace(f.user),
				Token:             f.token,
				TLSEnable:         f.tlsEnable,
				DriverMode:        f.driverMode,
				SubprocessPath:    strings.TrimSpace(f.subprocessPath),
				SubprocessVersion: strings.TrimSpace(f.subprocessVersion),
				Enabled:           f.enabled,
				AutoStart:         f.autoStart,
				PoolCount:         f.poolCount,
				HeartbeatInterval: f.heartbeatInterval,
				HeartbeatTimeout:  f.heartbeatTimeout,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if err := st.CreateEndpoint(ep); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "endpoint: created %s (id=%d)\n", ep.Name, ep.ID)
			return nil
		},
	}
	bindEndpointFlags(c, f)
	return c
}

func newEndpointUpdateCmd(opts *GlobalOptions) *cobra.Command {
	f := &endpointFlags{}
	c := &cobra.Command{
		Use:   "update <id|name>",
		Short: "Update specific fields of an endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			ep, err := resolveEndpoint(st, args[0])
			if err != nil {
				return err
			}
			changed := cmd.Flags().Changed
			if changed("name") {
				ep.Name = strings.TrimSpace(f.name)
			}
			if changed("group") {
				ep.Group = strings.TrimSpace(f.group)
			}
			if changed("addr") {
				ep.Addr = strings.TrimSpace(f.addr)
			}
			if changed("port") {
				ep.Port = f.port
			}
			if changed("protocol") {
				ep.Protocol = strings.TrimSpace(f.protocol)
			}
			if changed("user") {
				ep.User = strings.TrimSpace(f.user)
			}
			if changed("token") {
				ep.Token = f.token
			}
			if changed("tls") {
				ep.TLSEnable = f.tlsEnable
			}
			if changed("driver") {
				if f.driverMode != model.DriverModeEmbedded && f.driverMode != model.DriverModeSubprocess {
					return fmt.Errorf("invalid --driver %q", f.driverMode)
				}
				ep.DriverMode = f.driverMode
			}
			if changed("subprocess-path") {
				ep.SubprocessPath = strings.TrimSpace(f.subprocessPath)
			}
			if changed("subprocess-version") {
				ep.SubprocessVersion = strings.TrimSpace(f.subprocessVersion)
			}
			if changed("enabled") {
				ep.Enabled = f.enabled
			}
			if changed("auto-start") {
				ep.AutoStart = f.autoStart
			}
			if changed("pool-count") {
				ep.PoolCount = f.poolCount
			}
			if changed("heartbeat-interval") {
				ep.HeartbeatInterval = f.heartbeatInterval
			}
			if changed("heartbeat-timeout") {
				ep.HeartbeatTimeout = f.heartbeatTimeout
			}
			ep.UpdatedAt = time.Now()
			if err := st.UpdateEndpoint(ep); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "endpoint: updated %s (id=%d)\n", ep.Name, ep.ID)
			return nil
		},
	}
	bindEndpointFlags(c, f)
	return c
}

// newEndpointEnableCmd returns either the `enable` or `disable`
// subcommand depending on the boolean flag — they only differ in the
// final value written, so collapsing the factory keeps the surface
// area honest.
func newEndpointEnableCmd(opts *GlobalOptions, enable bool) *cobra.Command {
	verb := "disable"
	if enable {
		verb = "enable"
	}
	return &cobra.Command{
		Use:   verb + " <id|name>",
		Short: capitalise(verb) + " an endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			ep, err := resolveEndpoint(st, args[0])
			if err != nil {
				return err
			}
			if ep.Enabled == enable {
				fmt.Fprintf(cmd.OutOrStdout(), "endpoint: %s already %sd (id=%d)\n", ep.Name, verb, ep.ID)
				return nil
			}
			ep.Enabled = enable
			ep.UpdatedAt = time.Now()
			if err := st.UpdateEndpoint(ep); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "endpoint: %sd %s (id=%d)\n", verb, ep.Name, ep.ID)
			return nil
		},
	}
}

func newEndpointRemoveCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id|name>",
		Short: "Delete an endpoint and every tunnel under it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			ep, err := resolveEndpoint(st, args[0])
			if err != nil {
				return err
			}
			tunnels, err := st.ListTunnelsByEndpoint(ep.ID)
			if err != nil {
				return err
			}
			if !opts.Yes {
				if err := confirm(cmd.OutOrStdout(), fmt.Sprintf("Remove endpoint %s (id=%d) and %d tunnel(s)? [y/N]: ", ep.Name, ep.ID, len(tunnels))); err != nil {
					return err
				}
			}
			if err := st.DeleteEndpoint(ep.ID); err != nil {
				return err
			}
			if err := pingReconcile(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "endpoint: removed %s (id=%d) + %d tunnel(s)\n", ep.Name, ep.ID, len(tunnels))
			return nil
		},
	}
}

// endpointRow is the canonical projection used by `list`. Keep the
// keys in sync with endpointDetailCols() below; tests only verify
// keys that appear in both.
func endpointRow(e *model.Endpoint) map[string]any {
	return map[string]any{
		"id":          e.ID,
		"name":        e.Name,
		"addr":        fmt.Sprintf("%s:%d", e.Addr, e.Port),
		"driver_mode": e.DriverMode,
		"enabled":     e.Enabled,
		"auto_start":  e.AutoStart,
		"group":       e.Group,
	}
}

func endpointDetail(e *model.Endpoint) map[string]any {
	row := endpointRow(e)
	row["protocol"] = e.Protocol
	row["user"] = e.User
	row["tls_enable"] = e.TLSEnable
	row["pool_count"] = e.PoolCount
	row["heartbeat_interval"] = e.HeartbeatInterval
	row["heartbeat_timeout"] = e.HeartbeatTimeout
	row["subprocess_path"] = e.SubprocessPath
	row["subprocess_version"] = e.SubprocessVersion
	row["created_at"] = e.CreatedAt.Format(time.RFC3339)
	row["updated_at"] = e.UpdatedAt.Format(time.RFC3339)
	return row
}

func endpointDetailCols() []output.Column {
	return []output.Column{
		{Title: "ID", Key: "id"},
		{Title: "Name", Key: "name"},
		{Title: "Group", Key: "group"},
		{Title: "Address", Key: "addr"},
		{Title: "Protocol", Key: "protocol"},
		{Title: "User", Key: "user"},
		{Title: "TLS", Key: "tls_enable"},
		{Title: "Driver", Key: "driver_mode"},
		{Title: "Subprocess Path", Key: "subprocess_path"},
		{Title: "Subprocess Ver", Key: "subprocess_version"},
		{Title: "Pool Count", Key: "pool_count"},
		{Title: "Heartbeat Interval", Key: "heartbeat_interval"},
		{Title: "Heartbeat Timeout", Key: "heartbeat_timeout"},
		{Title: "Enabled", Key: "enabled"},
		{Title: "Auto Start", Key: "auto_start"},
		{Title: "Created", Key: "created_at"},
		{Title: "Updated", Key: "updated_at"},
	}
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
