// HTTP surface for the SubprocessDriver supporting actions:
//
//   - POST /api/frpc/probe        — probe a path/PATH binary's frpc -v
//   - POST /api/frpc/download     — kick a one-click download to data_dir/bin
//
// These routes are admin-only because they touch executables on disk.

package api

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/model"
)

type subprocessProbeReq struct {
	Path string `json:"path"`
}

type subprocessProbeResp struct {
	Path        string `json:"path"`
	Version     string `json:"version"`
	Compatible  bool   `json:"compatible"`
	MinRequired string `json:"min_required"`
}

// handleProbeFrpc invokes `frpc -v` on the supplied (or PATH-located)
// binary and returns the parsed version. Compatibility is reported as
// a boolean so the UI can disable Save until the operator picks a
// supported binary.
func (s *Server) handleProbeFrpc(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var req subprocessProbeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		// Fallback to PATH lookup so the operator can confirm a
		// system-wide frpc install exists before saving.
		p, err := exec.LookPath("frpc")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "frpc binary not found in PATH"})
			return
		}
		path = p
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	ver, err := frpcd.ProbeFrpcVersion(ctx, path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp := subprocessProbeResp{
		Path:        path,
		Version:     ver,
		MinRequired: frpcd.MinimumFrpVersion,
		Compatible:  frpcd.CompareVersion(ver, frpcd.MinimumFrpVersion),
	}
	c.JSON(http.StatusOK, resp)
}

type subprocessDownloadReq struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	SHA256  string `json:"sha256"`
}

type subprocessDownloadResp struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// handleDownloadFrpc fetches the requested release, verifies the digest
// when supplied, and stores the binary under <data_dir>/bin/. The
// response includes the resolved on-disk path so the UI can prefill
// `subprocess_path` for the operator.
func (s *Server) handleDownloadFrpc(c *gin.Context) {
	if !s.ensureAdmin(c) {
		return
	}
	var req subprocessDownloadReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Version) == "" {
		req.Version = frpcd.BundledFrpVersion
	}
	if !strings.HasPrefix(req.Version, "v") {
		req.Version = "v" + strings.TrimSpace(req.Version)
	}
	if !frpcd.CompareVersion(req.Version, frpcd.MinimumFrpVersion) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":        "version below minimum supported (" + frpcd.MinimumFrpVersion + ")",
			"min_required": frpcd.MinimumFrpVersion,
		})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	path, err := frpcd.DownloadFrpc(ctx, frpcd.DownloadOptions{
		Version:        req.Version,
		OS:             strings.TrimSpace(req.OS),
		Arch:           strings.TrimSpace(req.Arch),
		ExpectedSHA256: strings.TrimSpace(req.SHA256),
		DataDir:        s.cfg.DataDir,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "download_frpc", Actor: actor, ActorIP: s.clientIP(c),
		Detail: req.Version + " -> " + path,
	})
	c.JSON(http.StatusOK, subprocessDownloadResp{Path: path, Version: req.Version})
}

// ensureSubprocessReady is invoked by the endpoint create/update
// pipeline whenever DriverMode == subprocess. It probes the configured
// binary and stores the version on the endpoint so the UI can flag a
// stale cache. Returning nil is non-fatal: a missing binary is allowed
// if the operator has not yet downloaded it (Start() will surface the
// real error).
func (s *Server) ensureSubprocessReady(ep *model.Endpoint) error {
	if ep == nil || ep.DriverMode != model.DriverModeSubprocess {
		return nil
	}
	path := strings.TrimSpace(ep.SubprocessPath)
	if path == "" {
		return nil
	}
	ver, err := frpcd.ProbeFrpcVersion(context.Background(), path)
	if err != nil {
		ep.SubprocessVersion = ""
		return err
	}
	if !frpcd.CompareVersion(ver, frpcd.MinimumFrpVersion) {
		ep.SubprocessVersion = ver
		return errors.New("frpc " + ver + " < minimum " + frpcd.MinimumFrpVersion)
	}
	ep.SubprocessVersion = ver
	return nil
}
