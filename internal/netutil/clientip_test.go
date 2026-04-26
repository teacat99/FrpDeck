package netutil

import (
	"net"
	"net/http"
	"testing"
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func TestClientIP_NoTrustedProxy(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.1:12345"
	r.Header.Set("X-Forwarded-For", "evil.example")
	got := ClientIP(r, nil)
	if got != "203.0.113.1" {
		t.Fatalf("want remote addr when no proxies trusted, got %q", got)
	}
}

func TestClientIP_TrustedChain(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:9999"
	r.Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.10, 10.0.0.5")
	trusted := []*net.IPNet{mustCIDR("10.0.0.0/8")}
	got := ClientIP(r, trusted)
	if got != "198.51.100.10" {
		t.Fatalf("expected first untrusted hop, got %q", got)
	}
}

func TestClientIP_RealIPHeader(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("X-Real-IP", "198.51.100.99")
	trusted := []*net.IPNet{mustCIDR("127.0.0.0/8")}
	got := ClientIP(r, trusted)
	if got != "198.51.100.99" {
		t.Fatalf("expected X-Real-IP value, got %q", got)
	}
}
