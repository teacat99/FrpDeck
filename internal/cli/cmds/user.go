package cmds

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/model"
)

// NewUserCmd builds the `frpdeck user` command tree.
//
// All four subcommands operate Direct-DB and are safe to run with
// the daemon up: the users table is read at request time by the
// auth middleware (no in-memory cache to invalidate). The password
// hash uses bcrypt at the same cost as auth.LoginHandler so the new
// hash is indistinguishable from a UI-issued one.
func NewUserCmd(opts *GlobalOptions) *cobra.Command {
	user := &cobra.Command{
		Use:   "user",
		Short: "Manage local user accounts",
	}
	user.AddCommand(
		newUserListCmd(opts),
		newUserAddCmd(opts),
		newUserPasswdCmd(opts),
		newUserRemoveCmd(opts),
	)
	return user
}

func newUserListCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all user accounts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			users, err := st.ListUsers()
			if err != nil {
				return err
			}
			rows := make([]map[string]any, len(users))
			for i, u := range users {
				rows[i] = map[string]any{
					"id":         u.ID,
					"username":   u.Username,
					"role":       u.Role,
					"disabled":   u.Disabled,
					"created_at": u.CreatedAt.Format(time.RFC3339),
				}
			}
			cols := []output.Column{
				{Title: "ID", Key: "id"},
				{Title: "USERNAME", Key: "username"},
				{Title: "ROLE", Key: "role"},
				{Title: "DISABLED", Key: "disabled"},
				{Title: "CREATED", Key: "created_at"},
			}
			return output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...)
		},
	}
}

func newUserAddCmd(opts *GlobalOptions) *cobra.Command {
	var role string
	var password string
	c := &cobra.Command{
		Use:   "add <username>",
		Short: "Create a new user account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := strings.TrimSpace(args[0])
			if username == "" {
				return errors.New("username must not be empty")
			}
			if role != model.RoleAdmin && role != model.RoleUser {
				return fmt.Errorf("invalid role %q (expected admin | user)", role)
			}
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()

			if existing, err := st.GetUserByUsername(username); err == nil && existing != nil {
				return fmt.Errorf("user %q already exists (id=%d); use `user passwd` to change the password", username, existing.ID)
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			pw := password
			if pw == "" {
				pw, err = promptPassword(cmd.OutOrStdout(), "Password for "+username+": ")
				if err != nil {
					return err
				}
			}
			if pw == "" {
				return errors.New("password must not be empty")
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}
			now := time.Now()
			u := &model.User{
				Username:     username,
				PasswordHash: string(hash),
				Role:         role,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := st.CreateUser(u); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "user: created %s (id=%d, role=%s)\n", username, u.ID, role)
			return nil
		},
	}
	c.Flags().StringVar(&role, "role", model.RoleUser, "Role: admin | user")
	c.Flags().StringVar(&password, "password", "", "Password (omit to read from stdin)")
	return c
}

func newUserPasswdCmd(opts *GlobalOptions) *cobra.Command {
	var newPassword string
	c := &cobra.Command{
		Use:   "passwd <username>",
		Short: "Reset the password of a user (rescue scenario)",
		Long: `Reset a user's password. The new password may be supplied via
--password (handy for scripts) or read from stdin (the default — the
prompt does not echo on a terminal). The bcrypt cost matches the
LoginHandler so the rewritten hash is indistinguishable from a
UI-issued one.

Safe to run while the daemon is up: the auth middleware reads the
users table per request, no in-process cache to invalidate.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := strings.TrimSpace(args[0])
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			u, err := st.GetUserByUsername(username)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("user %q not found", username)
				}
				return err
			}
			pw := newPassword
			if pw == "" {
				pw, err = promptPassword(cmd.OutOrStdout(), "New password for "+username+": ")
				if err != nil {
					return err
				}
			}
			if pw == "" {
				return errors.New("password must not be empty")
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}
			if err := st.SetUserPasswordHash(u.ID, string(hash)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "user: password updated for %s (id=%d)\n", username, u.ID)
			return nil
		},
	}
	c.Flags().StringVar(&newPassword, "password", "", "New password (omit to read from stdin without echo)")
	return c
}

func newUserRemoveCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <username>",
		Short: "Delete a user account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := strings.TrimSpace(args[0])
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()
			u, err := st.GetUserByUsername(username)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("user %q not found", username)
				}
				return err
			}
			// Refuse to drop the last active admin so the operator
			// cannot lock themselves out via CLI any easier than via
			// the UI (which has the same guard).
			if u.Role == model.RoleAdmin && !u.Disabled {
				count, err := st.CountActiveAdmins()
				if err != nil {
					return err
				}
				if count <= 1 {
					return fmt.Errorf("refusing to remove the last active admin %q", username)
				}
			}
			if !opts.Yes {
				if err := confirm(cmd.OutOrStdout(), fmt.Sprintf("Remove user %s (id=%d)? [y/N]: ", username, u.ID)); err != nil {
					return err
				}
			}
			if err := st.DeleteUser(u.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "user: removed %s (id=%d)\n", username, u.ID)
			return nil
		},
	}
}

// promptPassword reads a line from stdin without echoing. We do
// not depend on golang.org/x/term to keep dependencies minimal;
// when stdin is not a TTY (CI / pipeline) the prompt prints
// silently and the password is read with normal echo from the
// shell — exactly the behaviour the operator expects.
func promptPassword(out interface{ Write([]byte) (int, error) }, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func confirm(out interface{ Write([]byte) (int, error) }, prompt string) error {
	fmt.Fprint(out, prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	default:
		return errors.New("aborted")
	}
}
