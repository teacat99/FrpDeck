package netutil

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the perceived public client IP for an HTTP request,
// honouring X-Forwarded-For only for hops contained in trustedProxies.
//
// The algorithm walks the XFF chain right-to-left. As long as each node is
// reachable via a trusted proxy, we pop it and continue; when we encounter
// an untrusted hop we stop and use that as the client. This matches the
// behaviour described in https://adam-p.ca/blog/2022/03/x-forwarded-for/.
func ClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remote := stripPort(r.RemoteAddr)
	if len(trustedProxies) == 0 {
		return remote
	}
	if !isTrusted(remote, trustedProxies) {
		return remote
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		if rip := strings.TrimSpace(r.Header.Get("X-Real-IP")); rip != "" {
			return rip
		}
		return remote
	}
	parts := splitAndTrim(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := parts[i]
		if i == 0 {
			return ip
		}
		if !isTrusted(ip, trustedProxies) {
			return ip
		}
	}
	return remote
}

func isTrusted(ip string, nets []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func stripPort(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func splitAndTrim(s, sep string) []string {
	raw := strings.Split(s, sep)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
