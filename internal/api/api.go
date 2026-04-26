// Package api wires the HTTP routes that drive the FrpDeck UI.
//
// P0 ships skeleton handlers for endpoints / tunnels that respond with
// the persisted shape only — no driver interaction yet. P1 plugs the
// frpcd driver in and the same handlers begin pushing real proxies to
// the live frps server.
package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/captcha"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/diag"
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/lifecycle"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/netutil"
	"github.com/teacat99/FrpDeck/internal/notify"
	"github.com/teacat99/FrpDeck/internal/runtime"
	"github.com/teacat99/FrpDeck/internal/store"
)

// Server wires the HTTP router with its dependencies. Constructing Server
// from main keeps the API package free of global state and simplifies tests.
type Server struct {
	cfg       *config.Config
	rt        *runtime.Settings
	store     *store.Store
	lifecycle *lifecycle.Manager
	driver    frpcd.FrpDriver
	auth      *auth.Authenticator
	captcha   *captcha.Service
	notify    *notify.Ntfy
	limiter   *ipRateLimiter
}

// New builds a Server with all collaborators supplied. Callers must not
// pass nil pointers (except captcha / notify, which are optional features).
func New(
	cfg *config.Config,
	rt *runtime.Settings,
	s *store.Store,
	lm *lifecycle.Manager,
	drv frpcd.FrpDriver,
	a *auth.Authenticator,
	cs *captcha.Service,
	nt *notify.Ntfy,
) *Server {
	limiter := newIPRateLimiter(rt.RateLimitPerMinutePerIP(), time.Minute)
	rt.AddHook(runtime.KeyRateLimitPerMinutePerIP, func() {
		limiter.SetMax(rt.RateLimitPerMinutePerIP())
	})
	return &Server{
		cfg:       cfg,
		rt:        rt,
		store:     s,
		lifecycle: lm,
		driver:    drv,
		auth:      a,
		captcha:   cs,
		notify:    nt,
		limiter:   limiter,
	}
}

// Router mounts the /api/* tree on a gin.Engine. Authentication is
// enforced by the auth middleware; /auth/* endpoints are mounted before
// the gate so unauthenticated clients can still log in.
func (s *Server) Router(engine *gin.Engine) {
	pub := engine.Group("/api")
	pub.GET("/auth/status", s.auth.StatusHandler)
	pub.POST("/auth/login", s.auth.LoginHandler)
	pub.GET("/auth/captcha", s.handleIssueCaptcha)
	pub.GET("/version", s.handleVersion)
	// WebSocket sits on the public group: the gin auth middleware
	// only knows how to validate Authorization headers, but browsers
	// cannot set those on a WS handshake, so the WS handler does its
	// own JWT check via the Sec-WebSocket-Protocol subprotocol.
	pub.GET("/ws", s.handleWebSocket)

	g := engine.Group("/api", s.auth.Middleware())

	g.GET("/health", s.handleHealth)
	g.GET("/client-ip", s.handleClientIP)

	// Identity & self-service.
	g.GET("/auth/me", s.handleMe)
	g.POST("/auth/password", s.handleChangeOwnPassword)
	g.GET("/auth/my-recent-logins", s.handleMyLoginHistory)
	g.GET("/auth/login-history", s.handleLoginHistory)

	// Endpoints (frps servers).
	g.GET("/endpoints", s.handleListEndpoints)
	g.POST("/endpoints", s.handleCreateEndpoint)
	g.GET("/endpoints/:id", s.handleGetEndpoint)
	g.PUT("/endpoints/:id", s.handleUpdateEndpoint)
	g.DELETE("/endpoints/:id", s.handleDeleteEndpoint)

	// Tunnels (frp proxies / visitors).
	g.GET("/tunnels", s.handleListTunnels)
	g.POST("/tunnels", s.handleCreateTunnel)
	g.GET("/tunnels/:id", s.handleGetTunnel)
	g.PUT("/tunnels/:id", s.handleUpdateTunnel)
	g.DELETE("/tunnels/:id", s.handleDeleteTunnel)
	g.POST("/tunnels/:id/start", s.handleStartTunnel)
	g.POST("/tunnels/:id/stop", s.handleStopTunnel)
	g.POST("/tunnels/:id/renew", s.handleRenewTunnel)
	g.POST("/tunnels/:id/diagnose", s.handleDiagnoseTunnel)

	// User management endpoints (admin-only is enforced inside the
	// handler via ensureAdmin so the auth layer can keep a single gate).
	g.GET("/users", s.handleListUsers)
	g.POST("/users", s.handleCreateUser)
	g.PUT("/users/:id", s.handleUpdateUser)
	g.POST("/users/:id/password", s.handleResetUserPassword)
	g.DELETE("/users/:id", s.handleDeleteUser)

	g.GET("/settings", s.handleGetSettings)
	g.PUT("/settings", s.handlePutSettings)

	// Audit log (admin-only). Frontend HistoryView reads from here; the
	// backend keeps writing entries via store.WriteAudit on every mutation.
	g.GET("/audit", s.handleListAudit)

	// Runtime tunables: typed view of the hot-mutable subset of config.
	g.GET("/runtime-settings", s.handleGetRuntimeSettings)
	g.PUT("/runtime-settings", s.handlePutRuntimeSettings)

	// Ntfy push: synchronous test hook so the operator can validate URL.
	g.POST("/notify/test", s.handleTestNotify)
}

// clientIP is the single choke-point for extracting the trusted client IP.
func (s *Server) clientIP(c *gin.Context) string {
	return netutil.ClientIP(c.Request, s.cfg.TrustedProxies)
}

// ------------------------- meta handlers -------------------------

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now().UTC()})
}

func (s *Server) handleClientIP(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ip": s.clientIP(c)})
}

func (s *Server) handleVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"frp_version": frpcd.BundledFrpVersion,
		"driver":      s.driver.Name(),
	})
}

func (s *Server) handleIssueCaptcha(c *gin.Context) {
	if s.captcha == nil {
		c.JSON(http.StatusOK, gin.H{"id": "", "question": ""})
		return
	}
	id, q := s.captcha.Issue()
	c.JSON(http.StatusOK, gin.H{"id": id, "question": q})
}

func (s *Server) handleTestNotify(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	if s.notify == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notify subsystem not initialised"})
		return
	}
	if err := s.notify.Test(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ------------------------- endpoints -------------------------

func (s *Server) handleListEndpoints(c *gin.Context) {
	rows, err := s.store.ListEndpoints()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"endpoints": rows})
}

func (s *Server) handleGetEndpoint(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	e, err := s.store.GetEndpoint(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if e == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
		return
	}
	c.JSON(http.StatusOK, e)
}

func (s *Server) handleCreateEndpoint(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var req endpointReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var e model.Endpoint
	req.applyToEndpoint(&e, nil)
	if err := s.store.CreateEndpoint(&e); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "create_endpoint", Actor: actor, ActorIP: s.clientIP(c),
		Detail: e.Name,
	})
	c.JSON(http.StatusOK, e)
}

func (s *Server) handleUpdateEndpoint(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := s.store.GetEndpoint(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
		return
	}
	var req endpointReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	patch := *existing
	req.applyToEndpoint(&patch, existing)
	if err := s.store.UpdateEndpoint(&patch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "update_endpoint", Actor: actor, ActorIP: s.clientIP(c),
		Detail: patch.Name,
	})
	c.JSON(http.StatusOK, patch)
}

func (s *Server) handleDeleteEndpoint(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if ep, err := s.store.GetEndpoint(id); err == nil && ep != nil {
		_ = s.driver.Stop(c.Request.Context(), ep)
	}
	if err := s.store.DeleteEndpoint(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "delete_endpoint", Actor: actor, ActorIP: s.clientIP(c),
	})
	c.Status(http.StatusNoContent)
}

// ------------------------- tunnels -------------------------

func (s *Server) handleListTunnels(c *gin.Context) {
	if epStr := c.Query("endpoint_id"); epStr != "" {
		epID, err := parseID(epStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		rows, err := s.store.ListTunnelsByEndpoint(epID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tunnels": rows})
		return
	}
	rows, err := s.store.ListTunnels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tunnels": rows})
}

func (s *Server) handleGetTunnel(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := s.store.GetTunnel(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}
	c.JSON(http.StatusOK, t)
}

func (s *Server) handleCreateTunnel(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var req tunnelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if ep, err := s.store.GetEndpoint(req.EndpointID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	} else if ep == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint not found"})
		return
	}
	var t model.Tunnel
	req.applyToTunnel(&t, nil)
	t.Status = model.StatusPending
	t.Source = model.TunnelSourceManual
	uid, _, _ := auth.Principal(c)
	t.CreatedBy = uid
	if err := s.store.CreateTunnel(&t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.lifecycle.Schedule(&t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "create_tunnel", TunnelID: t.ID, Actor: actor, ActorIP: s.clientIP(c),
		Detail: t.Name,
	})
	c.JSON(http.StatusOK, t)
}

func (s *Server) handleUpdateTunnel(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := s.store.GetTunnel(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}
	var req tunnelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	patch := *existing
	req.applyToTunnel(&patch, existing)
	if err := s.store.UpdateTunnel(&patch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Reschedule the lifecycle in case ExpireAt or Enabled flipped.
	if err := s.lifecycle.Schedule(&patch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// If the tunnel is currently active, push the refreshed config to the
	// live engine so the operator does not have to manually stop/start.
	if patch.Status == model.StatusActive {
		if err := s.pushTunnelToDriver(&patch); err != nil {
			// Persisted change stays; surface the driver error so the UI
			// can flag the row as needing a restart.
			c.JSON(http.StatusOK, gin.H{"tunnel": patch, "driver_warning": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, patch)
}

func (s *Server) handleDeleteTunnel(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if t, err := s.store.GetTunnel(id); err == nil && t != nil {
		_ = s.removeTunnelFromDriver(t)
	}
	if err := s.store.DeleteTunnel(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// handleStartTunnel marks the tunnel active, registers it with the live
// frp engine via the driver, and arms its expiry timer.
func (s *Server) handleStartTunnel(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := s.store.GetTunnel(id)
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}
	if err := s.pushTunnelToDriver(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	t.Status = model.StatusActive
	t.LastStartAt = &now
	t.LastError = ""
	if err := s.store.UpdateTunnel(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.lifecycle.Schedule(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

func (s *Server) handleStopTunnel(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := s.store.GetTunnel(id)
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}
	_ = s.removeTunnelFromDriver(t)
	if err := s.lifecycle.StopTunnel(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// handleRenewTunnel extends a temporary tunnel's expiry by
// `extend_seconds`, or makes it permanent when `extend_seconds == 0`.
// Reactivates `expired` rows in either case so operators get a one-click
// recovery path from the Tunnels page. Permanent tunnels (ExpireAt nil)
// are rejected with 400 to prevent accidental conversions — the regular
// PUT /tunnels/:id is the right path for that.
func (s *Server) handleRenewTunnel(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req tunnelRenewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	delta := time.Duration(*req.ExtendSeconds) * time.Second
	t, err := s.lifecycle.Renew(id, delta)
	if err != nil {
		switch {
		case errors.Is(err, lifecycle.ErrTunnelNoExpire):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "renew_tunnel", TunnelID: t.ID, Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf(`{"extend_seconds":%d}`, *req.ExtendSeconds),
	})
	c.JSON(http.StatusOK, t)
}

// handleDiagnoseTunnel runs the four-step connectivity self-check
// (DNS / TCP probe / frps register / local reach) against the tunnel
// and returns a structured Report. The route is admin-only because
// the probe touches the configured frps host on every call and we
// don't want anonymous traffic generating outbound dials.
//
// The result is informational: we deliberately do not flip tunnel
// state on diag failure (the user might still want to save and try
// later). The frontend renders the four checks as a step-by-step
// list in the tunnel detail panel.
func (s *Server) handleDiagnoseTunnel(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := s.store.GetTunnel(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}
	ep, err := s.store.GetEndpoint(t.EndpointID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if ep == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "endpoint not found"})
		return
	}
	rep := diag.NewRunner(s.driver).Run(c.Request.Context(), ep, t)
	c.JSON(http.StatusOK, rep)
}

// pushTunnelToDriver looks up the endpoint and registers/refreshes the
// tunnel on the running frp engine. Returns an error if the endpoint is
// missing or disabled — the caller should not flip the persisted status
// to "active" in that case.
func (s *Server) pushTunnelToDriver(t *model.Tunnel) error {
	ep, err := s.store.GetEndpoint(t.EndpointID)
	if err != nil {
		return err
	}
	if ep == nil {
		return errors.New("endpoint not found")
	}
	if !ep.Enabled {
		return errors.New("endpoint disabled")
	}
	return s.driver.AddTunnel(ep, t)
}

// removeTunnelFromDriver unregisters a tunnel from its endpoint runner.
// Best-effort: a missing endpoint or driver error is swallowed because
// the persisted state is already moving to "stopped".
func (s *Server) removeTunnelFromDriver(t *model.Tunnel) error {
	ep, err := s.store.GetEndpoint(t.EndpointID)
	if err != nil || ep == nil {
		return nil
	}
	return s.driver.RemoveTunnel(ep, t)
}

// ------------------------- settings -------------------------

func (s *Server) handleGetSettings(c *gin.Context) {
	rows, err := s.store.ListSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := gin.H{
		"auth_mode":              string(s.cfg.AuthMode),
		"max_duration_hours":     s.rt.MaxDurationHours(),
		"history_retention_days": s.rt.HistoryRetentionDays(),
		"trusted_proxies":        stringifyNets(s.cfg.TrustedProxies),
		"kv":                     rows,
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) handlePutSettings(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var kv map[string]string
	if err := c.ShouldBindJSON(&kv); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for k, v := range kv {
		if err := s.store.SetSetting(k, v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.Status(http.StatusOK)
}

func (s *Server) handleGetRuntimeSettings(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	system := gin.H{
		"listen":          s.cfg.Listen,
		"data_dir":        s.cfg.DataDir,
		"driver":          s.driver.Name(),
		"frp_version":     frpcd.BundledFrpVersion,
		"auth_mode":       string(s.cfg.AuthMode),
		"jwt_secret_set":  s.cfg.JWTSecret != "",
		"trusted_proxies": stringifyNets(s.cfg.TrustedProxies),
	}
	c.JSON(http.StatusOK, gin.H{
		"settings": s.rt.Snapshot(),
		"system":   system,
	})
}

func (s *Server) handlePutRuntimeSettings(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var raw map[string]string
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := make(map[runtime.Key]string, len(raw))
	for k, v := range raw {
		updates[runtime.Key(k)] = v
	}
	if err := s.rt.SetMany(updates, s.store.SetSetting); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": s.rt.Snapshot()})
}

// handleListAudit serves the recent audit log to admins. Filtering by date
// range or actor IP is supported via query params; the default response is
// the most recent 200 entries.
func (s *Server) handleListAudit(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	filter := store.AuditFilter{
		IP:     c.Query("ip"),
		Limit:  parseIntDefault(c.Query("limit"), 200),
		Offset: parseIntDefault(c.Query("offset"), 0),
	}
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.From = t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.To = t
		}
	}
	rows, total, err := s.store.ListAudit(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": rows, "total": total})
}

// ------------------------- helpers -------------------------

func parseID(s string) (uint, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, errors.New("invalid id")
	}
	return uint(n), nil
}

func parseIntDefault(s string, d int) int {
	if s == "" {
		return d
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return d
	}
	return n
}

func stringifyNets(nets []*net.IPNet) []string {
	out := make([]string, 0, len(nets))
	for _, n := range nets {
		out = append(out, n.String())
	}
	return out
}
