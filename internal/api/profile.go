// Profile CRUD + activation HTTP handlers (P8-C).
//
// REST shape:
//
//	GET    /api/profiles                  → list
//	POST   /api/profiles                  → create
//	GET    /api/profiles/active           → currently active profile (or null)
//	GET    /api/profiles/:id              → fetch one (with bindings)
//	PUT    /api/profiles/:id              → update name + bindings
//	DELETE /api/profiles/:id              → delete (refuses active)
//	POST   /api/profiles/:id/activate     → switch active profile, reconcile
//	POST   /api/profiles/deactivate       → mark all inactive (no fan-out)
//
// Activation is the load-bearing operation: it flips Endpoint.Enabled
// and Tunnel.Enabled in a single transaction, then nudges lifecycle
// reconcile so the runtime catches up.

package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/model"
)

type profileBindingDTO struct {
	EndpointID uint `json:"endpoint_id"`
	TunnelID   uint `json:"tunnel_id"`
}

type profileReq struct {
	Name     string              `json:"name"`
	Active   bool                `json:"active"`
	Bindings []profileBindingDTO `json:"bindings"`
}

func (r *profileReq) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("name required")
	}
	for _, b := range r.Bindings {
		if b.EndpointID == 0 && b.TunnelID == 0 {
			return errors.New("binding must reference an endpoint or tunnel")
		}
	}
	return nil
}

func (r *profileReq) toModelBindings() []model.ProfileBinding {
	out := make([]model.ProfileBinding, 0, len(r.Bindings))
	for _, b := range r.Bindings {
		out = append(out, model.ProfileBinding{
			EndpointID: b.EndpointID,
			TunnelID:   b.TunnelID,
		})
	}
	return out
}

type profileResp struct {
	Profile  model.Profile         `json:"profile"`
	Bindings []model.ProfileBinding `json:"bindings"`
}

func (s *Server) registerProfileRoutes(g *gin.RouterGroup) {
	g.GET("/profiles", s.handleListProfiles)
	g.POST("/profiles", s.handleCreateProfile)
	g.GET("/profiles/active", s.handleGetActiveProfile)
	g.POST("/profiles/deactivate", s.handleDeactivateProfiles)
	g.GET("/profiles/:id", s.handleGetProfile)
	g.PUT("/profiles/:id", s.handleUpdateProfile)
	g.DELETE("/profiles/:id", s.handleDeleteProfile)
	g.POST("/profiles/:id/activate", s.handleActivateProfile)
}

func (s *Server) handleListProfiles(c *gin.Context) {
	rows, err := s.store.ListProfiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"profiles": rows})
}

func (s *Server) handleGetActiveProfile(c *gin.Context) {
	p, err := s.store.GetActiveProfile()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusOK, gin.H{"profile": nil})
		return
	}
	bindings, err := s.store.ListProfileBindings(p.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, profileResp{Profile: *p, Bindings: bindings})
}

func (s *Server) handleGetProfile(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := s.store.GetProfile(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
		return
	}
	bindings, err := s.store.ListProfileBindings(p.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, profileResp{Profile: *p, Bindings: bindings})
}

func (s *Server) handleCreateProfile(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var req profileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p := model.Profile{Name: strings.TrimSpace(req.Name), Active: req.Active}
	bindings := req.toModelBindings()
	if err := s.store.CreateProfile(&p, bindings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p.Active {
		if _, err := s.store.ActivateProfile(p.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		s.afterProfileActivation()
	}
	rows, _ := s.store.ListProfileBindings(p.ID)
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "create_profile", Actor: actor, ActorIP: s.clientIP(c), Detail: p.Name,
	})
	c.JSON(http.StatusOK, profileResp{Profile: p, Bindings: rows})
}

func (s *Server) handleUpdateProfile(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := s.store.GetProfile(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
		return
	}
	var req profileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	patch := *existing
	patch.Name = strings.TrimSpace(req.Name)
	patch.Active = req.Active
	bindings := req.toModelBindings()
	if err := s.store.UpdateProfile(&patch, bindings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// If the active profile was edited, re-apply bindings so the runtime
	// reflects the new selection without forcing the operator to click
	// "activate" again.
	if patch.Active {
		if _, err := s.store.ActivateProfile(patch.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		s.afterProfileActivation()
	}
	rows, _ := s.store.ListProfileBindings(patch.ID)
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "update_profile", Actor: actor, ActorIP: s.clientIP(c), Detail: patch.Name,
	})
	c.JSON(http.StatusOK, profileResp{Profile: patch, Bindings: rows})
}

func (s *Server) handleDeleteProfile(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.store.DeleteProfile(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "delete_profile", Actor: actor, ActorIP: s.clientIP(c),
	})
	c.Status(http.StatusNoContent)
}

func (s *Server) handleActivateProfile(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := s.store.ActivateProfile(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.afterProfileActivation()
	bindings, _ := s.store.ListProfileBindings(p.ID)
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "activate_profile", Actor: actor, ActorIP: s.clientIP(c), Detail: p.Name,
	})
	c.JSON(http.StatusOK, profileResp{Profile: *p, Bindings: bindings})
}

func (s *Server) handleDeactivateProfiles(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	if err := s.store.DeactivateAllProfiles(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "deactivate_profiles", Actor: actor, ActorIP: s.clientIP(c),
	})
	c.Status(http.StatusOK)
}

// afterProfileActivation poke the lifecycle manager so it tears down
// tunnels that were just disabled and starts the ones that were just
// enabled. We deliberately swallow errors: Reconcile logs them itself
// and the response has already committed the persistent change.
func (s *Server) afterProfileActivation() {
	if s.lifecycle == nil {
		return
	}
	go func() {
		_ = s.lifecycle.Reconcile()
	}()
}
