package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// AuthMode enumerates the supported authentication strategies.
type AuthMode string

const (
	AuthModePassword    AuthMode = "password"
	AuthModeIPWhitelist AuthMode = "ipwhitelist"
	AuthModeNone        AuthMode = "none"
)

// Config is the fully-resolved runtime configuration for FrpDeck.
//
// All fields are populated from environment variables by Load. Sensible
// defaults cover a typical single-host deployment; see README for full details.
type Config struct {
	Listen                  string
	AuthMode                AuthMode
	AdminUsername           string
	AdminPassword           string
	AdminIPWhitelist        []*net.IPNet
	TrustedProxies          []*net.IPNet
	FrpcdDriver             string
	DataDir                 string
	HistoryRetentionDays    int
	MaxDurationHours        int
	MaxRulesPerIP           int
	RateLimitPerMinutePerIP int
	JWTSecret               string

	// Login hardening (brute-force defence). All values are tunable via
	// env vars; defaults target a balanced posture for a small self-hosted
	// tool. Window and lockout durations are in minutes.
	LoginFailMaxPerIP       int
	LoginFailWindowIPMin    int
	LoginFailMaxPerUser     int
	LoginFailWindowUserMin  int
	LoginLockoutIPMin       int
	LoginLockoutUserMin     int
	LoginMinPasswordLen     int
}

// Load reads environment variables and returns a populated Config, applying
// defaults where values are missing. Any invalid value causes a descriptive
// error so the operator can fix deployment configuration quickly.
func Load() (*Config, error) {
	cfg := &Config{
		Listen:                  envOr("FRPDECK_LISTEN", ":8080"),
		AuthMode:                AuthMode(strings.ToLower(envOr("FRPDECK_AUTH_MODE", string(AuthModePassword)))),
		AdminUsername:           envOr("FRPDECK_ADMIN_USERNAME", "admin"),
		AdminPassword:           os.Getenv("FRPDECK_ADMIN_PASSWORD"),
		FrpcdDriver:             strings.ToLower(envOr("FRPDECK_FRPCD_DRIVER", "embedded")),
		DataDir:                 envOr("FRPDECK_DATA_DIR", "/data"),
		HistoryRetentionDays:    envInt("FRPDECK_HISTORY_RETENTION_DAYS", 30),
		MaxDurationHours:        envInt("FRPDECK_MAX_DURATION_HOURS", 24),
		MaxRulesPerIP:           envInt("FRPDECK_MAX_RULES_PER_IP", 20),
		RateLimitPerMinutePerIP: envInt("FRPDECK_RATELIMIT_PER_MINUTE", 10),
		JWTSecret:               envOr("FRPDECK_JWT_SECRET", ""),

		LoginFailMaxPerIP:      envInt("FRPDECK_LOGIN_FAIL_MAX_PER_IP", 10),
		LoginFailWindowIPMin:   envInt("FRPDECK_LOGIN_FAIL_WINDOW_IP_MINUTES", 10),
		LoginFailMaxPerUser:    envInt("FRPDECK_LOGIN_FAIL_MAX_PER_USER", 5),
		LoginFailWindowUserMin: envInt("FRPDECK_LOGIN_FAIL_WINDOW_USER_MINUTES", 15),
		LoginLockoutIPMin:      envInt("FRPDECK_LOGIN_LOCKOUT_IP_MINUTES", 10),
		LoginLockoutUserMin:    envInt("FRPDECK_LOGIN_LOCKOUT_USER_MINUTES", 15),
		LoginMinPasswordLen:    envInt("FRPDECK_LOGIN_MIN_PASSWORD_LEN", 8),
	}

	switch cfg.AuthMode {
	case AuthModePassword, AuthModeIPWhitelist, AuthModeNone:
	default:
		return nil, fmt.Errorf("invalid FRPDECK_AUTH_MODE=%q", cfg.AuthMode)
	}

	nets, err := parseCIDRs(os.Getenv("FRPDECK_ADMIN_IP_WHITELIST"))
	if err != nil {
		return nil, fmt.Errorf("FRPDECK_ADMIN_IP_WHITELIST: %w", err)
	}
	cfg.AdminIPWhitelist = nets

	proxies, err := parseCIDRs(os.Getenv("FRPDECK_TRUSTED_PROXIES"))
	if err != nil {
		return nil, fmt.Errorf("FRPDECK_TRUSTED_PROXIES: %w", err)
	}
	cfg.TrustedProxies = proxies

	// FRPDECK_ADMIN_PASSWORD is no longer required in password mode: the
	// store seeds a default admin/passwd on first boot (with a log warning)
	// when the users table is empty, and after that credentials are
	// managed via the UI. The env var still has an effect when present
	// (used as the seed password on the very first boot only).
	if cfg.AuthMode == AuthModeIPWhitelist && len(cfg.AdminIPWhitelist) == 0 {
		return nil, fmt.Errorf("FRPDECK_AUTH_MODE=ipwhitelist requires FRPDECK_ADMIN_IP_WHITELIST")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// parseCIDRs accepts a comma-separated list of CIDR blocks or plain IPs. A
// bare IPv4 is normalised to /32 and IPv6 to /128 so callers can treat every
// entry uniformly as a network.
func parseCIDRs(raw string) ([]*net.IPNet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			if ip := net.ParseIP(p); ip != nil {
				if ip.To4() != nil {
					p += "/32"
				} else {
					p += "/128"
				}
			}
		}
		_, n, err := net.ParseCIDR(p)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", part, err)
		}
		out = append(out, n)
	}
	return out, nil
}
