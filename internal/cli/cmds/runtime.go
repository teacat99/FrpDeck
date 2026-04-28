package cmds

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/runtime"
)

// NewRuntimeCmd builds the `frpdeck runtime` command tree.
//
// The runtime KV is the daemon's hot-mutable settings store: rate
// limits, retention windows, ntfy topics, etc. CLI runtime get/set
// is always Direct-DB (the runtime.AllKeys list is the source of
// truth for which rows even exist), then we ping the daemon so the
// in-memory Settings struct reloads. Daemons not running just leave
// the KV change durable in SQLite — next boot picks it up via
// LoadFromKV.
func NewRuntimeCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "runtime",
		Short: "Manage hot-mutable runtime settings",
	}
	c.AddCommand(newRuntimeGetCmd(opts), newRuntimeSetCmd(opts), newRuntimeKeysCmd(opts))
	return c
}

func newRuntimeGetCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get [<key>]",
		Short: "Print one or every runtime setting from SQLite",
		Long: `Without arguments, prints the value (or "(default)") for every
known key. With a key argument, prints just that one. Pipe through
` + "`-o json`" + ` for shell automation.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			if len(args) == 1 {
				key := runtime.Key(strings.TrimSpace(args[0]))
				if !runtime.IsKnownKey(key) {
					return fmt.Errorf("unknown key %q (use `frpdeck runtime keys` to list)", key)
				}
				v, ok, err := st.LookupSetting(string(key))
				if err != nil {
					return err
				}
				row := map[string]any{
					"key":   string(key),
					"value": v,
					"set":   ok,
				}
				cols := []output.Column{
					{Title: "Key", Key: "key"},
					{Title: "Value", Key: "value"},
					{Title: "Persisted", Key: "set"},
				}
				return output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row)
			}
			rows := make([]map[string]any, 0, len(runtime.AllKeys))
			for _, k := range runtime.AllKeys {
				v, ok, err := st.LookupSetting(string(k))
				if err != nil {
					return err
				}
				rows = append(rows, map[string]any{
					"key":   string(k),
					"value": v,
					"set":   ok,
				})
			}
			sort.Slice(rows, func(i, j int) bool {
				return rows[i]["key"].(string) < rows[j]["key"].(string)
			})
			cols := []output.Column{
				{Title: "KEY", Key: "key"},
				{Title: "VALUE", Key: "value"},
				{Title: "SET", Key: "set"},
			}
			return output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...)
		},
	}
}

func newRuntimeSetCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Persist a runtime setting and ping the daemon to reload",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := runtime.Key(strings.TrimSpace(args[0]))
			val := args[1]
			if !runtime.IsKnownKey(key) {
				return fmt.Errorf("unknown key %q", key)
			}
			if err := runtime.Validate(key, val); err != nil {
				return fmt.Errorf("invalid value for %s: %w", key, err)
			}
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			if err := st.SetSetting(string(key), val); err != nil {
				return err
			}
			if err := pingReloadRuntime(opts.SocketClient); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "runtime: %s = %s\n", key, val)
			return nil
		},
	}
}

func newRuntimeKeysCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "keys",
		Short: "List every known runtime setting key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows := make([]map[string]any, len(runtime.AllKeys))
			for i, k := range runtime.AllKeys {
				rows[i] = map[string]any{"key": string(k)}
			}
			cols := []output.Column{{Title: "KEY", Key: "key"}}
			return output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...)
		},
	}
}

