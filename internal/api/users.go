package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/model"
)

// minPasswordLen is the floor length for user-supplied passwords. The
// actual limit used by the handlers comes from cfg.LoginMinPasswordLen,
// with this const as the safety default when config was not wired (tests).
const minPasswordLen = 8

// passwordPolicyError is returned from validatePassword when a password
// does not meet the policy. The string is machine-readable so the UI
// can localise it; the English phrasing is the log / CLI fallback.
type passwordPolicyError struct {
	code string
	min  int
}

func (e *passwordPolicyError) Error() string {
	switch e.code {
	case "password_too_short":
		return fmt.Sprintf("password must be at least %d characters", e.min)
	case "password_missing_letter":
		return "password must contain at least one letter"
	case "password_missing_digit":
		return "password must contain at least one digit"
	}
	return "password does not meet policy"
}

// validatePassword enforces the policy: >= min length AND contains at
// least one ASCII letter AND one digit. We intentionally do NOT require
// special characters - empirical data shows complexity requirements push
// users toward predictable substitutions without adding real entropy.
func validatePassword(pw string, min int) error {
	if min <= 0 {
		min = minPasswordLen
	}
	if len(pw) < min {
		return &passwordPolicyError{code: "password_too_short", min: min}
	}
	hasLetter, hasDigit := false, false
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
		if hasLetter && hasDigit {
			break
		}
	}
	if !hasLetter {
		return &passwordPolicyError{code: "password_missing_letter", min: min}
	}
	if !hasDigit {
		return &passwordPolicyError{code: "password_missing_digit", min: min}
	}
	return nil
}

// respondPasswordPolicy wires a passwordPolicyError (or generic error)
// into the standard {code, error} JSON envelope so the frontend can show
// a localised message.
func respondPasswordPolicy(c *gin.Context, err error) {
	if ppe, ok := err.(*passwordPolicyError); ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    ppe.code,
			"error":   ppe.Error(),
			"min_len": ppe.min,
		})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

// ensureAdmin aborts the request with 403 when the current principal is
// not an admin. Returns true on success so the caller can continue.
func (s *Server) ensureAdmin(c *gin.Context) bool {
	_, _, role := auth.Principal(c)
	if role != model.RoleAdmin {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return false
	}
	return true
}

// minPasswordLen reads the live runtime value when configured, falling
// back to the env-loaded cfg, then to the package-level safety floor.
// Centralising here means new callers cannot forget the precedence rule.
func (s *Server) minPasswordLen() int {
	if s.rt != nil {
		if n := s.rt.LoginMinPasswordLen(); n > 0 {
			return n
		}
	}
	if s.cfg != nil && s.cfg.LoginMinPasswordLen > 0 {
		return s.cfg.LoginMinPasswordLen
	}
	return minPasswordLen
}

// currentUserID pulls the authenticated user id from the context. Used
// when scoping list/create operations to the caller.
func currentUserID(c *gin.Context) uint {
	id, _, _ := auth.Principal(c)
	return id
}

// ------------------------- /api/auth/me /password -------------------------

func (s *Server) handleMe(c *gin.Context) {
	id, name, role := auth.Principal(c)
	c.JSON(http.StatusOK, gin.H{
		"id":       id,
		"username": name,
		"role":     role,
		"auth_mode": string(s.cfg.AuthMode),
	})
}

type changePwdReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password" binding:"required"`
}

// handleChangeOwnPassword lets the signed-in user rotate their own
// password. In password auth mode we require the old password; other
// modes don't have real credentials so we skip that check.
func (s *Server) handleChangeOwnPassword(c *gin.Context) {
	var req changePwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePassword(req.NewPassword, s.minPasswordLen()); err != nil {
		respondPasswordPolicy(c, err)
		return
	}
	uid := currentUserID(c)
	u, err := s.store.GetUserByID(uid)
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}
	if u.PasswordHash != "" {
		// In password mode the old password must match. In ipwhitelist /
		// none modes the system admin account may have an empty hash (no
		// login path); we still allow an admin to set an initial one.
		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.OldPassword)); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "old password incorrect"})
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.store.SetUserPasswordHash(u.ID, string(hash)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "change_password", Actor: u.Username, ActorIP: s.clientIP(c),
		Detail: "self",
	})
	c.Status(http.StatusOK)
}

// ------------------------- /api/auth/login-history -------------------------

// handleMyLoginHistory returns the last N login attempts (success AND
// failure) for the signed-in user. Lets real users see "someone tried to
// log in to my account" and, combined with the "last login" field in the
// login response, gives them visibility into their own activity without
// exposing other accounts.
func (s *Server) handleMyLoginHistory(c *gin.Context) {
	_, name, _ := auth.Principal(c)
	if name == "" {
		c.JSON(http.StatusOK, gin.H{"attempts": []model.LoginAttempt{}})
		return
	}
	limit := parseLimit(c, 20, 100)
	rows, err := s.store.ListLoginAttempts(name, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"attempts": rows})
}

// handleLoginHistory is the admin-only, unscoped view of login attempts.
// Used by Settings > Security to audit brute-force activity or confirm
// an account wasn't compromised. Optional ?username= narrows the query.
func (s *Server) handleLoginHistory(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	limit := parseLimit(c, 50, 500)
	username := strings.TrimSpace(c.Query("username"))
	rows, err := s.store.ListLoginAttempts(username, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"attempts": rows})
}

// parseLimit extracts a bounded ?limit= from the request. Anything out of
// range (including unparseable) snaps back to the default.
func parseLimit(c *gin.Context, def, max int) int {
	raw := c.Query("limit")
	if raw == "" {
		return def
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
		if n > max {
			return max
		}
	}
	if n <= 0 {
		return def
	}
	return n
}

// ------------------------- /api/users (admin-only) -------------------------

type createUserReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role"`
}

func (s *Server) handleListUsers(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	us, err := s.store.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": us})
}

func (s *Server) handleCreateUser(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var req createUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}
	if err := validatePassword(req.Password, s.minPasswordLen()); err != nil {
		respondPasswordPolicy(c, err)
		return
	}
	role := normaliseRole(req.Role)
	if role == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be admin or user"})
		return
	}
	if existing, err := s.store.GetUserByUsername(username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	} else if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	u := &model.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
	}
	if err := s.store.CreateUser(u); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "create_user", Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("%s role=%s", username, role),
	})
	c.JSON(http.StatusOK, u)
}

type updateUserReq struct {
	Role     *string `json:"role,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
}

// handleUpdateUser patches role / disabled with the "at-least-one active
// admin" invariant and the "cannot modify self" rule enforced atomically
// before we touch the row.
func (s *Server) handleUpdateUser(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	selfID := currentUserID(c)
	var req updateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	target, err := s.store.GetUserByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if id == selfID && (req.Role != nil || req.Disabled != nil) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot modify role/disabled on self"})
		return
	}
	fields := map[string]any{}
	newRole := target.Role
	newDisabled := target.Disabled
	if req.Role != nil {
		role := normaliseRole(*req.Role)
		if role == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be admin or user"})
			return
		}
		newRole = role
		fields["role"] = role
	}
	if req.Disabled != nil {
		newDisabled = *req.Disabled
		fields["disabled"] = newDisabled
	}
	if len(fields) == 0 {
		c.JSON(http.StatusOK, target)
		return
	}
	// If this change reduces the set of active admins, block it when it
	// would leave the system without one.
	wasActiveAdmin := target.Role == model.RoleAdmin && !target.Disabled
	becomesInactiveAdmin := newRole != model.RoleAdmin || newDisabled
	if wasActiveAdmin && becomesInactiveAdmin {
		n, err := s.store.CountActiveAdmins()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if n <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "must keep at least one active admin"})
			return
		}
	}
	if err := s.store.UpdateUserFields(id, fields); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	updated, _ := s.store.GetUserByID(id)
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "update_user", Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("id=%d role=%s disabled=%v", id, newRole, newDisabled),
	})
	// Notify the operator when an account flips disabled state. We
	// only fire on the boundary so a no-op PUT (same values resent
	// from the UI) does not spam the channel.
	if s.notify != nil && req.Disabled != nil && newDisabled != target.Disabled {
		title := "FrpDeck · 账号已启用"
		tag := "white_check_mark"
		if newDisabled {
			title = "FrpDeck · 账号已禁用"
			tag = "no_entry_sign"
		}
		s.notify.Notify(title, fmt.Sprintf("用户 %s 由 %s 操作", target.Username, actor), tag)
	}
	c.JSON(http.StatusOK, updated)
}

type resetPwdReq struct {
	NewPassword string `json:"new_password" binding:"required"`
}

func (s *Server) handleResetUserPassword(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req resetPwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePassword(req.NewPassword, s.minPasswordLen()); err != nil {
		respondPasswordPolicy(c, err)
		return
	}
	target, err := s.store.GetUserByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.store.SetUserPasswordHash(id, string(hash)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "reset_password", Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("target=%s", target.Username),
	})
	c.Status(http.StatusOK)
}

// handleDeleteUser enforces the two invariants the spec calls out:
// (a) an admin cannot delete themselves; (b) the last active admin cannot
// be removed. Tunnels created by the deleted user are not auto-purged in
// P0 — the operator can re-assign or delete them from the tunnels page.
// (P1 follow-up: stop user-owned tunnels via the lifecycle manager so no
// frp proxies are left running on the frps server.)
func (s *Server) handleDeleteUser(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	selfID := currentUserID(c)
	if id == selfID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete self"})
		return
	}
	target, err := s.store.GetUserByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if target.Role == model.RoleAdmin && !target.Disabled {
		n, err := s.store.CountActiveAdmins()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if n <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "must keep at least one active admin"})
			return
		}
	}

	if err := s.store.DeleteUser(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "delete_user", Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("%s (id=%d)", target.Username, id),
	})
	c.Status(http.StatusNoContent)
}

// normaliseRole maps user input to the canonical role enum; returns an
// empty string when the value is unrecognised so the caller can 400.
func normaliseRole(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", model.RoleUser:
		return model.RoleUser
	case model.RoleAdmin:
		return model.RoleAdmin
	}
	return ""
}
