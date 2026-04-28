package cmds

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teacat99/FrpDeck/internal/control"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

// resolveEndpoint accepts either a numeric ID or a (case-insensitive)
// name and returns the matching endpoint. We support both because most
// CLI usage thinks in names ("frpdeck endpoint enable nas") while
// scripts that piped through `endpoint list -o json | jq` already
// have IDs in hand.
//
// Disambiguation: a purely numeric token is always treated as ID. A
// name that happens to start with a digit but contains non-digits is
// treated as name. Names are matched case-insensitively because the
// underlying database collation is binary on SQLite (case-sensitive)
// and operators routinely capitalise the same endpoint differently
// across help docs / chat / shell history.
func resolveEndpoint(st *store.Store, ref string) (*model.Endpoint, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("endpoint reference required")
	}
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		ep, err := st.GetEndpoint(uint(id))
		if err != nil {
			return nil, err
		}
		if ep == nil {
			return nil, fmt.Errorf("endpoint id=%d not found", id)
		}
		return ep, nil
	}
	all, err := st.ListEndpoints()
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(ref)
	var hit *model.Endpoint
	for i := range all {
		if strings.ToLower(all[i].Name) == low {
			if hit != nil {
				return nil, fmt.Errorf("endpoint name %q matches multiple rows; use ID instead", ref)
			}
			hit = &all[i]
		}
	}
	if hit == nil {
		return nil, fmt.Errorf("endpoint %q not found", ref)
	}
	return hit, nil
}

// resolveTunnel mirrors resolveEndpoint for tunnels. Names within an
// endpoint are unique, but the same name can appear under different
// endpoints — to disambiguate, callers may pass `<endpoint>/<name>`
// (e.g. "nas/ssh") or accept the "first match wins" fallback when
// only the bare name is given.
func resolveTunnel(st *store.Store, ref string) (*model.Tunnel, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("tunnel reference required")
	}
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		t, err := st.GetTunnel(uint(id))
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, fmt.Errorf("tunnel id=%d not found", id)
		}
		return t, nil
	}
	var endpointFilter *model.Endpoint
	name := ref
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		var err error
		endpointFilter, err = resolveEndpoint(st, ref[:i])
		if err != nil {
			return nil, err
		}
		name = ref[i+1:]
	}
	low := strings.ToLower(name)
	var rows []model.Tunnel
	var err error
	if endpointFilter != nil {
		rows, err = st.ListTunnelsByEndpoint(endpointFilter.ID)
	} else {
		rows, err = st.ListTunnels()
	}
	if err != nil {
		return nil, err
	}
	var hit *model.Tunnel
	for i := range rows {
		if strings.ToLower(rows[i].Name) == low {
			if hit != nil {
				return nil, fmt.Errorf("tunnel name %q is ambiguous (matches multiple endpoints); use <endpoint>/<name> or numeric id", ref)
			}
			hit = &rows[i]
		}
	}
	if hit == nil {
		return nil, fmt.Errorf("tunnel %q not found", ref)
	}
	return hit, nil
}

// resolveProfile mirrors resolveEndpoint for profiles.
func resolveProfile(st *store.Store, ref string) (*model.Profile, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("profile reference required")
	}
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		p, err := st.GetProfile(uint(id))
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, fmt.Errorf("profile id=%d not found", id)
		}
		return p, nil
	}
	all, err := st.ListProfiles()
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(ref)
	var hit *model.Profile
	for i := range all {
		if strings.ToLower(all[i].Name) == low {
			if hit != nil {
				return nil, fmt.Errorf("profile name %q matches multiple rows; use ID instead", ref)
			}
			hit = &all[i]
		}
	}
	if hit == nil {
		return nil, fmt.Errorf("profile %q not found", ref)
	}
	return hit, nil
}

// pingReconcile is the canonical "I just changed something the daemon
// cares about, please re-run lifecycle.Reconcile" call. It silently
// no-ops when the daemon is not running so CLI commands continue to
// succeed in the offline-administration path. Real socket errors
// still bubble up because they indicate a misconfigured deployment
// (e.g. wrong --data-dir, daemon dead but socket file lingering).
func pingReconcile(c *control.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := c.Reconcile(ctx)
	if err == nil || errors.Is(err, control.ErrDaemonNotRunning) {
		return nil
	}
	return fmt.Errorf("notify daemon: %w", err)
}

// pingReloadRuntime is the runtime-settings equivalent of
// pingReconcile: same offline semantics, different command.
func pingReloadRuntime(c *control.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := c.ReloadRuntime(ctx)
	if err == nil || errors.Is(err, control.ErrDaemonNotRunning) {
		return nil
	}
	return fmt.Errorf("notify daemon: %w", err)
}
