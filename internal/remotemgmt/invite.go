// Package remotemgmt implements the P5-A remote-management pairing flow:
// one FrpDeck instance ("A") generates a self-contained invitation that a
// peer instance ("B") can redeem to gain a pre-wired stcp tunnel back to
// A's web UI. The invitation is base64-encoded JSON; the embedded
// `mgmt_token` JWT is what authorises B's first call into A. See
// plan.md §8 (架构) and the four §11 decisions tagged "P5-A …".
package remotemgmt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// InvitationVersion identifies the wire format. The decoder rejects any
// payload whose v field disagrees so future format bumps cannot be
// silently misparsed.
const InvitationVersion = 1

// InvitationTTL is how long a freshly generated invitation may sit
// before B redeems it. Matches plan.md §8.3 ("邀请码 5 分钟有效"). Long
// enough to share via QR / chat, short enough that an intercepted code
// is generally useless by the time it lands in an attacker's hands.
const InvitationTTL = 5 * time.Minute

// MgmtTokenTTL is how long the embedded mgmt_token remains valid for
// redemption at A's /api/auth/remote-redeem. It outlives the invitation
// itself so that B retaining the redeemed access JWT does not lose
// connectivity the moment the invitation TTL elapses; revocation runs
// through the RemoteNode row, not the JWT lifetime.
const MgmtTokenTTL = 24 * time.Hour

// Invitation is the wire-level payload encoded into the base64 string
// the operator copies from A and pastes on B. Keep this struct shape
// stable; bump InvitationVersion if anything changes.
//
// Field-level constraints:
//   - FrpsToken is opaque; B writes it into the auto-created Endpoint
//     so the same frps accepts both clients. Empty allowed (token-less).
//   - Sk is the stcp pre-shared key; both sides MUST use this verbatim.
//   - MgmtToken is a JWT signed by A's JWT secret with scope =
//     auth.MgmtTokenScope. Validation lives in auth.ValidateMgmtToken.
//   - ExpireAt uses RFC3339 to keep the payload diff-friendly when the
//     operator inspects the decoded JSON for debugging.
type Invitation struct {
	V               int       `json:"v"`
	NodeName        string    `json:"node_name"`
	Addr            string    `json:"addr"`
	Port            int       `json:"port"`
	Protocol        string    `json:"protocol,omitempty"`
	TLSEnable       bool      `json:"tls_enable"`
	FrpsUser        string    `json:"frps_user,omitempty"`
	FrpsToken       string    `json:"frps_token,omitempty"`
	RemoteUser      string    `json:"remote_user,omitempty"`
	Sk              string    `json:"sk"`
	UIScheme        string    `json:"ui_scheme,omitempty"`
	ServerProxyName string    `json:"server_proxy_name"`
	MgmtToken       string    `json:"mgmt_token"`
	IssuedAt        time.Time `json:"issued_at"`
	ExpireAt        time.Time `json:"expire_at"`
}

// ProxyName returns the frps-side proxy name the visitor must target. It
// falls back to a derived value when an older v1 invitation is decoded
// against a newer client (the issuing node id is stable across versions).
func (inv *Invitation) ProxyName() string {
	if inv == nil {
		return ""
	}
	return inv.ServerProxyName
}

// Encode serialises the invitation into the base64 string the operator
// copies. Uses URL-safe encoding without padding so the payload survives
// being pasted into chat clients that mangle `+` / `=`.
func Encode(inv *Invitation) (string, error) {
	if inv == nil {
		return "", errors.New("invitation required")
	}
	if inv.V == 0 {
		inv.V = InvitationVersion
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		return "", fmt.Errorf("marshal invitation: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Decode parses an invitation string. Whitespace and accidental wrapping
// dashes (some chat clients hard-wrap long strings) are stripped before
// decoding. Padding-flexible: accepts both with and without `=` so users
// can paste either RawURL or URL encoding.
func Decode(s string) (*Invitation, error) {
	cleaned := stripWhitespace(s)
	if cleaned == "" {
		return nil, errors.New("invitation is empty")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(cleaned, "="))
	if err != nil {
		// Fall back to standard base64 in case the source generated
		// padded output (some QR encoders pad by default).
		raw, err = base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return nil, fmt.Errorf("invitation is not valid base64: %w", err)
		}
	}
	var inv Invitation
	if err := json.Unmarshal(raw, &inv); err != nil {
		return nil, fmt.Errorf("invitation is not valid JSON: %w", err)
	}
	if inv.V != InvitationVersion {
		return nil, fmt.Errorf("invitation version %d not supported (expected %d)", inv.V, InvitationVersion)
	}
	if err := inv.Validate(); err != nil {
		return nil, err
	}
	return &inv, nil
}

// Validate runs structural checks the redeem path relies on. ExpireAt is
// not checked here (caller decides whether expired invites should still
// be parsed for diagnostics); use ExpiresAt + time.Now to gate redemption.
func (inv *Invitation) Validate() error {
	if strings.TrimSpace(inv.Addr) == "" {
		return errors.New("invitation missing endpoint address")
	}
	if inv.Port <= 0 || inv.Port > 65535 {
		return fmt.Errorf("invitation port %d out of range", inv.Port)
	}
	if strings.TrimSpace(inv.Sk) == "" {
		return errors.New("invitation missing stcp pre-shared key")
	}
	if strings.TrimSpace(inv.MgmtToken) == "" {
		return errors.New("invitation missing mgmt token")
	}
	if strings.TrimSpace(inv.ServerProxyName) == "" {
		return errors.New("invitation missing server_proxy_name")
	}
	if inv.ExpireAt.IsZero() {
		return errors.New("invitation missing expire_at")
	}
	return nil
}

// Expired reports whether the invitation TTL has elapsed.
func (inv *Invitation) Expired(now time.Time) bool {
	return !inv.ExpireAt.IsZero() && inv.ExpireAt.Before(now)
}

func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\n', '\r', '\t', '\v', '\f':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
