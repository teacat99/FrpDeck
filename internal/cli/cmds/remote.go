package cmds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/control"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

// NewRemoteCmd builds the `frpdeck remote` command tree.
//
// Coverage in P10-D: read-only `nodes list` / `nodes get` keep their
// Direct-DB shortcut; the four mutating actions (invite / refresh /
// revoke / revoke-token) round-trip through the control socket so
// the daemon can mint mgmt_tokens, push the auto stcp tunnel, and
// write the audit row. The CLI fails fast with ErrDaemonNotRunning
// when the socket is unreachable — these operations cannot work
// Direct-DB because the JWT signing key only exists in the running
// daemon's memory.
func NewRemoteCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "remote",
		Short: "Manage remote-managed FrpDeck pairings",
	}
	nodes := &cobra.Command{
		Use:   "nodes",
		Short: "Operate on RemoteNode rows",
	}
	nodes.AddCommand(newRemoteNodesListCmd(opts), newRemoteNodesGetCmd(opts))
	c.AddCommand(
		nodes,
		newRemoteInviteCmd(opts),
		newRemoteRefreshCmd(opts),
		newRemoteRevokeMgmtTokenCmd(opts),
		newRemoteRevokeCmd(opts),
	)
	return c
}

func newRemoteNodesListCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every RemoteNode pairing in the local store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			nodes, err := st.ListRemoteNodes()
			if err != nil {
				return err
			}
			endpointNames, err := buildEndpointNameMap(st)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, len(nodes))
			for i, n := range nodes {
				rows[i] = remoteNodeRow(&n, endpointNames)
			}
			cols := []output.Column{
				{Title: "ID", Key: "id"},
				{Title: "NAME", Key: "name"},
				{Title: "DIRECTION", Key: "direction"},
				{Title: "ENDPOINT", Key: "endpoint"},
				{Title: "TUNNEL", Key: "tunnel_id"},
				{Title: "STATUS", Key: "status"},
				{Title: "EXPIRY", Key: "invite_expiry"},
				{Title: "LAST SEEN", Key: "last_seen"},
			}
			return output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...)
		},
	}
}

func newRemoteNodesGetCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id|name>",
		Short: "Show one RemoteNode in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			node, err := resolveRemoteNode(st, args[0])
			if err != nil {
				return err
			}
			endpointNames, err := buildEndpointNameMap(st)
			if err != nil {
				return err
			}
			row := remoteNodeRow(node, endpointNames)
			row["remote_user"] = node.RemoteUser
			row["local_bind_port"] = node.LocalBindPort
			row["created_at"] = node.CreatedAt.Format(time.RFC3339)
			row["updated_at"] = node.UpdatedAt.Format(time.RFC3339)
			cols := []output.Column{
				{Title: "ID", Key: "id"},
				{Title: "Name", Key: "name"},
				{Title: "Direction", Key: "direction"},
				{Title: "Endpoint", Key: "endpoint"},
				{Title: "Tunnel ID", Key: "tunnel_id"},
				{Title: "Status", Key: "status"},
				{Title: "Remote User", Key: "remote_user"},
				{Title: "Local Bind Port", Key: "local_bind_port"},
				{Title: "Invite Expiry", Key: "invite_expiry"},
				{Title: "Last Seen", Key: "last_seen"},
				{Title: "Created", Key: "created_at"},
				{Title: "Updated", Key: "updated_at"},
			}
			return output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row)
		},
	}
}

// invocationResult mirrors remoteops.InvitationResult on the wire.
// We re-declare it here rather than import internal/remoteops so the
// CLI binary stays free of the daemon-only dependency tree (auth,
// frpcd driver, etc.) — keeping the CLI's binary footprint small.
type invocationResult struct {
	Node          *model.RemoteNode `json:"node"`
	Invitation    string            `json:"invitation"`
	ExpireAt      time.Time         `json:"expire_at"`
	MgmtToken     string            `json:"mgmt_token"`
	TunnelID      uint              `json:"tunnel_id"`
	DriverWarning string            `json:"driver_warning,omitempty"`
}

// invocationActor mirrors remoteops.Actor for wire serialization.
// CLI calls leave UserID = 0 so the daemon falls back to the first
// active admin (audit row will be attributed to that user with
// "cli" as the actor name).
type invocationActor struct {
	UserID   uint   `json:"UserID,omitempty"`
	Username string `json:"Username,omitempty"`
	IP       string `json:"IP,omitempty"`
}

// invokeRemoteOps wraps the control-socket Invoke call with consistent
// error handling — every mutating remote command takes the same path.
func invokeRemoteOps(ctx context.Context, opts *GlobalOptions, method string, args any) (json.RawMessage, error) {
	if opts.SocketClient == nil {
		return nil, errExitCode{code: 1, msg: "control socket client not initialised"}
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := opts.SocketClient.Invoke(ctx, method, body)
	if err != nil {
		if errors.Is(err, control.ErrDaemonNotRunning) {
			return nil, errExitCode{code: 3, msg: "daemon is not running — start frpdeck-server before running mutating remote commands"}
		}
		return nil, err
	}
	return res, nil
}

func newRemoteInviteCmd(opts *GlobalOptions) *cobra.Command {
	var endpointRef, nodeName, uiScheme string
	c := &cobra.Command{
		Use:   "invite",
		Short: "Generate a remote-management invitation that lets a peer FrpDeck dial back",
		Long: "Mints a fresh invitation: creates a stcp server-role tunnel pointed at\n" +
			"this instance's web UI, persists a RemoteNode row, signs a 24h mgmt_token\n" +
			"and pushes the tunnel to the running driver. Output includes the encoded\n" +
			"invitation the peer pastes into `frpdeck-server`'s redeem flow.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(endpointRef) == "" {
				return UsageError{Msg: "--endpoint is required"}
			}
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			ep, err := resolveEndpoint(st, endpointRef)
			closer()
			if err != nil {
				return err
			}
			args := struct {
				EndpointID uint            `json:"EndpointID"`
				NodeName   string          `json:"NodeName,omitempty"`
				UIScheme   string          `json:"UIScheme,omitempty"`
				Actor      invocationActor `json:"Actor"`
			}{
				EndpointID: ep.ID,
				NodeName:   strings.TrimSpace(nodeName),
				UIScheme:   strings.TrimSpace(uiScheme),
				Actor:      invocationActor{Username: "cli"},
			}
			raw, err := invokeRemoteOps(cmd.Context(), opts, "remote.invite", args)
			if err != nil {
				return err
			}
			return renderInvitationResult(cmd, opts, raw)
		},
	}
	c.Flags().StringVarP(&endpointRef, "endpoint", "e", "", "Endpoint hosting the auto stcp tunnel (id or name)")
	c.Flags().StringVar(&nodeName, "name", "", "Node name surfaced to the peer (defaults to instance name)")
	c.Flags().StringVar(&uiScheme, "ui-scheme", "http", "UI scheme advertised in the invitation (http or https)")
	return c
}

func newRemoteRefreshCmd(opts *GlobalOptions) *cobra.Command {
	var uiScheme string
	c := &cobra.Command{
		Use:   "refresh <id|name>",
		Short: "Rotate the mgmt_token + SK on an existing manages_me pairing",
		Long: "Used when the original invitation expired before the peer redeemed it,\n" +
			"or when rotating credentials. Replays the new tunnel SK into the driver\n" +
			"so the visitor only needs to import the freshly-emitted invitation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			node, err := resolveRemoteNode(st, args[0])
			closer()
			if err != nil {
				return err
			}
			req := struct {
				NodeID   uint            `json:"NodeID"`
				UIScheme string          `json:"UIScheme,omitempty"`
				Actor    invocationActor `json:"Actor"`
			}{
				NodeID:   node.ID,
				UIScheme: strings.TrimSpace(uiScheme),
				Actor:    invocationActor{Username: "cli"},
			}
			raw, err := invokeRemoteOps(cmd.Context(), opts, "remote.refresh", req)
			if err != nil {
				return err
			}
			return renderInvitationResult(cmd, opts, raw)
		},
	}
	c.Flags().StringVar(&uiScheme, "ui-scheme", "http", "UI scheme advertised in the invitation (http or https)")
	return c
}

func newRemoteRevokeMgmtTokenCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "revoke-token <id|name>",
		Short: "Void the outstanding mgmt_token without tearing down the pairing",
		Long: "Idempotent: revoking a row whose JTI is already empty returns the row\n" +
			"unchanged. The underlying stcp tunnel keeps working — call `frpdeck\n" +
			"remote refresh` afterwards if you also want to mint a fresh invitation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			node, err := resolveRemoteNode(st, args[0])
			closer()
			if err != nil {
				return err
			}
			req := struct {
				NodeID uint            `json:"NodeID"`
				Actor  invocationActor `json:"Actor"`
			}{NodeID: node.ID, Actor: invocationActor{Username: "cli"}}
			raw, err := invokeRemoteOps(cmd.Context(), opts, "remote.revoke-mgmt-token", req)
			if err != nil {
				return err
			}
			return renderRemoteNodeResult(cmd, opts, raw)
		},
	}
	return c
}

func newRemoteRevokeCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "revoke <id|name>",
		Short: "Tear down a remote pairing (stop tunnel, mark node revoked)",
		Long: "Performs the same operation in both directions: removes the auto stcp\n" +
			"tunnel from the driver, deletes its row, marks the RemoteNode as\n" +
			"revoked (kept in DB for audit). Not reversible — use `invite` to\n" +
			"create a fresh pairing if needed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			node, err := resolveRemoteNode(st, args[0])
			closer()
			if err != nil {
				return err
			}
			if !opts.Yes {
				return UsageError{Msg: fmt.Sprintf("refusing to revoke remote node %d (%s); pass --yes to confirm", node.ID, node.Name)}
			}
			req := struct {
				NodeID uint            `json:"NodeID"`
				Actor  invocationActor `json:"Actor"`
			}{NodeID: node.ID, Actor: invocationActor{Username: "cli"}}
			raw, err := invokeRemoteOps(cmd.Context(), opts, "remote.revoke", req)
			if err != nil {
				return err
			}
			return renderRemoteNodeResult(cmd, opts, raw)
		},
	}
	return c
}

// renderInvitationResult prints the InvitationResult according to the
// requested output format. For table mode we emit a vertical
// key/value pair view since the invitation string is too wide for a
// horizontal table — the operator usually wants to copy it verbatim.
func renderInvitationResult(cmd *cobra.Command, opts *GlobalOptions, raw json.RawMessage) error {
	var res invocationResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("decode invitation result: %w", err)
	}
	switch opts.Format {
	case output.FormatJSON, output.FormatYAML:
		return output.RenderRaw(cmd.OutOrStdout(), opts.Format, res)
	}
	row := map[string]any{
		"node_id":     res.Node.ID,
		"node_name":   res.Node.Name,
		"endpoint_id": res.Node.EndpointID,
		"tunnel_id":   res.TunnelID,
		"status":      res.Node.Status,
		"expire_at":   res.ExpireAt.Format(time.RFC3339),
		"invitation":  res.Invitation,
		"mgmt_token":  res.MgmtToken,
	}
	if res.DriverWarning != "" {
		row["driver_warning"] = res.DriverWarning
	}
	cols := []output.Column{
		{Title: "Node ID", Key: "node_id"},
		{Title: "Name", Key: "node_name"},
		{Title: "Endpoint ID", Key: "endpoint_id"},
		{Title: "Tunnel ID", Key: "tunnel_id"},
		{Title: "Status", Key: "status"},
		{Title: "Expire At", Key: "expire_at"},
		{Title: "Invitation", Key: "invitation"},
		{Title: "Mgmt Token", Key: "mgmt_token"},
	}
	if res.DriverWarning != "" {
		cols = append(cols, output.Column{Title: "Driver Warning", Key: "driver_warning"})
	}
	return output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row)
}

// renderRemoteNodeResult prints the RemoteNode row returned by the
// revoke / revoke-token RPCs. We reuse the read-only `nodes get`
// projection so the operator sees a familiar shape after the action.
func renderRemoteNodeResult(cmd *cobra.Command, opts *GlobalOptions, raw json.RawMessage) error {
	var node model.RemoteNode
	if err := json.Unmarshal(raw, &node); err != nil {
		return fmt.Errorf("decode remote node: %w", err)
	}
	switch opts.Format {
	case output.FormatJSON, output.FormatYAML:
		return output.RenderRaw(cmd.OutOrStdout(), opts.Format, node)
	}
	st, closer, err := opts.OpenStore()
	if err != nil {
		return err
	}
	defer closer()
	endpointNames, err := buildEndpointNameMap(st)
	if err != nil {
		return err
	}
	row := remoteNodeRow(&node, endpointNames)
	cols := []output.Column{
		{Title: "ID", Key: "id"},
		{Title: "Name", Key: "name"},
		{Title: "Direction", Key: "direction"},
		{Title: "Endpoint", Key: "endpoint"},
		{Title: "Tunnel ID", Key: "tunnel_id"},
		{Title: "Status", Key: "status"},
		{Title: "Invite Expiry", Key: "invite_expiry"},
		{Title: "Last Seen", Key: "last_seen"},
	}
	return output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row)
}

// resolveRemoteNode looks up by ID first (numeric) then by
// case-insensitive name. Same shape as resolveEndpoint.
func resolveRemoteNode(st *store.Store, ref string) (*model.RemoteNode, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("remote node reference required")
	}
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		n, err := st.GetRemoteNode(uint(id))
		if err != nil {
			return nil, err
		}
		if n == nil {
			return nil, fmt.Errorf("remote node id=%d not found", id)
		}
		return n, nil
	}
	all, err := st.ListRemoteNodes()
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(ref)
	var hit *model.RemoteNode
	for i := range all {
		if strings.ToLower(all[i].Name) == low {
			if hit != nil {
				return nil, fmt.Errorf("remote node name %q matches multiple rows; use ID instead", ref)
			}
			hit = &all[i]
		}
	}
	if hit == nil {
		return nil, fmt.Errorf("remote node %q not found", ref)
	}
	return hit, nil
}

func remoteNodeRow(n *model.RemoteNode, endpointNames map[uint]string) map[string]any {
	endpoint := endpointNames[n.EndpointID]
	if endpoint == "" && n.EndpointID != 0 {
		endpoint = fmt.Sprintf("ep#%d", n.EndpointID)
	}
	expiry := ""
	if n.InviteExpiry != nil {
		expiry = n.InviteExpiry.Format(time.RFC3339)
	}
	last := ""
	if n.LastSeen != nil {
		last = n.LastSeen.Format(time.RFC3339)
	}
	return map[string]any{
		"id":            n.ID,
		"name":          n.Name,
		"direction":     n.Direction,
		"endpoint":      endpoint,
		"endpoint_id":   n.EndpointID,
		"tunnel_id":     n.TunnelID,
		"status":        n.Status,
		"invite_expiry": expiry,
		"last_seen":     last,
	}
}
