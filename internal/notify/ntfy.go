// Package notify ships an asynchronous push notifier built on top of
// the ntfy.sh protocol. We deliberately speak just enough of the
// protocol (topic + optional auth + Title/Tags headers) to cover both
// the public ntfy.sh service and a self-hosted instance, without
// pulling in a dedicated SDK.
//
// The notifier is intentionally fire-and-forget: any failure (DNS,
// timeout, 5xx) is logged and dropped. The login flow MUST NOT block
// on push delivery because the operator's phone might be offline and
// the legitimate user is still waiting for their JWT.
package notify

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/teacat99/FrpDeck/internal/runtime"
)

// Ntfy implements auth.Notifier on top of an ntfy server. URL / topic
// / token are read live from runtime.Settings on every send so the
// operator can change them in the UI without restarting.
type Ntfy struct {
	rt     *runtime.Settings
	client *http.Client
}

// New builds a Ntfy with a sensible 5s timeout. Pass the same Settings
// object the API server uses so updates from PUT /api/runtime-settings
// take effect immediately.
func New(rt *runtime.Settings) *Ntfy {
	return &Ntfy{
		rt:     rt,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Notify is fire-and-forget: it spawns a goroutine to do the HTTP POST
// so callers (login handler) return to the user immediately.
func (n *Ntfy) Notify(title, body, tag string) {
	url := n.urlForPost()
	if url == "" {
		return // not configured
	}
	token := n.rt.NtfyToken()
	go n.send(url, token, title, body, tag)
}

// urlForPost composes the destination URL: NtfyURL must be the server
// root (e.g. "https://ntfy.sh") and NtfyTopic the topic name. We tolerate
// an URL that already contains the topic by detecting a non-empty path.
// Returns "" when not enough config is present to attempt a send.
func (n *Ntfy) urlForPost() string {
	base := strings.TrimRight(strings.TrimSpace(n.rt.NtfyURL()), "/")
	if base == "" {
		return ""
	}
	topic := strings.Trim(strings.TrimSpace(n.rt.NtfyTopic()), "/")
	if topic == "" {
		// If the operator already pasted "https://host/topic", honour
		// it as-is. We cannot tell the difference between "no path"
		// and "path is the topic" without a heuristic; prefer not to
		// double-append.
		if strings.Contains(base, "://") && strings.Count(strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://"), "/") >= 1 {
			return base
		}
		return ""
	}
	return base + "/" + topic
}

func (n *Ntfy) send(url, token, title, body, tag string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		log.Printf("[ntfy] build request: %v", err)
		return
	}
	if title != "" {
		req.Header.Set("Title", title)
	}
	if tag != "" {
		req.Header.Set("Tags", tag)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		log.Printf("[ntfy] post %s: %v", url, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Printf("[ntfy] %s -> %s", url, resp.Status)
	}
}

// Test sends a one-off "FrpDeck · 测试通知" message synchronously and
// returns the resulting error, if any. Used by the "测试" button in
// Settings > Notifications so the operator gets immediate feedback.
func (n *Ntfy) Test() error {
	url := n.urlForPost()
	if url == "" {
		return errEmptyConfig
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewBufferString("FrpDeck 通知配置成功 / Notification channel verified"))
	if err != nil {
		return err
	}
	req.Header.Set("Title", "FrpDeck · 测试通知")
	req.Header.Set("Tags", "white_check_mark")
	if token := n.rt.NtfyToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromStatus(resp.Status)
	}
	return nil
}

// Sentinel errors avoid pulling fmt for the common cases.
var (
	errEmptyConfig = httpErr("ntfy URL/topic not configured")
)

type httpErr string

func (e httpErr) Error() string { return string(e) }

func errFromStatus(s string) error { return httpErr("ntfy responded " + s) }
