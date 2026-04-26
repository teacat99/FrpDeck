// Package frpcd — Subprocess driver runner (per-endpoint state).

package frpcd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/teacat99/FrpDeck/internal/model"
)

const (
	subprocessAdminUserPrefix = "frpdeck"
	subprocessAdminAuthBytes  = 16
	subprocessReadyTimeout    = 10 * time.Second
	subprocessStopGrace       = 5 * time.Second
	subprocessRestartMin      = 1 * time.Second
	subprocessRestartMax      = 30 * time.Second
	subprocessStatusPeriod    = 3 * time.Second
)

type subprocessRunner struct {
	drv *Subprocess
	mu  sync.Mutex

	ep      model.Endpoint
	tunnels map[uint]*model.Tunnel

	binaryPath string
	runDir     string
	tomlPath   string
	logPath    string

	adminAddr     string
	adminPort     int
	adminUser     string
	adminPassword string
	adminClient   *http.Client

	cmd *exec.Cmd

	ctx       context.Context
	cancel    context.CancelFunc
	supervise sync.WaitGroup

	state      string
	lastErr    string
	tunnelInfo map[string]subprocessTunnelInfo

	stopRequested bool
}

type subprocessTunnelInfo struct {
	Phase   string
	Err     string
	Updated time.Time
}

func newSubprocessRunner(drv *Subprocess, ep *model.Endpoint) (*subprocessRunner, error) {
	bin, err := drv.resolveBinary(ep)
	if err != nil {
		return nil, err
	}
	port, err := allocateAdminPort()
	if err != nil {
		return nil, fmt.Errorf("allocate admin port: %w", err)
	}
	user := subprocessAdminUserPrefix + "-" + fmt.Sprint(ep.ID)
	pass, err := generateSecret(subprocessAdminAuthBytes)
	if err != nil {
		return nil, fmt.Errorf("generate admin secret: %w", err)
	}
	dir := drv.runDir(ep.ID)
	return &subprocessRunner{
		drv:           drv,
		ep:            *ep,
		tunnels:       make(map[uint]*model.Tunnel),
		binaryPath:    bin,
		runDir:        dir,
		tomlPath:      filepath.Join(dir, "frpc.toml"),
		logPath:       filepath.Join(dir, "frpc.log"),
		adminAddr:     "127.0.0.1",
		adminPort:     port,
		adminUser:     user,
		adminPassword: pass,
		adminClient:   &http.Client{Timeout: 5 * time.Second},
		tunnelInfo:    make(map[string]subprocessTunnelInfo),
	}, nil
}

func (r *subprocessRunner) refreshEndpoint(ep *model.Endpoint) {
	r.mu.Lock()
	r.ep = *ep
	r.mu.Unlock()
}

// start writes the initial empty config, spawns frpc, waits for the
// admin port to come up, and launches the supervision goroutine.
func (r *subprocessRunner) start(_ context.Context) error {
	if err := os.MkdirAll(r.runDir, 0o700); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}
	if err := r.renderTOML(); err != nil {
		return err
	}
	if err := r.spawn(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.ctx = ctx
	r.cancel = cancel
	r.mu.Unlock()

	r.supervise.Add(1)
	go r.superviseLoop(ctx)
	r.supervise.Add(1)
	go r.statusLoop(ctx)
	return nil
}

func (r *subprocessRunner) stop() {
	r.mu.Lock()
	r.stopRequested = true
	cmd := r.cmd
	cancel := r.cancel
	r.mu.Unlock()

	// Best-effort graceful shutdown via admin API; SIGTERM if that fails.
	_ = r.adminPostShort("/api/stop")

	if cmd != nil && cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = cmd.Process.Wait()
		}()
		select {
		case <-done:
		case <-time.After(subprocessStopGrace):
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(subprocessStopGrace):
				_ = cmd.Process.Kill()
			}
		}
	}
	if cancel != nil {
		cancel()
	}
	r.supervise.Wait()

	r.setState("disconnected", "")
}

// addTunnel persists the tunnel (replacing any prior entry by ID) and
// rewrites + reloads frpc.toml. Returns immediately on the network
// boundary; reload latency is dominated by frpc's parser cost.
func (r *subprocessRunner) addTunnel(t *model.Tunnel) error {
	if t == nil {
		return nil
	}
	r.mu.Lock()
	r.tunnels[t.ID] = t
	r.mu.Unlock()
	return r.applyAndReload()
}

func (r *subprocessRunner) removeTunnel(t *model.Tunnel) error {
	if t == nil {
		return nil
	}
	r.mu.Lock()
	delete(r.tunnels, t.ID)
	delete(r.tunnelInfo, tunnelName(t))
	r.mu.Unlock()
	return r.applyAndReload()
}

// applyAndReload re-renders frpc.toml from the current state and POSTs
// /api/reload. We tolerate the reload call returning before the process
// has actually re-read the file — the next /api/status poll will reflect
// the new state.
func (r *subprocessRunner) applyAndReload() error {
	if err := r.renderTOML(); err != nil {
		return err
	}
	r.mu.Lock()
	port := r.adminPort
	user := r.adminUser
	pass := r.adminPassword
	cmd := r.cmd
	r.mu.Unlock()
	if cmd == nil {
		return errors.New("subprocess not running")
	}
	url := fmt.Sprintf("http://%s:%d/api/reload", r.adminAddr, port)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	resp, err := r.adminClient.Do(req)
	if err != nil {
		return fmt.Errorf("admin reload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("admin reload status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (r *subprocessRunner) renderTOML() error {
	r.mu.Lock()
	tunnels := make([]*model.Tunnel, 0, len(r.tunnels))
	for _, t := range r.tunnels {
		tunnels = append(tunnels, t)
	}
	cfg := SubprocessConfig{
		Endpoint:      &r.ep,
		Tunnels:       tunnels,
		AdminAddr:     r.adminAddr,
		AdminPort:     r.adminPort,
		AdminUser:     r.adminUser,
		AdminPassword: r.adminPassword,
		LogLevel:      "info",
	}
	r.mu.Unlock()
	data, err := BuildSubprocessTOML(cfg)
	if err != nil {
		return fmt.Errorf("render toml: %w", err)
	}
	if err := os.WriteFile(r.tomlPath, data, 0o600); err != nil {
		return fmt.Errorf("write toml: %w", err)
	}
	return nil
}

// spawn forks the frpc binary against the rendered TOML. stdout/stderr
// are captured into the in-memory bus AND tee'd to <runDir>/frpc.log so
// post-mortem debugging is possible after the process is gone.
func (r *subprocessRunner) spawn() error {
	r.mu.Lock()
	if r.stopRequested {
		r.mu.Unlock()
		return errors.New("runner stopped")
	}
	cmd := exec.Command(r.binaryPath, "-c", r.tomlPath)
	cmd.Dir = r.runDir
	cmd.Env = append(os.Environ(),
		"FRP_LOG_LEVEL=info",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("spawn frpc: %w", err)
	}
	r.cmd = cmd
	r.mu.Unlock()

	r.setState("connecting", "")

	go r.readLogs(stdout, "stdout")
	go r.readLogs(stderr, "stderr")

	// Wait for the admin port to accept connections — caps spurious
	// "connecting" → "connected" flicker on subsequent /api/reload.
	deadline := time.Now().Add(subprocessReadyTimeout)
	for time.Now().Before(deadline) {
		if err := r.adminPing(); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("frpc admin port did not come up in time")
}

func (r *subprocessRunner) readLogs(rd io.ReadCloser, source string) {
	defer rd.Close()
	logFile, _ := os.OpenFile(r.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if logFile != nil {
		defer logFile.Close()
	}
	scanner := bufio.NewScanner(rd)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if logFile != nil {
			fmt.Fprintln(logFile, line)
		}
		level := "info"
		if source == "stderr" {
			level = "warn"
		}
		r.drv.bus.Publish(Event{
			Type:       EventLog,
			EndpointID: r.ep.ID,
			Level:      level,
			Msg:        line,
			At:         time.Now(),
		})
	}
}

// superviseLoop watches the cmd; on unexpected exit it backs off and
// respawns. When stop() has been called we stay terminated.
func (r *subprocessRunner) superviseLoop(ctx context.Context) {
	defer r.supervise.Done()
	backoff := subprocessRestartMin
	for {
		r.mu.Lock()
		cmd := r.cmd
		stopRequested := r.stopRequested
		r.mu.Unlock()
		if cmd == nil {
			return
		}
		err := cmd.Wait()
		select {
		case <-ctx.Done():
			return
		default:
		}
		if stopRequested {
			return
		}
		exitMsg := ""
		if err != nil {
			exitMsg = err.Error()
		}
		r.setState("failed", "frpc exited: "+exitMsg)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff *= 2
		if backoff > subprocessRestartMax {
			backoff = subprocessRestartMax
		}
		if err := r.spawn(); err != nil {
			r.setState("failed", "respawn: "+err.Error())
		} else {
			backoff = subprocessRestartMin
		}
	}
}

// statusLoop polls /api/status every subprocessStatusPeriod, mapping
// frp's phase strings to FrpDeck terms and publishing EventTunnelState
// on transitions only.
func (r *subprocessRunner) statusLoop(ctx context.Context) {
	defer r.supervise.Done()
	t := time.NewTicker(subprocessStatusPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.pollStatus()
		}
	}
}

// adminStatus is the /api/status payload schema (only the bits we use).
type adminStatus map[string][]struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Err    string `json:"err"`
	Type   string `json:"type"`
}

func (r *subprocessRunner) pollStatus() {
	url := fmt.Sprintf("http://%s:%d/api/status", r.adminAddr, r.adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.SetBasicAuth(r.adminUser, r.adminPassword)
	resp, err := r.adminClient.Do(req)
	if err != nil {
		// Admin API gone is a soft failure — supervise loop owns the
		// "process really died" decision.
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var status adminStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return
	}

	r.mu.Lock()
	prev := r.tunnelInfo
	r.tunnelInfo = make(map[string]subprocessTunnelInfo, len(prev))
	tunnelByName := make(map[string]uint, len(r.tunnels))
	for tid, t := range r.tunnels {
		tunnelByName[tunnelName(t)] = tid
	}
	r.mu.Unlock()

	anyRunning := false
	for _, group := range status {
		for _, item := range group {
			r.mu.Lock()
			r.tunnelInfo[item.Name] = subprocessTunnelInfo{
				Phase:   item.Status,
				Err:     item.Err,
				Updated: time.Now(),
			}
			r.mu.Unlock()
			if item.Status != "" && item.Status != "new" {
				anyRunning = true
			}
			tunnelID, ok := tunnelByName[item.Name]
			if !ok {
				continue
			}
			previous := prev[item.Name].Phase
			if previous == item.Status {
				continue
			}
			r.drv.bus.Publish(Event{
				Type:       EventTunnelState,
				EndpointID: r.ep.ID,
				TunnelID:   tunnelID,
				State:      mapPhase(item.Status),
				Err:        item.Err,
			})
		}
	}
	if anyRunning {
		state, _ := r.snapshotState()
		if state != "connected" {
			r.setState("connected", "")
		}
	}
}

func (r *subprocessRunner) tunnelPhase(name string) (string, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, ok := r.tunnelInfo[name]
	if !ok {
		return "", "", false
	}
	return info.Phase, info.Err, true
}

func (r *subprocessRunner) snapshotState() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.lastErr
}

func (r *subprocessRunner) setState(state, errMsg string) {
	r.mu.Lock()
	if r.state == state && r.lastErr == errMsg {
		r.mu.Unlock()
		return
	}
	r.state = state
	r.lastErr = errMsg
	r.mu.Unlock()
	r.drv.bus.Publish(Event{
		Type:       EventEndpointState,
		EndpointID: r.ep.ID,
		State:      state,
		Err:        errMsg,
	})
}

func (r *subprocessRunner) adminPing() error {
	url := fmt.Sprintf("http://%s:%d/api/serverinfo", r.adminAddr, r.adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(r.adminUser, r.adminPassword)
	c := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("admin auth rejected")
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("admin status %d", resp.StatusCode)
	}
	return nil
}

func (r *subprocessRunner) adminPostShort(path string) error {
	url := fmt.Sprintf("http://%s:%d%s", r.adminAddr, r.adminPort, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return err
	}
	req.SetBasicAuth(r.adminUser, r.adminPassword)
	c := &http.Client{Timeout: 1 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Hint to the linter that we use net for the dialing helper.
var _ = net.JoinHostPort
