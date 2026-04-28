// Package runtime exposes the subset of FrpDeck configuration that may
// be edited from the admin UI without restarting the process. Everything
// in this package is mutex-guarded so concurrent readers (request hot
// path) and the rare writer (PUT /api/runtime-settings) cannot race.
//
// Layering:
//
//   - internal/config.Config:   env-driven, immutable, owns boot-time
//     values that genuinely require a restart (Listen, DataDir, JWT
//     secret, firewall driver).
//
//   - internal/runtime.Settings: superset of the hot-mutable subset of
//     Config plus extension fields (ntfy, captcha, subnet bits) that
//     never live in Config because they are only ever set from the UI.
//
// Boot order: load Config from env  ->  construct runtime.Settings,
// seeding from Config defaults  ->  LoadFromKV() to overlay any values
// the operator has saved on previous runs  ->  hand it to api/auth.
package runtime

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teacat99/FrpDeck/internal/config"
)

// Key is a stable string identifier for one runtime-mutable field. The
// values are persisted verbatim in the settings KV table and accepted
// by PUT /api/runtime-settings so we keep them short and immutable.
type Key string

const (
	// Rule limits.
	KeyMaxDurationHours        Key = "max_duration_hours"
	KeyHistoryRetentionDays    Key = "history_retention_days"
	KeyMaxRulesPerIP           Key = "max_rules_per_ip"
	KeyRateLimitPerMinutePerIP Key = "rate_limit_per_minute_per_ip"

	// Login hardening.
	KeyLoginFailMaxPerIP      Key = "login_fail_max_per_ip"
	KeyLoginFailWindowIPMin   Key = "login_fail_window_ip_min"
	KeyLoginFailMaxPerUser    Key = "login_fail_max_per_user"
	KeyLoginFailWindowUserMin Key = "login_fail_window_user_min"
	KeyLoginLockoutIPMin      Key = "login_lockout_ip_min"
	KeyLoginLockoutUserMin    Key = "login_lockout_user_min"
	KeyLoginMinPasswordLen    Key = "login_min_password_len"

	// Optional defences.
	KeyLoginFailSubnetBits Key = "login_fail_subnet_bits"
	KeyCaptchaThreshold    Key = "captcha_threshold"

	// Notifications (ntfy).
	KeyNtfyURL   Key = "ntfy_url"
	KeyNtfyTopic Key = "ntfy_topic"
	KeyNtfyToken Key = "ntfy_token"

	// Tunnel lifecycle.
	//
	// KeyTunnelExpiringNotifyMinutes controls how far ahead of a temporary
	// tunnel's ExpireAt the lifecycle manager publishes a `tunnel_expiring`
	// event so the UI / ntfy can warn the operator before the proxy is
	// auto-stopped. 0 disables the early warning entirely.
	KeyTunnelExpiringNotifyMinutes Key = "tunnel_expiring_notify_minutes"
)

// AllKeys lists every key the API will accept on writes. The slice is
// also used by LoadFromKV to know which rows to read on boot.
var AllKeys = []Key{
	KeyMaxDurationHours,
	KeyHistoryRetentionDays,
	KeyMaxRulesPerIP,
	KeyRateLimitPerMinutePerIP,

	KeyLoginFailMaxPerIP,
	KeyLoginFailWindowIPMin,
	KeyLoginFailMaxPerUser,
	KeyLoginFailWindowUserMin,
	KeyLoginLockoutIPMin,
	KeyLoginLockoutUserMin,
	KeyLoginMinPasswordLen,

	KeyLoginFailSubnetBits,
	KeyCaptchaThreshold,

	KeyNtfyURL,
	KeyNtfyTopic,
	KeyNtfyToken,

	KeyTunnelExpiringNotifyMinutes,
}

// Settings is the live, mutable runtime configuration. Read paths take
// the RWMutex in read mode (cheap); the rare PUT writer takes it in
// write mode. Numeric and string fields are kept inside the struct so
// readers never escape the lock to dereference a pointer.
type Settings struct {
	mu sync.RWMutex

	// Numeric hot fields, mirrored from config.Config on boot.
	maxDurationHours        int
	historyRetentionDays    int
	maxRulesPerIP           int
	rateLimitPerMinutePerIP int

	loginFailMaxPerIP      int
	loginFailWindowIPMin   int
	loginFailMaxPerUser    int
	loginFailWindowUserMin int
	loginLockoutIPMin      int
	loginLockoutUserMin    int
	loginMinPasswordLen    int

	// Optional defences.
	loginFailSubnetBits int // 0 disables subnet aggregation
	captchaThreshold    int // 0 disables captcha; otherwise N failures within window

	// Notifications.
	ntfyURL   string
	ntfyTopic string
	ntfyToken string

	// Tunnel lifecycle.
	tunnelExpiringNotifyMinutes int

	// Hooks invoked AFTER a successful Set; one per key. Optional.
	hooks map[Key][]func()
}

// New seeds a Settings from boot-time Config defaults. ntfy / captcha /
// subnet fields are zero by default because they are pure UI features
// without env vars (they exist only to be flipped on later from the UI).
func New(cfg *config.Config) *Settings {
	s := &Settings{
		maxDurationHours:        cfg.MaxDurationHours,
		historyRetentionDays:    cfg.HistoryRetentionDays,
		maxRulesPerIP:           cfg.MaxRulesPerIP,
		rateLimitPerMinutePerIP: cfg.RateLimitPerMinutePerIP,

		loginFailMaxPerIP:      cfg.LoginFailMaxPerIP,
		loginFailWindowIPMin:   cfg.LoginFailWindowIPMin,
		loginFailMaxPerUser:    cfg.LoginFailMaxPerUser,
		loginFailWindowUserMin: cfg.LoginFailWindowUserMin,
		loginLockoutIPMin:      cfg.LoginLockoutIPMin,
		loginLockoutUserMin:    cfg.LoginLockoutUserMin,
		loginMinPasswordLen:    cfg.LoginMinPasswordLen,

		loginFailSubnetBits: 0,
		captchaThreshold:    3, // out of the box: show math after 3 failures

		// 5 minutes feels right for the typical "show RDP to colleague"
		// use case; long-lived hosting setups will bump this from the UI.
		tunnelExpiringNotifyMinutes: 5,
	}
	return s
}

// AddHook registers a callback fired AFTER a successful Set on key.
// Used by the API server to refresh the live rate limiter when the
// per-IP threshold changes, etc. Hooks must not call back into this
// Settings (they fire after the lock is released, but a re-entrant Set
// would still serialise behind the next read).
func (s *Settings) AddHook(key Key, fn func()) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hooks == nil {
		s.hooks = make(map[Key][]func())
	}
	s.hooks[key] = append(s.hooks[key], fn)
}

// LoadFromKV walks AllKeys and overlays any persisted value over the
// boot defaults. The supplied loader returns (value, present, err);
// "not found" is signalled with present=false rather than an error so
// missing rows do not abort the load.
func (s *Settings) LoadFromKV(get func(key string) (value string, present bool, err error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range AllKeys {
		v, ok, err := get(string(k))
		if err != nil {
			return fmt.Errorf("load %s: %w", k, err)
		}
		if !ok {
			continue
		}
		if err := s.applyLocked(k, v); err != nil {
			// A persisted value that no longer validates is logged but
			// does not crash the server: the operator may have manually
			// edited the row, or we tightened validation.
			return fmt.Errorf("apply %s=%q: %w", k, v, err)
		}
	}
	return nil
}

// Set validates value against the rules for key, applies it in memory,
// and (if save is non-nil) persists it via the supplied KV writer.
// Returns an error when validation fails; the in-memory state is left
// untouched in that case.
func (s *Settings) Set(key Key, value string, save func(k, v string) error) error {
	s.mu.Lock()
	if err := s.applyLocked(key, value); err != nil {
		s.mu.Unlock()
		return err
	}
	hooks := append([]func(){}, s.hooks[key]...)
	s.mu.Unlock()

	if save != nil {
		if err := save(string(key), value); err != nil {
			return err
		}
	}
	for _, fn := range hooks {
		fn()
	}
	return nil
}

// SetMany applies a batch atomically: every value is validated first
// (without persisting), and only when all pass do we persist + fire
// hooks. This avoids leaving the system in a half-applied state when
// the UI submits multiple fields at once.
func (s *Settings) SetMany(values map[Key]string, save func(k, v string) error) error {
	if len(values) == 0 {
		return nil
	}
	// Validate first by replaying onto a clone snapshot; we cannot
	// easily clone the struct without copying the mutex so we just do
	// a "dry run" by calling validateOnly on each key.
	for k, v := range values {
		if !isKnownKey(k) {
			return fmt.Errorf("unknown key %q", k)
		}
		if _, err := validateOnly(k, v); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	s.mu.Lock()
	for k, v := range values {
		if err := s.applyLocked(k, v); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	hookSet := make(map[Key][]func(), len(values))
	for k := range values {
		hookSet[k] = append([]func(){}, s.hooks[k]...)
	}
	s.mu.Unlock()

	if save != nil {
		for k, v := range values {
			if err := save(string(k), v); err != nil {
				return err
			}
		}
	}
	for _, fns := range hookSet {
		for _, fn := range fns {
			fn()
		}
	}
	return nil
}

// applyLocked must be called with s.mu held in write mode.
func (s *Settings) applyLocked(key Key, raw string) error {
	parsed, err := validateOnly(key, raw)
	if err != nil {
		return err
	}
	switch key {
	case KeyMaxDurationHours:
		s.maxDurationHours = parsed.(int)
	case KeyHistoryRetentionDays:
		s.historyRetentionDays = parsed.(int)
	case KeyMaxRulesPerIP:
		s.maxRulesPerIP = parsed.(int)
	case KeyRateLimitPerMinutePerIP:
		s.rateLimitPerMinutePerIP = parsed.(int)

	case KeyLoginFailMaxPerIP:
		s.loginFailMaxPerIP = parsed.(int)
	case KeyLoginFailWindowIPMin:
		s.loginFailWindowIPMin = parsed.(int)
	case KeyLoginFailMaxPerUser:
		s.loginFailMaxPerUser = parsed.(int)
	case KeyLoginFailWindowUserMin:
		s.loginFailWindowUserMin = parsed.(int)
	case KeyLoginLockoutIPMin:
		s.loginLockoutIPMin = parsed.(int)
	case KeyLoginLockoutUserMin:
		s.loginLockoutUserMin = parsed.(int)
	case KeyLoginMinPasswordLen:
		s.loginMinPasswordLen = parsed.(int)

	case KeyLoginFailSubnetBits:
		s.loginFailSubnetBits = parsed.(int)
	case KeyCaptchaThreshold:
		s.captchaThreshold = parsed.(int)

	case KeyNtfyURL:
		s.ntfyURL = parsed.(string)
	case KeyNtfyTopic:
		s.ntfyTopic = parsed.(string)
	case KeyNtfyToken:
		s.ntfyToken = parsed.(string)
	case KeyTunnelExpiringNotifyMinutes:
		s.tunnelExpiringNotifyMinutes = parsed.(int)
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// Validate exposes the per-key validation rules without touching any
// in-memory state. The standalone `frpdeck` CLI uses this to refuse
// invalid values before they ever reach the SQLite KV table — so the
// daemon's next ReloadFromKV() does not have to roll back a bad
// persisted value at startup.
func Validate(key Key, raw string) error {
	if !isKnownKey(key) {
		return fmt.Errorf("unknown key %q", key)
	}
	_, err := validateOnly(key, raw)
	return err
}

// IsKnownKey reports whether key is a recognised runtime setting.
// Exposed so the CLI can fail fast on typos before consulting the DB.
func IsKnownKey(key Key) bool { return isKnownKey(key) }

// validateOnly returns the parsed value (int or string) without
// touching state. Used by both applyLocked and SetMany's dry run.
func validateOnly(key Key, raw string) (any, error) {
	switch key {
	case KeyMaxDurationHours:
		return parseRange(raw, 1, 24*30)
	case KeyHistoryRetentionDays:
		return parseRange(raw, 0, 365)
	case KeyMaxRulesPerIP:
		return parseRange(raw, 0, 10000)
	case KeyRateLimitPerMinutePerIP:
		return parseRange(raw, 0, 100000)

	case KeyLoginFailMaxPerIP, KeyLoginFailMaxPerUser:
		return parseRange(raw, 0, 1000)
	case KeyLoginFailWindowIPMin, KeyLoginFailWindowUserMin:
		return parseRange(raw, 0, 24*60)
	case KeyLoginLockoutIPMin, KeyLoginLockoutUserMin:
		return parseRange(raw, 0, 24*60)
	case KeyLoginMinPasswordLen:
		return parseRange(raw, 4, 128)

	case KeyLoginFailSubnetBits:
		// Accept 0 (disabled), or a sane CIDR-prefix length for either
		// IPv4 (8..32) or IPv6 (16..128). We store bits-from-the-left
		// matching `IP/N`. 0 is the wildcard "off".
		n, err := parseRange(raw, 0, 128)
		if err != nil {
			return 0, err
		}
		if n != 0 && n < 8 {
			return 0, fmt.Errorf("subnet bits must be 0 (disabled) or >= 8")
		}
		return n, nil
	case KeyCaptchaThreshold:
		return parseRange(raw, 0, 100)

	case KeyNtfyURL:
		v := strings.TrimSpace(raw)
		if v != "" && !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
			return "", errors.New("ntfy_url must start with http:// or https://")
		}
		if len(v) > 256 {
			return "", errors.New("ntfy_url too long")
		}
		return v, nil
	case KeyNtfyTopic:
		v := strings.TrimSpace(raw)
		if len(v) > 64 {
			return "", errors.New("ntfy_topic too long")
		}
		return v, nil
	case KeyNtfyToken:
		v := strings.TrimSpace(raw)
		if len(v) > 256 {
			return "", errors.New("ntfy_token too long")
		}
		return v, nil
	case KeyTunnelExpiringNotifyMinutes:
		// 0 disables; otherwise 1..120 minutes is a reasonable
		// guardrail (above two hours and the warning blends into noise).
		return parseRange(raw, 0, 120)
	}
	return nil, fmt.Errorf("unknown key %q", key)
}

func isKnownKey(k Key) bool {
	for _, x := range AllKeys {
		if x == k {
			return true
		}
	}
	return false
}

func parseRange(raw string, lo, hi int) (int, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, fmt.Errorf("value required")
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("not an integer: %q", raw)
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("out of range [%d,%d]: %d", lo, hi, n)
	}
	return n, nil
}

// ---------- read getters (RLock) ----------

func (s *Settings) MaxDurationHours() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxDurationHours
}
func (s *Settings) HistoryRetentionDays() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.historyRetentionDays
}
func (s *Settings) MaxRulesPerIP() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxRulesPerIP
}
func (s *Settings) RateLimitPerMinutePerIP() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rateLimitPerMinutePerIP
}

func (s *Settings) LoginFailMaxPerIP() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginFailMaxPerIP
}
func (s *Settings) LoginFailWindowIPMin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginFailWindowIPMin
}
func (s *Settings) LoginFailMaxPerUser() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginFailMaxPerUser
}
func (s *Settings) LoginFailWindowUserMin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginFailWindowUserMin
}
func (s *Settings) LoginLockoutIPMin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginLockoutIPMin
}
func (s *Settings) LoginLockoutUserMin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginLockoutUserMin
}
func (s *Settings) LoginMinPasswordLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginMinPasswordLen
}

func (s *Settings) LoginFailSubnetBits() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginFailSubnetBits
}
func (s *Settings) CaptchaThreshold() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.captchaThreshold
}

func (s *Settings) NtfyURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ntfyURL
}
func (s *Settings) NtfyTopic() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ntfyTopic
}
func (s *Settings) NtfyToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ntfyToken
}

// TunnelExpiringNotifyMinutes returns the early-warning threshold (in
// minutes) the lifecycle manager uses to publish `tunnel_expiring`
// events. 0 means the operator disabled advance warnings entirely.
func (s *Settings) TunnelExpiringNotifyMinutes() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tunnelExpiringNotifyMinutes
}

// Snapshot returns a JSON-friendly view of every hot field plus the
// rendered "current" value. Used by GET /api/runtime-settings so the
// UI can render a single coherent state.
func (s *Settings) Snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		string(KeyMaxDurationHours):        s.maxDurationHours,
		string(KeyHistoryRetentionDays):    s.historyRetentionDays,
		string(KeyMaxRulesPerIP):           s.maxRulesPerIP,
		string(KeyRateLimitPerMinutePerIP): s.rateLimitPerMinutePerIP,

		string(KeyLoginFailMaxPerIP):      s.loginFailMaxPerIP,
		string(KeyLoginFailWindowIPMin):   s.loginFailWindowIPMin,
		string(KeyLoginFailMaxPerUser):    s.loginFailMaxPerUser,
		string(KeyLoginFailWindowUserMin): s.loginFailWindowUserMin,
		string(KeyLoginLockoutIPMin):      s.loginLockoutIPMin,
		string(KeyLoginLockoutUserMin):    s.loginLockoutUserMin,
		string(KeyLoginMinPasswordLen):    s.loginMinPasswordLen,

		string(KeyLoginFailSubnetBits): s.loginFailSubnetBits,
		string(KeyCaptchaThreshold):    s.captchaThreshold,

		string(KeyNtfyURL):   s.ntfyURL,
		string(KeyNtfyTopic): s.ntfyTopic,
		// ntfy_token is intentionally redacted from snapshots so it is
		// never echoed back to the client; the UI only writes it.
		string(KeyNtfyToken): maskToken(s.ntfyToken),

		string(KeyTunnelExpiringNotifyMinutes): s.tunnelExpiringNotifyMinutes,
	}
}

// LoginWindow* helpers package the rolling window into a time.Duration
// for the auth handler so it does not need to know minutes-vs-seconds.
func (s *Settings) LoginIPWindow() time.Duration {
	return time.Duration(s.LoginFailWindowIPMin()) * time.Minute
}
func (s *Settings) LoginUserWindow() time.Duration {
	return time.Duration(s.LoginFailWindowUserMin()) * time.Minute
}

func maskToken(t string) string {
	if t == "" {
		return ""
	}
	if len(t) <= 4 {
		return "****"
	}
	return t[:2] + "****" + t[len(t)-2:]
}
