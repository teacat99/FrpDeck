package diag

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/model"
)

// stubProbe lets each test pin the endpoint state the runner will see.
type stubProbe struct {
	state     string
	lastErr   string
	returnErr error
	nilStatus bool
}

func (s *stubProbe) GetEndpointStatus(_ *model.Endpoint) (*frpcd.EndpointStatus, error) {
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	if s.nilStatus {
		return nil, nil
	}
	return &frpcd.EndpointStatus{State: s.state, LastError: s.lastErr, UpdatedAt: time.Now()}, nil
}

func find(rep *Report, id string) *Check {
	for i := range rep.Checks {
		if rep.Checks[i].ID == id {
			return &rep.Checks[i]
		}
	}
	return nil
}

// listenTCP binds a transient TCP port on 127.0.0.1 so the local-reach
// check has a real target to dial. Caller closes the listener.
func listenTCP(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return ln, port
}

func TestCheckDNSLiteralIPSkipped(t *testing.T) {
	r := NewRunner(nil)
	out := r.checkDNS(context.Background(), &model.Endpoint{Addr: "127.0.0.1"})
	if out.Status != StatusSkipped {
		t.Fatalf("expected skipped, got %s (%s)", out.Status, out.Message)
	}
}

func TestCheckDNSEmptyAddrFails(t *testing.T) {
	r := NewRunner(nil)
	out := r.checkDNS(context.Background(), &model.Endpoint{Addr: ""})
	if out.Status != StatusFail {
		t.Fatalf("expected fail, got %s", out.Status)
	}
}

func TestCheckTCPReachable(t *testing.T) {
	ln, port := listenTCP(t)
	defer ln.Close()
	r := NewRunner(nil)
	out := r.checkTCP(context.Background(), &model.Endpoint{Addr: "127.0.0.1", Port: port})
	if out.Status != StatusOK {
		t.Fatalf("expected ok, got %s (%s)", out.Status, out.Message)
	}
}

func TestCheckTCPUnreachableFails(t *testing.T) {
	r := NewRunner(nil)
	r.dialTimeout = 200 * time.Millisecond
	// Use a port we're (very) unlikely to have open + a non-routable
	// IP so the dial fails fast on every CI runner.
	out := r.checkTCP(context.Background(), &model.Endpoint{Addr: "127.0.0.1", Port: 1})
	if out.Status != StatusFail {
		t.Fatalf("expected fail, got %s (%s)", out.Status, out.Message)
	}
}

func TestCheckTCPKCPDowngradedToWarn(t *testing.T) {
	r := NewRunner(nil)
	r.dialTimeout = 200 * time.Millisecond
	out := r.checkTCP(context.Background(), &model.Endpoint{Addr: "127.0.0.1", Port: 1, Protocol: "kcp"})
	if out.Status != StatusWarn {
		t.Fatalf("expected warn for kcp probe miss, got %s (%s)", out.Status, out.Message)
	}
}

func TestCheckRegisterStates(t *testing.T) {
	cases := []struct {
		state string
		want  Status
	}{
		{"connected", StatusOK},
		{"connecting", StatusWarn},
		{"disconnected", StatusSkipped},
		{"", StatusSkipped},
		{"failed", StatusFail},
		{"weird", StatusWarn},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			r := NewRunner(&stubProbe{state: tc.state, lastErr: "boom"})
			out := r.checkRegister(&model.Endpoint{Enabled: true})
			if out.Status != tc.want {
				t.Fatalf("state=%q got %s want %s (%s)", tc.state, out.Status, tc.want, out.Message)
			}
			if tc.state == "failed" && !strings.Contains(out.Message, "boom") {
				t.Fatalf("failed state should embed last error, got %q", out.Message)
			}
		})
	}
}

func TestCheckRegisterDisabledSkipped(t *testing.T) {
	r := NewRunner(&stubProbe{state: "connected"})
	out := r.checkRegister(&model.Endpoint{Enabled: false})
	if out.Status != StatusSkipped {
		t.Fatalf("expected skipped for disabled endpoint, got %s", out.Status)
	}
}

func TestCheckLocalReachVisitorSkipped(t *testing.T) {
	r := NewRunner(nil)
	out := r.checkLocalReach(context.Background(), &model.Tunnel{Role: "visitor"})
	if out.Status != StatusSkipped {
		t.Fatalf("visitor should skip local reach, got %s", out.Status)
	}
}

func TestCheckLocalReachPluginSkipped(t *testing.T) {
	r := NewRunner(nil)
	out := r.checkLocalReach(context.Background(), &model.Tunnel{Plugin: "socks5", LocalPort: 0})
	if out.Status != StatusSkipped {
		t.Fatalf("plugin tunnel should skip local reach, got %s (%s)", out.Status, out.Message)
	}
}

func TestCheckLocalReachOK(t *testing.T) {
	ln, port := listenTCP(t)
	defer ln.Close()
	r := NewRunner(nil)
	out := r.checkLocalReach(context.Background(), &model.Tunnel{LocalIP: "127.0.0.1", LocalPort: port})
	if out.Status != StatusOK {
		t.Fatalf("expected ok local reach, got %s (%s)", out.Status, out.Message)
	}
}

func TestCheckLocalReachUDPDowngraded(t *testing.T) {
	r := NewRunner(nil)
	r.dialTimeout = 200 * time.Millisecond
	out := r.checkLocalReach(context.Background(), &model.Tunnel{Type: "udp", LocalIP: "127.0.0.1", LocalPort: 1})
	if out.Status != StatusWarn {
		t.Fatalf("udp dial fail should be warn, got %s", out.Status)
	}
}

func TestRunAggregatesAndOrders(t *testing.T) {
	ln, port := listenTCP(t)
	defer ln.Close()
	r := NewRunner(&stubProbe{state: "connected"})
	rep := r.Run(context.Background(),
		&model.Endpoint{Addr: "127.0.0.1", Port: port, Enabled: true},
		&model.Tunnel{LocalIP: "127.0.0.1", LocalPort: port, Type: "tcp"},
	)
	if rep.Overall != StatusOK {
		t.Fatalf("expected ok overall, got %s", rep.Overall)
	}
	if len(rep.Checks) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(rep.Checks))
	}
	wantOrder := []string{CheckDNS, CheckTCPProbe, CheckRegister, CheckLocalReach}
	for i, w := range wantOrder {
		if rep.Checks[i].ID != w {
			t.Fatalf("check %d expected %q got %q", i, w, rep.Checks[i].ID)
		}
	}
	if find(rep, CheckDNS).Status != StatusSkipped {
		t.Fatalf("dns vs literal IP should be skipped")
	}
}

func TestRunAggregateFailWins(t *testing.T) {
	r := NewRunner(&stubProbe{state: "failed"})
	r.dialTimeout = 200 * time.Millisecond
	rep := r.Run(context.Background(),
		&model.Endpoint{Addr: "127.0.0.1", Port: 1, Enabled: true},
		&model.Tunnel{LocalIP: "127.0.0.1", LocalPort: 1, Type: "tcp"},
	)
	if rep.Overall != StatusFail {
		t.Fatalf("any fail should win, got %s", rep.Overall)
	}
}
