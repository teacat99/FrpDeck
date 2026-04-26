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
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/captcha"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/diag"
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/frpcimport"
	"github.com/teacat99/FrpDeck/internal/frpshelper"
	"github.com/teacat99/FrpDeck/internal/lifecycle"
	"github.com/teacat99/FrpDeck/internal/templates"
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
	// The mgmt-token exchange is the very first call B's browser issues
	// to A through the stcp tunnel, so it has to live on the public
	// group; the token in the body IS the credential.
	pub.POST("/auth/remote-redeem", s.handleRemoteRedeemToken)
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
	g.GET("/tunnels/templates", s.handleListTunnelTemplates)
	g.GET("/tunnels", s.handleListTunnels)
	g.POST("/tunnels", s.handleCreateTunnel)
	g.GET("/tunnels/:id", s.handleGetTunnel)
	g.PUT("/tunnels/:id", s.handleUpdateTunnel)
	g.DELETE("/tunnels/:id", s.handleDeleteTunnel)
	g.POST("/tunnels/:id/start", s.handleStartTunnel)
	g.POST("/tunnels/:id/stop", s.handleStopTunnel)
	g.POST("/tunnels/:id/renew", s.handleRenewTunnel)
	g.POST("/tunnels/:id/diagnose", s.handleDiagnoseTunnel)
	g.GET("/tunnels/:id/frps-advice", s.handleAdviseTunnelFrps)
	g.POST("/tunnels/import/preview", s.handleImportTunnelsPreview)
	g.POST("/tunnels/import/commit", s.handleImportTunnelsCommit)

	// Remote management (P5-A). Every route is also gated by
	// remoteAuthGuard inside the handler so the API surface stays inert
	// in non-password modes regardless of route mounting order.
	g.GET("/remote/nodes", s.handleListRemoteNodes)
	g.POST("/remote/invitations", s.handleCreateInvitation)
	g.POST("/remote/nodes/:id/refresh", s.handleRefreshInvitation)
	g.POST("/remote/redeem", s.handleRedeemInvitation)
	g.DELETE("/remote/nodes/:id", s.handleRevokeRemoteNode)

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

	// Subprocess driver helpers (P8-A): probe a frpc binary's version
	// and one-click download the bundled release into <data_dir>/bin.
	g.POST("/frpc/probe", s.handleProbeFrpc)
	g.POST("/frpc/download", s.handleDownloadFrpc)

	// Profiles (P8-C/D): named bundles of (Endpoint, Tunnel) toggles.
	s.registerProfileRoutes(g)
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
	probeWarn := ""
	if err := s.ensureSubprocessReady(&e); err != nil {
		// Persist the row regardless — we want to surface "binary not
		// usable" as a UI warning rather than a hard 5xx.
		probeWarn = err.Error()
	}
	if err := s.store.CreateEndpoint(&e); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "create_endpoint", Actor: actor, ActorIP: s.clientIP(c),
		Detail: e.Name,
	})
	if probeWarn != "" {
		c.JSON(http.StatusOK, gin.H{"endpoint": e, "subprocess_warning": probeWarn})
		return
	}
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
	probeWarn := ""
	// Re-probe only if the binary path changed or driver mode flipped to
	// subprocess; otherwise reuse the cached version to avoid spawning a
	// process on every save.
	if patch.DriverMode == model.DriverModeSubprocess &&
		(existing.DriverMode != model.DriverModeSubprocess ||
			existing.SubprocessPath != patch.SubprocessPath) {
		if err := s.ensureSubprocessReady(&patch); err != nil {
			probeWarn = err.Error()
		}
	} else {
		patch.SubprocessVersion = existing.SubprocessVersion
	}
	if err := s.store.UpdateEndpoint(&patch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "update_endpoint", Actor: actor, ActorIP: s.clientIP(c),
		Detail: patch.Name,
	})
	if probeWarn != "" {
		c.JSON(http.StatusOK, gin.H{"endpoint": patch, "subprocess_warning": probeWarn})
		return
	}
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

// handleAdviseTunnelFrps reverse-engineers the frps.toml requirements
// for a given tunnel and returns a structured Advice payload. plan.md
// §7.1 calls this the "frp 配置助手": instead of asking users to read
// the gofrp.org docs from scratch, FrpDeck enumerates exactly which
// frps knobs the current tunnel implies (vhost ports, allow lists,
// stun servers, etc.) and renders a copy-pasteable TOML snippet.
//
// Pure read; safe for any authenticated user (no driver side effects).
// We still gate behind ensureAdmin to keep the access pattern aligned
// with the rest of the tunnels mutation routes — viewers should not
// depend on this and the helper output may leak shape of the network.
func (s *Server) handleAdviseTunnelFrps(c *gin.Context) {
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
	c.JSON(http.StatusOK, frpshelper.Advise(ep, t))
}

// handleListTunnelTemplates returns the embedded scenario templates
// described in plan.md §7.2 ("场景模板 × 10"). The frontend renders
// these as a wizard: pick → defaults flow into the create-tunnel
// form. Read-only and tiny payload, so we don't paginate.
//
// Why GET on /tunnels/templates: piggybacks on the existing tunnels
// route group which already enforces auth + IP whitelist. The route
// is open to any authenticated user (not admin-only) because the
// data is the same compile-time YAML for everyone.
func (s *Server) handleListTunnelTemplates(c *gin.Context) {
	all, err := templates.All()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": all})
}

// importPreviewReq is the dry-run payload: filename is optional but
// helps the parser detect the source format and seed the suggested
// endpoint name. content is the raw frpc.toml/yaml/json text the user
// pasted or uploaded — kept as a string (not multipart) because the
// existing JSON middleware handles auth + IP whitelist uniformly.
//
// EndpointID is optional. When the operator already picked a target
// endpoint in the preview UI we cross-reference its existing tunnel
// names and stamp `Conflict: true` on any draft that would collide,
// so the UI can default the per-row resolution to rename / skip.
type importPreviewReq struct {
	Filename   string `json:"filename"`
	Content    string `json:"content"`
	EndpointID uint   `json:"endpoint_id,omitempty"`
}

// handleImportTunnelsPreview parses an uploaded frpc config and returns
// a Plan describing the endpoint + tunnels we would create. No state
// is mutated. plan.md §15 calls out that imports must be dry-runnable
// before committing so the operator can review name collisions and
// drop entries they do not want.
func (s *Server) handleImportTunnelsPreview(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var req importPreviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	content := []byte(req.Content)
	if len(content) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content required"})
		return
	}
	plan, err := frpcimport.Parse(content, req.Filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.EndpointID > 0 {
		if existing, err := s.store.ListTunnelsByEndpoint(req.EndpointID); err == nil {
			taken := make(map[string]struct{}, len(existing))
			for _, t := range existing {
				taken[strings.ToLower(strings.TrimSpace(t.Name))] = struct{}{}
			}
			for i := range plan.Tunnels {
				key := strings.ToLower(strings.TrimSpace(plan.Tunnels[i].Name))
				if _, hit := taken[key]; hit {
					plan.Tunnels[i].Conflict = true
				}
			}
		}
	}
	c.JSON(http.StatusOK, plan)
}

// importCommitReq is the commit payload. The endpoint_id selects which
// existing endpoint should host the imported tunnels — we never create
// endpoints implicitly through import to keep the security boundary
// (token / TLS) explicit. Each tunnel is the user-edited TunnelDraft
// (the preview UI may have renamed entries or dropped some).
//
// DefaultOnConflict applies when a draft does not specify its own
// override; valid values are "error" (default), "skip", "rename".
type importCommitReq struct {
	EndpointID        uint                  `json:"endpoint_id"`
	Tunnels           []importCommitTunnel  `json:"tunnels"`
	DefaultOnConflict string                `json:"default_on_conflict,omitempty"`
}

// importCommitTunnel wraps the draft with the per-row conflict resolution
// strategy chosen in the preview UI. Keeping the override per-row lets
// the operator skip noisy collisions while still hard-renaming the rest.
type importCommitTunnel struct {
	frpcimport.TunnelDraft
	OnConflict string `json:"on_conflict,omitempty"`
}

// Conflict resolution strategies. We accept the strings literally from
// the UI; anything unrecognised falls back to "error" so a typo cannot
// silently skip a tunnel the operator wanted to import.
const (
	importConflictError  = "error"
	importConflictSkip   = "skip"
	importConflictRename = "rename"
)

// importCommitResult records what happened to one tunnel during commit.
// We always return both Created and Errors so the UI can render a
// per-row badge instead of failing the whole batch on the first error.
// Skipped is set when a row collided and the resolution chose to skip
// it; the row is still reported so the UI can render a visual hint.
type importCommitItem struct {
	Name    string `json:"name"`
	ID      uint   `json:"id,omitempty"`
	Error   string `json:"error,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
	Renamed string `json:"renamed,omitempty"`
}

// handleImportTunnelsCommit creates one tunnel per draft. It validates
// each draft through the regular tunnelReq.validate() so the import
// path produces tunnels indistinguishable from manually-created ones —
// just with `source = imported` for analytics. We deliberately skip
// pre-validating the entire batch up front: a malformed entry should
// not block the rest from being imported.
func (s *Server) handleImportTunnelsCommit(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var req importCommitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.EndpointID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint_id required"})
		return
	}
	ep, err := s.store.GetEndpoint(req.EndpointID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if ep == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint not found"})
		return
	}
	if len(req.Tunnels) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tunnels required"})
		return
	}

	defaultStrategy := normaliseConflictStrategy(req.DefaultOnConflict)

	existing, err := s.store.ListTunnelsByEndpoint(req.EndpointID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	taken := make(map[string]struct{}, len(existing))
	for _, t := range existing {
		taken[strings.ToLower(strings.TrimSpace(t.Name))] = struct{}{}
	}

	uid, actor, _ := auth.Principal(c)
	results := make([]importCommitItem, 0, len(req.Tunnels))
	for _, draft := range req.Tunnels {
		original := strings.TrimSpace(draft.Name)
		item := importCommitItem{Name: original}
		strategy := normaliseConflictStrategy(draft.OnConflict)
		if strategy == "" {
			strategy = defaultStrategy
		}
		if strategy == "" {
			strategy = importConflictError
		}

		nameLower := strings.ToLower(original)
		if _, hit := taken[nameLower]; hit {
			switch strategy {
			case importConflictSkip:
				item.Skipped = true
				results = append(results, item)
				continue
			case importConflictRename:
				renamed := uniqueImportName(original, taken)
				draft.Name = renamed
				item.Renamed = renamed
			default:
				item.Error = "tunnel name already exists; pick rename or skip"
				results = append(results, item)
				continue
			}
		}

		tr := importDraftToReq(req.EndpointID, draft.TunnelDraft)
		if err := tr.validate(); err != nil {
			item.Error = err.Error()
			results = append(results, item)
			continue
		}
		var t model.Tunnel
		tr.applyToTunnel(&t, nil)
		t.Status = model.StatusPending
		t.Source = model.TunnelSourceImported
		t.CreatedBy = uid
		if err := s.store.CreateTunnel(&t); err != nil {
			item.Error = err.Error()
			results = append(results, item)
			continue
		}
		if err := s.lifecycle.Schedule(&t); err != nil {
			// Best-effort: tunnel persisted, lifecycle hookup failed —
			// the periodic reconcile will recover, so we still report
			// success but pass the error through for visibility.
			item.Error = err.Error()
		}
		item.ID = t.ID
		taken[strings.ToLower(strings.TrimSpace(t.Name))] = struct{}{}
		results = append(results, item)
		_ = s.store.WriteAudit(&model.AuditLog{
			Action: "import_tunnel", TunnelID: t.ID, Actor: actor, ActorIP: s.clientIP(c),
			Detail: t.Name,
		})
	}

	c.JSON(http.StatusOK, gin.H{"items": results})
}

// normaliseConflictStrategy validates and lower-cases the strategy
// string. Empty input returns "" so the caller can decide between
// per-tunnel override → batch default → final fallback.
func normaliseConflictStrategy(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "skip":
		return importConflictSkip
	case "rename":
		return importConflictRename
	case "error":
		return importConflictError
	default:
		return ""
	}
}

// uniqueImportName appends `-<n>` (n>=2) until the result is not in the
// taken set. Naming defaults to `name-2` (not `-1`) because operators
// expect the original to read as the "primary" of the cluster.
func uniqueImportName(base string, taken map[string]struct{}) string {
	if base == "" {
		base = "imported"
	}
	for n := 2; n < 1000; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if _, hit := taken[strings.ToLower(candidate)]; !hit {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}

// importDraftToReq adapts a frpcimport.TunnelDraft into the existing
// tunnelReq used by the validate / applyToTunnel pipeline. Keeping the
// adapter local avoids leaking import-only types into the DTO file.
func importDraftToReq(endpointID uint, d frpcimport.TunnelDraft) *tunnelReq {
	return &tunnelReq{
		EndpointID:        endpointID,
		Name:              d.Name,
		Type:              d.Type,
		Role:              d.Role,
		LocalIP:           d.LocalIP,
		LocalPort:         d.LocalPort,
		RemotePort:        d.RemotePort,
		CustomDomains:     d.CustomDomains,
		Subdomain:         d.Subdomain,
		Locations:         d.Locations,
		HTTPUser:          d.HTTPUser,
		HTTPPassword:      d.HTTPPassword,
		HostHeaderRewrite: d.HostHeaderRewrite,
		SK:                d.SK,
		AllowUsers:        d.AllowUsers,
		ServerName:        d.ServerName,
		Encryption:        d.Encryption,
		Compression:       d.Compression,
		BandwidthLimit:    d.BandwidthLimit,
		Group:             d.Group,
		GroupKey:          d.GroupKey,
		HealthCheckType:   d.HealthCheckType,
		HealthCheckURL:    d.HealthCheckURL,
		Plugin:            d.Plugin,
		PluginConfig:      d.PluginConfig,
		Enabled:           d.Enabled,
		AutoStart:         d.AutoStart,
	}
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
