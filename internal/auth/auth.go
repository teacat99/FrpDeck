package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/netutil"
	"github.com/teacat99/FrpDeck/internal/runtime"
)

// UserRepo captures the parts of the user store that the authenticator
// needs. Keeping it narrow lets the unit tests provide a lightweight fake
// without booting a real SQLite database. The login-attempt methods back
// the brute-force defence; the user methods back the credential lookup.
type UserRepo interface {
	GetUserByUsername(name string) (*model.User, error)
	GetUserByID(id uint) (*model.User, error)

	RecordLoginAttempt(a *model.LoginAttempt) error
	CountLoginFailuresByIP(ip string, since time.Time) (int64, error)
	CountLoginFailuresByIPSubnet(ipPrefix string, since time.Time) (int64, error)
	CountLoginFailuresByUsername(username string, since time.Time) (int64, error)
	LastSuccessfulLogin(username string) (*model.LoginAttempt, error)
}

// CaptchaService verifies short-lived math-challenge solutions. The
// auth handler does not care about the storage backing; we just want
// to know "is this (id, answer) pair valid?". Keeping it as a tiny
// interface avoids a circular dep with the api package.
type CaptchaService interface {
	Required(username, ip string) bool
	Verify(id, answer string) bool
}

// Notifier is the optional async hook fired at security-sensitive
// events (lockouts, account state changes). Pass nil to disable.
type Notifier interface {
	Notify(title, body, tag string)
}

// Authenticator bundles login and middleware logic for a specific AuthMode.
// It is constructed once during server bootstrap and reused across requests.
type Authenticator struct {
	cfg     *config.Config
	rt      *runtime.Settings
	secret  []byte
	users   UserRepo
	captcha CaptchaService
	notify  Notifier

	// systemAdminID / systemAdminUsername identify the implicit actor for
	// ipwhitelist / none modes. They are populated after the store seeds
	// the admin account during bootstrap.
	systemAdminID       uint
	systemAdminUsername string
}

// Context keys used to propagate the authenticated principal to handlers.
const (
	ctxKeyUserID   = "pp_user_id"
	ctxKeyUsername = "pp_username"
	ctxKeyRole     = "pp_role"
)

// Login failure reason codes. They live in the DB verbatim (short, stable
// machine-readable strings) so the UI and audit log consumers can filter
// on them without parsing free-form English text.
const (
	ReasonOK             = "ok"
	ReasonBadRequest     = "bad_request"
	ReasonInvalidCreds   = "invalid_credentials"
	ReasonUserDisabled   = "user_disabled"
	ReasonAuthDisabled   = "auth_disabled"
	ReasonLockedIP       = "locked_ip"
	ReasonLockedSubnet   = "locked_subnet"
	ReasonLockedUser     = "locked_user"
	ReasonCaptchaMissing = "captcha_required"
	ReasonCaptchaWrong   = "captcha_wrong"
	ReasonInternal       = "internal"
)

// New initialises an Authenticator. When no JWT secret is configured we
// derive a random one so tokens from previous processes are invalidated
// (acceptable for a single-admin self-hosted tool). Pass rt = nil to fall
// back to env defaults from cfg (used by tests that don't exercise hot
// reload); production callers should always supply a *runtime.Settings.
func New(cfg *config.Config, rt *runtime.Settings, users UserRepo) *Authenticator {
	secret := []byte(cfg.JWTSecret)
	if len(secret) == 0 {
		secret = randomSecret(32)
	}
	if rt == nil {
		rt = runtime.New(cfg)
	}
	return &Authenticator{cfg: cfg, rt: rt, secret: secret, users: users}
}

// SetCaptcha wires the optional captcha challenger. nil disables.
func (a *Authenticator) SetCaptcha(svc CaptchaService) { a.captcha = svc }

// SetNotifier wires the optional async push notifier. nil disables.
func (a *Authenticator) SetNotifier(n Notifier) { a.notify = n }

// SetSystemAdmin registers the built-in admin identity that non-password
// modes should impersonate. Call once at bootstrap after SeedAdminIfEmpty.
func (a *Authenticator) SetSystemAdmin(id uint, username string) {
	a.systemAdminID = id
	a.systemAdminUsername = username
}

// Middleware is the shared gate used by /api/* (login/status endpoints are
// mounted before this middleware so they remain reachable). Behaviour per
// mode:
//
//   - password     : requires a valid JWT whose user is still active.
//   - ipwhitelist  : requires the resolved client IP to be in the whitelist;
//                    the request is executed as the built-in system admin.
//   - none         : always allow; executed as the system admin.
func (a *Authenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch a.cfg.AuthMode {
		case config.AuthModePassword:
			ok, u := a.checkJWT(c)
			if !ok {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorised"})
				return
			}
			a.setPrincipal(c, u.ID, u.Username, u.Role)
		case config.AuthModeIPWhitelist:
			if !a.checkIP(c) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "ip not permitted"})
				return
			}
			a.setPrincipal(c, a.systemAdminID, a.systemAdminUsername, model.RoleAdmin)
		case config.AuthModeNone:
			a.setPrincipal(c, a.systemAdminID, a.systemAdminUsername, model.RoleAdmin)
		}
		c.Next()
	}
}

func (a *Authenticator) setPrincipal(c *gin.Context, id uint, username, role string) {
	c.Set(ctxKeyUserID, id)
	c.Set(ctxKeyUsername, username)
	c.Set(ctxKeyRole, role)
}

// Principal reads the authenticated user attached by the middleware. It
// returns zero values when the context has not passed through the gate.
func Principal(c *gin.Context) (id uint, username, role string) {
	if v, ok := c.Get(ctxKeyUserID); ok {
		id, _ = v.(uint)
	}
	if v, ok := c.Get(ctxKeyUsername); ok {
		username, _ = v.(string)
	}
	if v, ok := c.Get(ctxKeyRole); ok {
		role, _ = v.(string)
	}
	return
}

// recordAttempt writes one login_attempts row and best-effort swallows
// the error: a DB write failure must not prevent the auth response.
func (a *Authenticator) recordAttempt(username, ip, userAgent, reason string, success bool) {
	_ = a.users.RecordLoginAttempt(&model.LoginAttempt{
		Username:  username,
		ClientIP:  ip,
		Success:   success,
		Reason:    reason,
		UserAgent: truncate(userAgent, 255),
	})
}

// penaltyDelay returns the per-attempt slowdown we inject on failures. It
// is driven by the running failure count for this username so normal
// users feel no delay on a single typo, while scripted attackers stall:
//   count=1 -> 100ms, 2 -> 200ms, 3 -> 400ms, ... capped at 5s.
// We apply it AFTER recording the failure so the counter is already up
// to date when the next attempt is evaluated.
func penaltyDelay(count int) time.Duration {
	if count <= 0 {
		return 0
	}
	ms := 100 * math.Pow(2, float64(count-1))
	if ms > 5000 {
		ms = 5000
	}
	return time.Duration(ms) * time.Millisecond
}

// LoginHandler authenticates against the users table (bcrypt) and returns
// a signed JWT. Password auth is the only mode that exposes this endpoint.
//
// Brute-force defence: every attempt is recorded in login_attempts. Before
// checking the password we reject requests whose client IP, IP subnet, or
// submitted username has already crossed the configured failure threshold
// within the rolling window, answering 429 with a retry_after hint. Once
// the failure count for a target reaches the captcha threshold the
// response also requires a math captcha. Failed attempts additionally
// get an exponential delay so scripted attackers see their RPS collapse.
// All counters are persisted, so a server restart does not wipe the
// attacker's state.
func (a *Authenticator) LoginHandler(c *gin.Context) {
	var req struct {
		Username       string `json:"username"`
		Password       string `json:"password" binding:"required"`
		CaptchaID      string `json:"captcha_id"`
		CaptchaAnswer  string `json:"captcha_answer"`
	}
	clientIP := netutil.ClientIP(c.Request, a.cfg.TrustedProxies)
	userAgent := c.GetHeader("User-Agent")

	if err := c.ShouldBindJSON(&req); err != nil {
		a.recordAttempt("", clientIP, userAgent, ReasonBadRequest, false)
		// `code` is a stable machine-readable discriminator the frontend
		// maps to localised user-facing strings; `error` is the English
		// fallback kept for non-browser clients and log scrapers.
		c.JSON(http.StatusBadRequest, gin.H{"code": ReasonBadRequest, "error": err.Error()})
		return
	}
	if a.cfg.AuthMode != config.AuthModePassword {
		c.JSON(http.StatusBadRequest, gin.H{"code": ReasonAuthDisabled, "error": "password auth disabled"})
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		// Legacy single-user clients may still send only {password}; in
		// that case we default to the configured seed admin username so
		// the existing login flow keeps working.
		username = a.cfg.AdminUsername
		if username == "" {
			username = "admin"
		}
	}

	// --- 1. IP lockout check -------------------------------------------------
	if a.rt.LoginFailMaxPerIP() > 0 && a.rt.LoginFailWindowIPMin() > 0 {
		since := time.Now().Add(-a.rt.LoginIPWindow())
		n, err := a.users.CountLoginFailuresByIP(clientIP, since)
		if err == nil && int(n) >= a.rt.LoginFailMaxPerIP() {
			retry := a.rt.LoginLockoutIPMin()
			if retry <= 0 {
				retry = a.rt.LoginFailWindowIPMin()
			}
			a.recordAttempt(username, clientIP, userAgent, ReasonLockedIP, false)
			a.fire(ReasonLockedIP, username, clientIP)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":             ReasonLockedIP,
				"error":            "too many failed attempts from this address",
				"retry_after_secs": retry * 60,
			})
			return
		}
	}

	// --- 1b. Subnet lockout (optional, off by default) ----------------------
	if bits := a.rt.LoginFailSubnetBits(); bits > 0 && a.rt.LoginFailMaxPerIP() > 0 && a.rt.LoginFailWindowIPMin() > 0 {
		if prefix, ok := subnetPrefix(clientIP, bits); ok {
			since := time.Now().Add(-a.rt.LoginIPWindow())
			n, err := a.users.CountLoginFailuresByIPSubnet(prefix, since)
			// Subnet threshold is intentionally 3x the per-IP one to
			// make the aggregate gate strictly looser than the per-IP
			// gate; otherwise opening it would break NAT users on a
			// single typo from a colleague.
			if err == nil && int(n) >= a.rt.LoginFailMaxPerIP()*3 {
				retry := a.rt.LoginLockoutIPMin()
				if retry <= 0 {
					retry = a.rt.LoginFailWindowIPMin()
				}
				a.recordAttempt(username, clientIP, userAgent, ReasonLockedSubnet, false)
				a.fire(ReasonLockedSubnet, username, prefix)
				c.JSON(http.StatusTooManyRequests, gin.H{
					"code":             ReasonLockedSubnet,
					"error":            "too many failed attempts from this network",
					"retry_after_secs": retry * 60,
				})
				return
			}
		}
	}

	// --- 2. Username lockout check ------------------------------------------
	if a.rt.LoginFailMaxPerUser() > 0 && a.rt.LoginFailWindowUserMin() > 0 {
		since := time.Now().Add(-a.rt.LoginUserWindow())
		n, err := a.users.CountLoginFailuresByUsername(username, since)
		if err == nil && int(n) >= a.rt.LoginFailMaxPerUser() {
			retry := a.rt.LoginLockoutUserMin()
			if retry <= 0 {
				retry = a.rt.LoginFailWindowUserMin()
			}
			a.recordAttempt(username, clientIP, userAgent, ReasonLockedUser, false)
			a.fire(ReasonLockedUser, username, clientIP)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":             ReasonLockedUser,
				"error":            "too many failed attempts for this account",
				"retry_after_secs": retry * 60,
			})
			return
		}
	}

	// --- 2b. Captcha gate ---------------------------------------------------
	// Once the user/IP has crossed the captcha threshold, the request
	// must carry a valid (id, answer) pair. We respond 401 with a
	// dedicated code so the frontend knows to render the math input.
	if a.captcha != nil && a.captcha.Required(username, clientIP) {
		if strings.TrimSpace(req.CaptchaID) == "" || strings.TrimSpace(req.CaptchaAnswer) == "" {
			a.recordAttempt(username, clientIP, userAgent, ReasonCaptchaMissing, false)
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":             ReasonCaptchaMissing,
				"error":            "captcha required",
				"captcha_required": true,
			})
			return
		}
		if !a.captcha.Verify(req.CaptchaID, req.CaptchaAnswer) {
			a.recordAttempt(username, clientIP, userAgent, ReasonCaptchaWrong, false)
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":             ReasonCaptchaWrong,
				"error":            "captcha incorrect",
				"captcha_required": true,
			})
			return
		}
	}

	// --- 3. Credential verification -----------------------------------------
	u, err := a.users.GetUserByUsername(username)
	if err != nil {
		a.recordAttempt(username, clientIP, userAgent, ReasonInternal, false)
		c.JSON(http.StatusInternalServerError, gin.H{"code": ReasonInternal, "error": err.Error()})
		return
	}
	verified := false
	disabledHit := false
	if u == nil || u.Disabled || u.PasswordHash == "" {
		// Do a dummy bcrypt compare so the response timing does not leak
		// whether the username exists.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvaOZQZQ0ZQZQZQZQZQZQZQZQZQZQZQZQO"), []byte(req.Password))
		disabledHit = u != nil && u.Disabled
	} else if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err == nil {
		verified = true
	}

	if !verified {
		reason := ReasonInvalidCreds
		if disabledHit {
			reason = ReasonUserDisabled
		}
		a.recordAttempt(username, clientIP, userAgent, reason, false)
		// Running failure count for this username drives the delay.
		if a.rt.LoginFailWindowUserMin() > 0 {
			since := time.Now().Add(-a.rt.LoginUserWindow())
			if n, err := a.users.CountLoginFailuresByUsername(username, since); err == nil {
				time.Sleep(penaltyDelay(int(n)))
			}
		}
		// Tell the frontend whether the next attempt will need a captcha
		// so it can render the math input proactively.
		needsCaptcha := a.captcha != nil && a.captcha.Required(username, clientIP)
		resp := gin.H{"code": reason, "error": "invalid credentials"}
		if needsCaptcha {
			resp["captcha_required"] = true
		}
		c.JSON(http.StatusUnauthorized, resp)
		return
	}

	// --- 4. Success ----------------------------------------------------------
	tok, err := a.sign(u)
	if err != nil {
		a.recordAttempt(username, clientIP, userAgent, ReasonInternal, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Fetch the previous successful login BEFORE recording this one, so the
	// "last login" banner shows the session the user cares about (the one
	// before this one) rather than the attempt they just completed.
	prev, _ := a.users.LastSuccessfulLogin(username)
	a.recordAttempt(username, clientIP, userAgent, ReasonOK, true)

	resp := gin.H{
		"token":    tok,
		"username": u.Username,
		"role":     u.Role,
	}
	if prev != nil {
		resp["last_login"] = gin.H{
			"at":         prev.CreatedAt,
			"client_ip":  prev.ClientIP,
			"user_agent": prev.UserAgent,
		}
	}
	c.JSON(http.StatusOK, resp)
}

// fire is a tiny shim around the optional Notifier so callers don't
// need to nil-check on every event. Tag is used as ntfy's "Tags"
// header to colour the icon in the mobile client.
func (a *Authenticator) fire(reason, username, where string) {
	if a.notify == nil {
		return
	}
	switch reason {
	case ReasonLockedIP:
		a.notify.Notify("FrpDeck · IP 已被临时锁定",
			fmt.Sprintf("IP %s 触发暴力登录阈值，账号目标=%s", where, username), "warning")
	case ReasonLockedSubnet:
		a.notify.Notify("FrpDeck · 网段已被临时锁定",
			fmt.Sprintf("子网 %s 累计失败次数过多，目标=%s", where, username), "warning")
	case ReasonLockedUser:
		a.notify.Notify("FrpDeck · 账号已被临时锁定",
			fmt.Sprintf("用户 %s 失败次数过多 (来源 %s)", username, where), "lock")
	}
}

// subnetPrefix derives the canonical "ip/bits" prefix the store uses
// to aggregate failures. Returns ok=false when ip is unparseable; the
// auth handler then quietly skips the subnet check rather than block
// a real user on a malformed forwarded header.
func subnetPrefix(ip string, bits int) (string, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil || bits <= 0 {
		return "", false
	}
	if v4 := parsed.To4(); v4 != nil {
		if bits > 32 {
			bits = 32
		}
		mask := net.CIDRMask(bits, 32)
		return (&net.IPNet{IP: v4.Mask(mask), Mask: mask}).String(), true
	}
	if bits > 128 {
		bits = 128
	}
	mask := net.CIDRMask(bits, 128)
	return (&net.IPNet{IP: parsed.Mask(mask), Mask: mask}).String(), true
}

// StatusHandler exposes enough metadata for the frontend router to decide
// whether to redirect to /login.
func (a *Authenticator) StatusHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"mode":     string(a.cfg.AuthMode),
		"required": a.cfg.AuthMode != config.AuthModeNone,
	})
}

// sign issues a JWT with a 24h expiry, including the authenticated user's
// id / username / role so downstream handlers can authorise actions.
func (a *Authenticator) sign(u *model.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":      u.Username,
		"uid":      u.ID,
		"role":     u.Role,
		"username": u.Username,
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

// IssueAccessToken mints a standard 24h JWT for the given user using
// the same signing path as LoginHandler. The Wails desktop entry
// point uses this to produce a self-issued token so its tray menu can
// hit the local HTTP API without a roundtrip through /api/auth/login.
//
// Callers are responsible for refreshing before expiry — the desktop
// loop currently re-mints once a day.
func (a *Authenticator) IssueAccessToken(u *model.User) (string, error) {
	return a.sign(u)
}

// checkJWT validates the bearer token and returns the active user behind
// it. An account that has since been disabled or deleted rejects the
// request even if the token has not yet expired.
func (a *Authenticator) checkJWT(c *gin.Context) (bool, *model.User) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false, nil
	}
	raw := strings.TrimPrefix(header, "Bearer ")
	return a.ValidateRawToken(raw)
}

// ValidateRawToken is the entry point used by transports that cannot
// pass an Authorization header — currently the WebSocket upgrader, which
// receives the JWT through the `Sec-WebSocket-Protocol` subprotocol
// instead of an HTTP header. Returns (true, user) iff the token is well
// formed, signed by us, not expired, and the referenced account is
// enabled.
func (a *Authenticator) ValidateRawToken(raw string) (bool, *model.User) {
	if raw == "" {
		return false, nil
	}
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return a.secret, nil
	})
	if err != nil || !tok.Valid {
		return false, nil
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return false, nil
	}
	uidFloat, _ := claims["uid"].(float64)
	uid := uint(uidFloat)
	if uid == 0 {
		return false, nil
	}
	u, err := a.users.GetUserByID(uid)
	if err != nil || u == nil || u.Disabled {
		return false, nil
	}
	return true, u
}

func (a *Authenticator) checkIP(c *gin.Context) bool {
	ip := netutil.ClientIP(c.Request, a.cfg.TrustedProxies)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range a.cfg.AdminIPWhitelist {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func randomSecret(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	out := make([]byte, hex.EncodedLen(n))
	hex.Encode(out, buf)
	return out
}

// truncate clamps a string at max UTF-8 bytes; used to keep User-Agent
// values within the DB column width without panicking on multibyte input.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
