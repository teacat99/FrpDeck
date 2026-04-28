package cmds

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/envfile"
)

// NewAuthCmd builds the `frpdeck auth` command tree.
//
// AuthMode is *not* persisted in SQLite — it is loaded from
// FRPDECK_AUTH_MODE in /etc/frpdeck/frpdeck.env (or the platform
// equivalent) at boot. So `auth mode set` has to rewrite the env
// file in place. Because env-driven config is read once at startup,
// we cannot live-reload the change via the control socket; we tell
// the user to restart the service, which is unavoidable for any
// env-sourced setting.
func NewAuthCmd(opts *GlobalOptions) *cobra.Command {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "View and change the authentication mode",
	}
	auth.AddCommand(newAuthShowCmd(opts), newAuthModeCmd(opts))
	return auth
}

func newAuthShowCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the currently configured auth mode",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := defaultEnvFilePath()
			values, err := envfile.Read(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("env file not found at %s; this CLI relies on the systemd-style env file written by `frpdeck-server install`", path)
				}
				return err
			}
			mode := values["FRPDECK_AUTH_MODE"]
			if mode == "" {
				mode = string(config.AuthModePassword)
			}
			row := map[string]any{
				"env_file":  path,
				"auth_mode": mode,
			}
			cols := []output.Column{
				{Title: "ENV FILE", Key: "env_file"},
				{Title: "AUTH MODE", Key: "auth_mode"},
			}
			return output.RenderSingle(cmd.OutOrStdout(), opts.Format, cols, row)
		},
	}
}

func newAuthModeCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "mode <password|ipwhitelist|none>",
		Short: "Change the auth mode (rewrites the env file; needs service restart)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(strings.TrimSpace(args[0]))
			switch config.AuthMode(mode) {
			case config.AuthModePassword, config.AuthModeIPWhitelist, config.AuthModeNone:
			default:
				return fmt.Errorf("invalid auth mode %q (expected password | ipwhitelist | none)", mode)
			}
			path := defaultEnvFilePath()
			values, err := envfile.Read(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if values == nil {
				values = map[string]string{}
			}
			values["FRPDECK_AUTH_MODE"] = mode
			if err := envfile.Write(path, values); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "auth: env file updated → FRPDECK_AUTH_MODE=%s\n", mode)
			fmt.Fprintf(cmd.OutOrStdout(), "auth: restart the service to apply (e.g. `systemctl restart frpdeck` on Linux)\n")
			return nil
		},
	}
}

// defaultEnvFilePath mirrors install.go so the CLI and the
// installer agree on the canonical path. We do not import
// cmd/server here because that would pull in service-lifecycle
// code; duplicating the four-line decision is the lesser evil.
func defaultEnvFilePath() string {
	if runtime.GOOS == "windows" {
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "frpdeck", "frpdeck.env")
	}
	return "/etc/frpdeck/frpdeck.env"
}
