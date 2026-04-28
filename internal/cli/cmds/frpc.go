package cmds

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/frpcd"
)

// NewFrpcCmd builds the `frpdeck frpc` command tree.
//
// Two utilities live here:
//
//   - probe: validate that an operator-supplied frpc binary actually
//     responds to `-v` and is at least frpcd.MinimumFrpVersion.
//   - download: fetch a tagged release from github.com/fatedier/frp,
//     drop it under <data_dir>/bin/, and print the on-disk path so
//     `frpdeck endpoint update --subprocess-path …` can use it.
//
// The download subcommand intentionally mirrors the API handler in
// internal/api/subprocess.go — same data-dir layout, same minimum
// version gate, optional same SHA256 verification — so binaries
// downloaded by the CLI and the UI are interchangeable.
func NewFrpcCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "frpc",
		Short: "Probe / download external frpc binaries",
	}
	c.AddCommand(newFrpcProbeCmd(opts), newFrpcDownloadCmd(opts))
	return c
}

func newFrpcProbeCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "probe <path>",
		Short: "Run `<path> -v` and report the parsed version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			version, err := frpcd.ProbeFrpcVersion(ctx, args[0])
			if err != nil {
				return err
			}
			ok := frpcd.CompareVersion(version, frpcd.MinimumFrpVersion)
			row := map[string]any{
				"path":         args[0],
				"version":      version,
				"min_required": frpcd.MinimumFrpVersion,
				"acceptable":   ok,
			}
			cols := []output.Column{
				{Title: "Path", Key: "path"},
				{Title: "Version", Key: "version"},
				{Title: "Min Required", Key: "min_required"},
				{Title: "Acceptable", Key: "acceptable"},
			}
			if err := output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row); err != nil {
				return err
			}
			if !ok {
				return errExitCode{code: 1, msg: "frpc version below minimum required"}
			}
			return nil
		},
	}
}

func newFrpcDownloadCmd(opts *GlobalOptions) *cobra.Command {
	var version, os, arch, sha256 string
	c := &cobra.Command{
		Use:   "download",
		Short: "Fetch a frpc release tarball, verify, and install under <data_dir>/bin/",
		Long: `Downloads the requested frpc release from GitHub, verifies the
SHA256 (when --sha256 is supplied), extracts the binary, and writes it
to <data_dir>/bin/frpc-<version>[.exe]. After it returns you can point
an endpoint at the path with:

    frpdeck endpoint update <name> --driver subprocess \
        --subprocess-path <printed path>

When --version is omitted we use the bundled frp version so a freshly
installed FrpDeck can populate the on-disk binary without consulting
GitHub for the latest tag.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !strings.HasPrefix(version, "v") {
				version = "v" + strings.TrimSpace(version)
			}
			if !frpcd.CompareVersion(version, frpcd.MinimumFrpVersion) {
				return fmt.Errorf("requested version %s is below minimum supported %s", version, frpcd.MinimumFrpVersion)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			path, err := frpcd.DownloadFrpc(ctx, frpcd.DownloadOptions{
				Version:        version,
				OS:             os,
				Arch:           arch,
				ExpectedSHA256: sha256,
				DataDir:        opts.DataDir,
			})
			if err != nil {
				return err
			}
			row := map[string]any{
				"version": version,
				"os":      defaultIfEmpty(os, goruntime.GOOS),
				"arch":    defaultIfEmpty(arch, goruntime.GOARCH),
				"path":    path,
			}
			cols := []output.Column{
				{Title: "Version", Key: "version"},
				{Title: "OS", Key: "os"},
				{Title: "Arch", Key: "arch"},
				{Title: "Path", Key: "path"},
			}
			return output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row)
		},
	}
	c.Flags().StringVar(&version, "version", frpcd.BundledFrpVersion, "frp release tag (e.g. v0.68.1)")
	c.Flags().StringVar(&os, "os", "", "Target OS (default: host OS)")
	c.Flags().StringVar(&arch, "arch", "", "Target arch (default: host arch)")
	c.Flags().StringVar(&sha256, "sha256", "", "Expected SHA256 of the release archive (skips verification when empty)")
	return c
}

func defaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
