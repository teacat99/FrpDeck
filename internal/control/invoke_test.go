package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestInvokeRoundTrip exercises the happy path: a typed args struct
// encodes into Args["body"], the server-side handler decodes it,
// returns a JSON-encoded result, and the client receives the raw
// bytes back via the Response.Result field.
func TestInvokeRoundTrip(t *testing.T) {
	c, _ := newTestServer(t, Handlers{
		Invoke: func(_ context.Context, method string, body json.RawMessage) (json.RawMessage, error) {
			if method != "echo" {
				return nil, errors.New("unexpected method " + method)
			}
			var in struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(body, &in); err != nil {
				return nil, err
			}
			out := map[string]string{"echoed": in.Msg + "!"}
			return json.Marshal(out)
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := json.Marshal(map[string]string{"msg": "hello"})
	res, err := c.Invoke(ctx, "echo", req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var out map[string]string
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if out["echoed"] != "hello!" {
		t.Fatalf("echoed = %q", out["echoed"])
	}
}

// TestInvokeNoHandler confirms that an old daemon (no Invoke
// handler) returns the documented "no handler" sentinel rather than
// nil-derefing on the dispatch path.
func TestInvokeNoHandler(t *testing.T) {
	c, _ := newTestServer(t, Handlers{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Invoke(ctx, "any.method", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no handler") {
		t.Fatalf("unexpected error %v", err)
	}
}

// TestInvokeMethodRequired enforces that a caller cannot send an
// empty method (the server has a defensive check; the client
// itself does not validate, so this is the safety net).
func TestInvokeMethodRequired(t *testing.T) {
	c, _ := newTestServer(t, Handlers{
		Invoke: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			t.Fatal("handler must not be called when method is empty")
			return nil, nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Invoke(ctx, "", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error for empty method")
	}
	if !strings.Contains(err.Error(), "method required") {
		t.Fatalf("unexpected error %v", err)
	}
}

// TestInvokeSurfacesHandlerError checks that a typed handler error
// reaches the caller verbatim — handlers shape their own messages
// for human consumption, the protocol must not mangle them.
func TestInvokeSurfacesHandlerError(t *testing.T) {
	c, _ := newTestServer(t, Handlers{
		Invoke: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, errors.New("widget exploded")
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Invoke(ctx, "boom", []byte(`{}`))
	if err == nil || err.Error() != "widget exploded" {
		t.Fatalf("expected verbatim error, got %v", err)
	}
}

// TestInvokeAllowsLongerCeiling makes sure an Invoke handler that
// takes a couple of seconds is not killed by the per-RPC 5s
// deadline reserved for the cheap commands. We sleep just past 5s
// to confirm the dispatch upgrade to 30s actually takes effect.
func TestInvokeAllowsLongerCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("long-running deadline test; -short skips")
	}
	c, _ := newTestServer(t, Handlers{
		Invoke: func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			select {
			case <-time.After(6 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return json.Marshal("done")
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := c.Invoke(ctx, "slow", nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var got string
	if err := json.Unmarshal(res, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "done" {
		t.Fatalf("unexpected payload %q", got)
	}
}
