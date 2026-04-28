package cmds

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

// NewRemoteCmd builds the `frpdeck remote` command tree.
//
// Coverage in P10-C: read-only `nodes list` / `nodes get`. The
// invite / refresh / revoke / revoke-token paths involve mgmt-token
// JWT issuance, driver tunnel push, and audit-log writes — all of
// which currently live inside the gin handler in
// `internal/api/remote.go`. Refactoring those into a daemon-shared
// helper is a P10-D scope item; until then, the CLI documents
// `frpdeck remote …` as the read-only side and points the operator
// to the Web UI for one-shot actions.
//
// Read-only is still useful: scripts that build a status board, or
// ops who want to confirm a node is `active` without opening the
// browser, get a clean answer with this command alone.
func NewRemoteCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "remote",
		Short: "Inspect remote-managed FrpDeck pairings (read-only in P10-C)",
	}
	nodes := &cobra.Command{
		Use:   "nodes",
		Short: "Operate on RemoteNode rows",
	}
	nodes.AddCommand(newRemoteNodesListCmd(opts), newRemoteNodesGetCmd(opts))
	c.AddCommand(nodes, newRemoteInviteStubCmd("invite"), newRemoteInviteStubCmd("refresh"), newRemoteInviteStubCmd("revoke"), newRemoteInviteStubCmd("revoke-token"))
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

// newRemoteInviteStubCmd surfaces the not-yet-CLI-implemented actions
// as visible subcommands so `frpdeck remote --help` advertises them
// — failing fast with a helpful pointer beats a silent omission that
// has the operator wondering whether they're holding it wrong.
func newRemoteInviteStubCmd(action string) *cobra.Command {
	return &cobra.Command{
		Use:   action,
		Short: "(P10-D) Mutating remote-pairing action — currently CLI-out-of-scope; use the Web UI",
		Long: "The mutating remote-pairing actions (invite / refresh / revoke /\n" +
			"revoke-token) involve mgmt-token JWT issuance, stcp tunnel push to the\n" +
			"running driver, and audit-log writes. The CLI's Direct-DB pattern\n" +
			"cannot reach those subsystems without the daemon's auth + driver\n" +
			"context.\n\n" +
			"Refactoring the API handler internals to live behind the control\n" +
			"socket is tracked as P10-D. Until then please use the Web UI's\n" +
			"\"Remote Nodes\" page for these actions; the read-only `frpdeck\n" +
			"remote nodes list / get` already covers status inspection.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return errExitCode{
				code: 64, // EX_USAGE
				msg:  fmt.Sprintf("`frpdeck remote %s` is not implemented in P10-C; use the Web UI's Remote Nodes page (tracked as P10-D)", action),
			}
		},
	}
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
