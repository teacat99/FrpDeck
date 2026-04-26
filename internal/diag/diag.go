// Package diag implements the post-save connectivity self-check that
// plan.md §7.3 promises: a quick four-step probe that tells the user
// "is this tunnel actually going to work" without forcing them to read
// frpc / frps logs themselves.
//
// Layering:
//
//   - diag/diag.go (this file) — pure probing logic; no HTTP, no DB.
//     Inputs are model.Endpoint + model.Tunnel + an optional driver
//     `Probe` interface so unit tests can stub state without spinning
//     up an Embedded driver.
//
// The four checks run in deterministic order so the UI can render a
// step-by-step list. Each check returns its own `Status`, `Message`
// and optional `Hint`; the aggregate `Overall` follows
// fail > warn > ok > skipped severity ordering.
//
// Design note (why probes, not assertions): we do not block tunnel
// creation on diag failure. A user might be configuring against an
// frps that isn't up yet, or against a domain that DNS hasn't
// propagated. Diag is informational. The save path stays fast.
package diag

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/model"
)

// Status enumerates the possible verdicts of a single check. The values
// double as i18n suffixes on the frontend (`diag.status.<value>`), so
// keep them lowercase ASCII and stable.
type Status string

const (
	StatusOK      Status = "ok"
	StatusWarn    Status = "warn"
	StatusFail    Status = "fail"
	StatusSkipped Status = "skipped"
)

// Check IDs. Stable so the frontend can map them to localized titles
// and hints; never rename without updating both i18n bundles.
const (
	CheckDNS        = "dns"
	CheckTCPProbe   = "tcp_probe"
	CheckRegister   = "frps_register"
	CheckLocalReach = "local_reach"
)

// Check is the result of a single probe step.
type Check struct {
	ID       string `json:"id"`
	Status   Status `json:"status"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
	Duration int64  `json:"duration_ms"`
}

// Report is the JSON payload returned by POST /api/tunnels/:id/diagnose.
type Report struct {
	TunnelID    uint      `json:"tunnel_id"`
	EndpointID  uint      `json:"endpoint_id"`
	Overall     Status    `json:"overall"`
	GeneratedAt time.Time `json:"generated_at"`
	Checks      []Check   `json:"checks"`
}

// Probe is the read-only slice of FrpDriver needed by the runner.
// Pulling it into its own interface keeps the diag package easy to
// test (no driver boot) and avoids making `frpcd` an unconditional
// import wherever diag wants to live next.
type Probe interface {
	GetEndpointStatus(ep *model.Endpoint) (*frpcd.EndpointStatus, error)
}

// Runner orchestrates the four checks. Hold an instance per Server
// (cheap; just a couple of timeouts).
type Runner struct {
	driver      Probe
	dialTimeout time.Duration
	dnsTimeout  time.Duration
}

// NewRunner builds a Runner with sensible defaults. The probe is
// allowed to be nil — the register check downgrades to "skipped" if
// no driver is wired (handy for headless smoke tests).
func NewRunner(d Probe) *Runner {
	return &Runner{
		driver:      d,
		dialTimeout: 3 * time.Second,
		dnsTimeout:  3 * time.Second,
	}
}

// Run executes the four checks against the given (endpoint, tunnel)
// pair and returns the aggregated report. Run is safe to call
// concurrently for distinct (ep, t) pairs; each invocation builds its
// own check slice and never mutates shared state.
func (r *Runner) Run(ctx context.Context, ep *model.Endpoint, t *model.Tunnel) *Report {
	rep := &Report{
		TunnelID:    t.ID,
		EndpointID:  ep.ID,
		GeneratedAt: time.Now().UTC(),
		Checks:      make([]Check, 0, 4),
	}
	rep.Checks = append(rep.Checks, r.checkDNS(ctx, ep))
	rep.Checks = append(rep.Checks, r.checkTCP(ctx, ep))
	rep.Checks = append(rep.Checks, r.checkRegister(ep))
	rep.Checks = append(rep.Checks, r.checkLocalReach(ctx, t))
	rep.Overall = aggregate(rep.Checks)
	return rep
}

// aggregate folds individual check verdicts into the report-level
// Overall. Severity order is fail > warn > ok > skipped; "all
// skipped" is reported as Skipped so the UI can render a neutral
// banner ("nothing to verify yet").
func aggregate(checks []Check) Status {
	has := map[Status]bool{}
	for _, c := range checks {
		has[c.Status] = true
	}
	switch {
	case has[StatusFail]:
		return StatusFail
	case has[StatusWarn]:
		return StatusWarn
	case has[StatusOK]:
		return StatusOK
	default:
		return StatusSkipped
	}
}

// checkDNS resolves ep.Addr. Literal IPs are reported Skipped so the
// user is not nagged for doing the right thing. Empty addr is a hard
// failure since the endpoint cannot work without one.
func (r *Runner) checkDNS(ctx context.Context, ep *model.Endpoint) Check {
	start := time.Now()
	out := Check{ID: CheckDNS}
	defer func() { out.Duration = time.Since(start).Milliseconds() }()

	addr := strings.TrimSpace(ep.Addr)
	if addr == "" {
		out.Status = StatusFail
		out.Message = "endpoint addr is empty"
		return out
	}
	if ip := net.ParseIP(addr); ip != nil {
		out.Status = StatusSkipped
		out.Message = fmt.Sprintf("addr %s is a literal IP, DNS lookup not required", addr)
		return out
	}
	cctx, cancel := context.WithTimeout(ctx, r.dnsTimeout)
	defer cancel()
	res, err := net.DefaultResolver.LookupHost(cctx, addr)
	if err != nil {
		out.Status = StatusFail
		out.Message = err.Error()
		out.Hint = "确认 addr 在本机可解析（DNS / hosts / VPN 路由）"
		return out
	}
	out.Status = StatusOK
	out.Message = fmt.Sprintf("resolved %d address(es): %s", len(res), strings.Join(res, ", "))
	return out
}

// checkTCP dials the frps endpoint port. For UDP-style transports
// (kcp/quic) a TCP probe doesn't really tell us much, so a failure
// is downgraded to Warn — the UI nudges the user to verify with a
// real handshake.
func (r *Runner) checkTCP(ctx context.Context, ep *model.Endpoint) Check {
	start := time.Now()
	out := Check{ID: CheckTCPProbe}
	defer func() { out.Duration = time.Since(start).Milliseconds() }()

	addr := strings.TrimSpace(ep.Addr)
	if addr == "" || ep.Port <= 0 {
		out.Status = StatusFail
		out.Message = "endpoint addr/port is invalid"
		return out
	}
	proto := strings.ToLower(strings.TrimSpace(ep.Protocol))
	target := net.JoinHostPort(addr, strconv.Itoa(ep.Port))
	cctx, cancel := context.WithTimeout(ctx, r.dialTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(cctx, "tcp", target)
	if err != nil {
		if proto == "kcp" || proto == "quic" {
			out.Status = StatusWarn
			out.Message = fmt.Sprintf("TCP probe to %s failed: %v (protocol=%s, expected for non-TCP transport)", target, err, proto)
			out.Hint = "对 KCP/QUIC frps 此项无法直接验证，请用真实代理握手验证"
			return out
		}
		out.Status = StatusFail
		out.Message = fmt.Sprintf("TCP dial %s: %v", target, err)
		out.Hint = "确认 frps 端口已监听 / 防火墙放行 / NAT 转发正确"
		return out
	}
	_ = conn.Close()
	out.Status = StatusOK
	out.Message = fmt.Sprintf("TCP %s reachable", target)
	return out
}

// checkRegister consults the live frpcd driver state for the
// endpoint. We map the four canonical states to a verdict:
//
//	connected    → ok
//	connecting   → warn (transient)
//	disconnected → skipped (not started yet — Save flow's own job)
//	failed       → fail (with last error if any)
//
// If no driver is wired (Probe nil) the check is Skipped.
func (r *Runner) checkRegister(ep *model.Endpoint) Check {
	start := time.Now()
	out := Check{ID: CheckRegister}
	defer func() { out.Duration = time.Since(start).Milliseconds() }()

	if r.driver == nil {
		out.Status = StatusSkipped
		out.Message = "driver probe not wired"
		return out
	}
	if !ep.Enabled {
		out.Status = StatusSkipped
		out.Message = "endpoint disabled — start it before this check is meaningful"
		return out
	}
	st, err := r.driver.GetEndpointStatus(ep)
	if err != nil {
		out.Status = StatusFail
		out.Message = err.Error()
		return out
	}
	if st == nil {
		out.Status = StatusSkipped
		out.Message = "no status reported yet"
		return out
	}
	switch strings.ToLower(st.State) {
	case "connected":
		out.Status = StatusOK
		out.Message = "frps session connected"
	case "connecting":
		out.Status = StatusWarn
		out.Message = "still connecting; rerun in a few seconds"
	case "disconnected", "":
		out.Status = StatusSkipped
		out.Message = "endpoint not yet connected; start it first"
	case "failed":
		out.Status = StatusFail
		out.Message = "frps session failed"
		if st.LastError != "" {
			out.Message += ": " + st.LastError
		}
		out.Hint = "查看实时日志面板获取 token / TLS / heartbeat 的具体错误"
	default:
		out.Status = StatusWarn
		out.Message = "unknown endpoint state: " + st.State
	}
	return out
}

// checkLocalReach probes the local target the proxy will forward to.
// Visitor tunnels and plugin-backed proxies have no LocalIP:LocalPort
// to probe; both are reported as Skipped instead of fabricated.
// UDP/SUDP proxies cannot be honestly probed by TCP, so the failure
// is softened to Warn.
func (r *Runner) checkLocalReach(ctx context.Context, t *model.Tunnel) Check {
	start := time.Now()
	out := Check{ID: CheckLocalReach}
	defer func() { out.Duration = time.Since(start).Milliseconds() }()

	role := strings.ToLower(strings.TrimSpace(t.Role))
	typ := strings.ToLower(strings.TrimSpace(t.Type))

	if role == "visitor" {
		out.Status = StatusSkipped
		out.Message = "visitor 角色无本地目标，跳过"
		return out
	}
	if strings.TrimSpace(t.Plugin) != "" {
		out.Status = StatusSkipped
		out.Message = fmt.Sprintf("plugin=%s 自管数据源，跳过本地端口探测", t.Plugin)
		return out
	}
	if t.LocalPort <= 0 {
		out.Status = StatusFail
		out.Message = "local_port not configured"
		return out
	}
	addr := strings.TrimSpace(t.LocalIP)
	if addr == "" {
		addr = "127.0.0.1"
	}
	target := net.JoinHostPort(addr, strconv.Itoa(t.LocalPort))
	cctx, cancel := context.WithTimeout(ctx, r.dialTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(cctx, "tcp", target)
	if err != nil {
		if typ == "udp" || typ == "sudp" {
			out.Status = StatusWarn
			out.Message = fmt.Sprintf("TCP probe to %s failed: %v (UDP-style proxy, TCP probe is best-effort)", target, err)
			out.Hint = "UDP 服务请通过实际客户端验证"
			return out
		}
		out.Status = StatusFail
		out.Message = fmt.Sprintf("TCP dial %s: %v", target, err)
		out.Hint = "确认本地服务已启动并监听该端口（systemctl / netstat -lntp / ss -lntp）"
		return out
	}
	_ = conn.Close()
	out.Status = StatusOK
	out.Message = fmt.Sprintf("local TCP %s reachable", target)
	return out
}
