package cmds

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// NewCompletionCmd builds `frpdeck completion`. We delegate to
// cobra's stock generators because they already cover bash 4+,
// bash 3.2 (macOS), zsh, fish, and PowerShell — re-implementing
// these by hand has a lot of edge cases (subcommand aliases,
// flag completion descriptions, etc.) we'd inevitably get wrong.
//
// Usage examples:
//
//	frpdeck completion bash > /etc/bash_completion.d/frpdeck
//	frpdeck completion zsh  > "${fpath[1]}/_frpdeck"
//	frpdeck completion fish > ~/.config/fish/completions/frpdeck.fish
//	frpdeck completion powershell | Out-String | Invoke-Expression
func NewCompletionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                   "completion [bash|zsh|fish|powershell]",
		Short:                 "Generate shell completion scripts",
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			}
			return fmt.Errorf("unsupported shell %q", args[0])
		},
	}
	return c
}

// NewDocCmd builds `frpdeck doc man <out_dir>`. Generated man pages
// are dropped into <out_dir>/frpdeck-*.1; the operator can then `cp`
// the files into the system man directory or ship them in a
// distro package without us baking man-renderer logic into the binary.
func NewDocCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "doc",
		Short: "Generate documentation artifacts (man pages, etc.)",
	}
	c.AddCommand(newDocManCmd(), newDocMarkdownCmd())
	return c
}

func newDocManCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "man <out_dir>",
		Short: "Render man pages for every frpdeck subcommand",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outDir := args[0]
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			header := &doc.GenManHeader{
				Title:   "FRPDECK",
				Section: "1",
				Source:  "FrpDeck",
				Manual:  "FrpDeck Manual",
			}
			return doc.GenManTree(cmd.Root(), header, outDir)
		},
	}
}

func newDocMarkdownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "markdown <out_dir>",
		Short: "Render markdown reference for every frpdeck subcommand",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outDir := args[0]
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			return doc.GenMarkdownTree(cmd.Root(), filepath.Clean(outDir))
		},
	}
}
