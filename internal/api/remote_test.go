package api

import (
	"net"
	"testing"

	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/model"
)

// stubRemoteStore implements only the surface allocateLocalBindPort
// touches; we build it inline so the test does not depend on the full
// gorm-backed store wiring (saving us a tempfile + migrate step).
type stubRemoteStore struct {
	nodes []model.RemoteNode
}

func (s *stubRemoteStore) ListRemoteNodes() ([]model.RemoteNode, error) {
	return s.nodes, nil
}

func TestLocalUIPort(t *testing.T) {
	cases := []struct {
		listen string
		want   int
		ok     bool
	}{
		{"127.0.0.1:18080", 18080, true},
		{":8080", 8080, true},
		{"0.0.0.0:65535", 65535, true},
		{"", 0, false},
		{"127.0.0.1", 0, false},
		{"127.0.0.1:0", 0, false},
		{"127.0.0.1:99999", 0, false},
	}
	for _, tc := range cases {
		got, err := localUIPort(tc.listen)
		if tc.ok && err != nil {
			t.Errorf("localUIPort(%q) unexpected err: %v", tc.listen, err)
			continue
		}
		if !tc.ok && err == nil {
			t.Errorf("localUIPort(%q) expected err, got nil", tc.listen)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("localUIPort(%q) = %d, want %d", tc.listen, got, tc.want)
		}
	}
}

func TestFallbackNodeName(t *testing.T) {
	if got := fallbackNodeName(nil); got != "frpdeck-node" {
		t.Errorf("nil cfg: got %q", got)
	}
	cfg := &config.Config{InstanceName: "  hq-node  ", Listen: ":18080"}
	if got := fallbackNodeName(cfg); got != "hq-node" {
		t.Errorf("instance name not honoured: got %q", got)
	}
	cfg = &config.Config{Listen: ":18080"}
	if got := fallbackNodeName(cfg); got != "frpdeck-18080" {
		t.Errorf("port-based fallback wrong: got %q", got)
	}
	cfg = &config.Config{Listen: "weird"}
	if got := fallbackNodeName(cfg); got != "frpdeck-node" {
		t.Errorf("invalid listen fallback: got %q", got)
	}
}

func TestAllocateLocalBindPort_SkipsTaken(t *testing.T) {
	store := &stubRemoteStore{
		nodes: []model.RemoteNode{
			{LocalBindPort: 9201},
			{LocalBindPort: 9202},
		},
	}
	port, err := allocateLocalBindPort(store)
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	if port < 9203 || port > 9499 {
		t.Fatalf("port %d not in expected range", port)
	}
}

func TestAllocateLocalBindPort_SkipsBound(t *testing.T) {
	// Reserve 9201 by holding it; alloc must skip it.
	l, err := net.Listen("tcp", "127.0.0.1:9201")
	if err != nil {
		t.Skip("port 9201 not available on this host, skipping bind test")
	}
	defer l.Close()
	store := &stubRemoteStore{}
	port, err := allocateLocalBindPort(store)
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	if port == 9201 {
		t.Fatalf("alloc returned bound port 9201")
	}
}

func TestPortFreeRoundtrip(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	if portFree(port) {
		t.Fatalf("port %d reported free while held", port)
	}
}
