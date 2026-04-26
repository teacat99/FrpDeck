// Package frpshelper turns a FrpDeck Tunnel + Endpoint pair into actionable
// "what does my frps.toml need to look like" guidance. plan.md §7.1
// asks for a "frps 配置助手" that lowers the frp learning curve; the
// helper does this by inspecting the tunnel's `type / role / custom_domains
// / subdomain / token` and returning a structured `Advice` the UI can
// render (severity badges, copy-to-clipboard snippet, doc deeplinks).
//
// Layering note: this package is pure data. It must not perform any
// network I/O — that belongs to internal/diag. The output of Advise()
// is fully driven by the persisted Tunnel + Endpoint, so it is also
// safe to cache or render on the frontend without re-querying.
package frpshelper

import (
	"fmt"
	"sort"
	"strings"

	"github.com/teacat99/FrpDeck/internal/model"
)

// Severity ranks each advice item from "you must do this" to "FYI".
// Lowercase ASCII; doubles as i18n suffix on the frontend.
type Severity string

const (
	SeverityRequired    Severity = "required"
	SeverityRecommended Severity = "recommended"
	SeverityInfo        Severity = "info"
	SeverityWarn        Severity = "warn"
)

// Item is a single piece of advice the frontend renders as one row.
// Field/Value carry the structured form; Title/Detail hold the
// human-friendly text. DocURL deeplinks to the upstream gofrp.org
// section for that knob.
type Item struct {
	Severity Severity `json:"severity"`
	Field    string   `json:"field,omitempty"`
	Value    string   `json:"value,omitempty"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	DocURL   string   `json:"doc_url,omitempty"`
}

// Advice is the helper's full report for a (Endpoint, Tunnel) pair.
// `TomlSnippet` is a ready-to-paste fragment the user can drop into
// their frps.toml; `Caveats` collects bullet warnings that don't map
// onto a single config knob (e.g. "tokens must match").
type Advice struct {
	TunnelID    uint     `json:"tunnel_id"`
	EndpointID  uint     `json:"endpoint_id"`
	Items       []Item   `json:"items"`
	TomlSnippet string   `json:"toml_snippet"`
	Caveats     []string `json:"caveats,omitempty"`
}

// Advise inspects the tunnel + endpoint and returns the structured
// frps.toml guidance. Always returns a non-nil Advice.
//
// Rules implemented (based on frp v0.68 docs, plan.md §7.1):
//
//   - type=http with custom_domains  → recommend vhostHTTPPort
//   - type=https with custom_domains → recommend vhostHTTPSPort + cert
//   - type=tcpmux                    → recommend tcpmuxHTTPConnectPort
//   - type=stcp/xtcp/sudp            → token-match caveat; xtcp also
//                                      needs natHoleStunServer
//   - type=tcp/udp with remote_port  → allow_ports must cover it
//   - endpoint.Token / TLSEnable     → propagate to auth/tls section
//   - subdomain set                  → subdomain_host must be configured
func Advise(ep *model.Endpoint, t *model.Tunnel) *Advice {
	out := &Advice{
		TunnelID:   t.ID,
		EndpointID: ep.ID,
		Items:      []Item{},
	}

	typ := strings.ToLower(strings.TrimSpace(t.Type))
	role := strings.ToLower(strings.TrimSpace(t.Role))

	// --- Top-level transport / auth knobs from the endpoint --------
	if strings.TrimSpace(ep.Token) != "" {
		out.Items = append(out.Items, Item{
			Severity: SeverityRequired,
			Field:    "auth.token",
			Value:    "<must match endpoint token>",
			Title:    "frps 必须开启 auth.method=token 且 token 与本端点一致",
			Detail:   "frps 一旦设置了 token，未携带相同 token 的 frpc 会被立即拒绝。FrpDeck 的密钥不会被反向写入 frps，需要你手动同步。",
			DocURL:   "https://gofrp.org/zh-cn/docs/reference/configurations/auth/",
		})
	}
	if ep.TLSEnable {
		out.Items = append(out.Items, Item{
			Severity: SeverityRecommended,
			Field:    "transport.tls.force",
			Value:    "true",
			Title:    "frps 建议同步开启 TLS 强制",
			Detail:   "客户端已 tls_enable=true，启用 transport.tls.force 可拒绝任何 plain TCP 客户端，避免凭据明文泄露。",
			DocURL:   "https://gofrp.org/zh-cn/docs/reference/configurations/tls/",
		})
	}

	// --- Per-proxy-type guidance -----------------------------------
	switch typ {
	case "http":
		if hasAnyDomain(t) {
			out.Items = append(out.Items, Item{
				Severity: SeverityRequired,
				Field:    "vhostHTTPPort",
				Value:    "80",
				Title:    "frps 必须监听 HTTP vhost 端口",
				Detail:   "未启用 vhostHTTPPort 时，type=http 的代理无法被路由；80 是公网默认值，也可换成 8080 等内网端口配合反代。",
				DocURL:   "https://gofrp.org/zh-cn/docs/features/http-https/",
			})
		}
		if strings.TrimSpace(t.HTTPUser) != "" {
			out.Items = append(out.Items, Item{
				Severity: SeverityInfo,
				Title:    "Basic Auth 在 frpc 侧生效",
				Detail:   "用户/密码由 frpc 端注入到代理，frps 只做转发，无需额外配置。",
			})
		}
	case "https":
		if hasAnyDomain(t) {
			out.Items = append(out.Items, Item{
				Severity: SeverityRequired,
				Field:    "vhostHTTPSPort",
				Value:    "443",
				Title:    "frps 必须监听 HTTPS vhost 端口",
				Detail:   "type=https 走 SNI 路由；443 之外的端口仍然有效，反代后再 443→x 即可。",
				DocURL:   "https://gofrp.org/zh-cn/docs/features/http-https/",
			})
			out.Caveats = append(out.Caveats, "如果你想让 frps 直接终结 TLS（而不是把 TLS 交回内网服务），需要在 frps.toml 顶部配置 https2http 插件或者 'plugin = \"https2http\"'。多数家用场景下让内网服务自行处理 TLS 更稳。")
		}
	case "tcpmux":
		out.Items = append(out.Items, Item{
			Severity: SeverityRequired,
			Field:    "tcpmuxHTTPConnectPort",
			Value:    "1337",
			Title:    "type=tcpmux 必须监听 HTTP CONNECT 端口",
			Detail:   "tcpmux 通过 HTTP CONNECT 多路复用单个 TCP 端口；frps 必须显式指定该端口才能接受连接。",
			DocURL:   "https://gofrp.org/zh-cn/docs/features/tcpmux/",
		})
	case "tcp", "udp":
		if t.RemotePort > 0 {
			out.Items = append(out.Items, Item{
				Severity: SeverityRecommended,
				Field:    "allowPorts",
				Value:    fmt.Sprintf("%d", t.RemotePort),
				Title:    "建议把 remote_port 加入 frps allowPorts 白名单",
				Detail:   "默认情况下 frps 接受所有端口，但生产环境通常会用 allowPorts 限制可分配的远端口段。在白名单外的端口启动会被 frps 直接拒绝。",
				DocURL:   "https://gofrp.org/zh-cn/docs/reference/configurations/proxy/#allowports",
			})
		}
	case "stcp", "sudp":
		out.Caveats = append(out.Caveats, "stcp / sudp 仅借 frps 转发握手元数据，frps.toml 无需改动；只要保证 server 与 visitor 的 sk 一致即可。")
	case "xtcp":
		out.Items = append(out.Items, Item{
			Severity: SeverityRequired,
			Field:    "natHoleStunServer",
			Value:    "stun.easyvoip.com:3478",
			Title:    "xtcp 必须开启 NAT hole punching 协调",
			Detail:   "FrpDeck 推荐使用任意公开 STUN 服务（aliyun/Google 等），xtcp 握手期间 frps 仅负责协调，真正连接是 visitor ⟷ server 直连。",
			DocURL:   "https://gofrp.org/zh-cn/docs/features/xtcp/",
		})
		out.Caveats = append(out.Caveats, "xtcp 在双 NAT/对称 NAT 场景下可能直连失败，必要时回退 stcp。")
	}

	// --- Domain plumbing (applies to both http and https) ----------
	if (typ == "http" || typ == "https") && strings.TrimSpace(t.Subdomain) != "" {
		out.Items = append(out.Items, Item{
			Severity: SeverityRequired,
			Field:    "subdomainHost",
			Title:    "使用 subdomain 时必须设置 subdomainHost",
			Detail:   "frps 会用 subdomainHost 拼接出最终域名（例如 subdomain=blog + subdomainHost=example.com → blog.example.com）。",
			DocURL:   "https://gofrp.org/zh-cn/docs/features/http-https/#%E8%87%AA%E5%AE%9A%E4%B9%89%E5%9F%9F%E5%90%8D",
		})
	}

	// --- Visitor-side hint -----------------------------------------
	if role == "visitor" {
		out.Items = append(out.Items, Item{
			Severity: SeverityInfo,
			Title:    "Visitor 仅在 frpc 侧体现",
			Detail:   "Visitor 角色不在 frps.toml 写代理；frps 只需具备目标 server 已注册即可。如果 visitor 这端找不到对应 server，多半是 sk 不匹配或 server 还没启动。",
		})
	}

	// --- Health check transparency ---------------------------------
	if strings.TrimSpace(t.HealthCheckType) != "" {
		out.Items = append(out.Items, Item{
			Severity: SeverityInfo,
			Title:    "Health check 在 frpc 侧执行，frps 不需要配置",
			Detail:   "frps 看不到 health check 结果，只感知 frpc 的连接断开。",
		})
	}

	// --- Plugin notes ----------------------------------------------
	if p := strings.TrimSpace(t.Plugin); p != "" {
		out.Items = append(out.Items, Item{
			Severity: SeverityInfo,
			Title:    "frpc 插件 " + p + " 在客户端侧执行",
			Detail:   "插件代码运行在 frpc，frps 仅作为传输；与 type=tcp/http 的 frps.toml 要求一致。",
			DocURL:   "https://gofrp.org/zh-cn/docs/features/common/client-plugin/",
		})
	}

	out.Items = sortItems(out.Items)
	out.TomlSnippet = renderSnippet(ep, t, out.Items)
	return out
}

// hasAnyDomain reports whether the tunnel routes by domain (custom or
// subdomain). HTTP/HTTPS proxies need at least one of these to be
// useful — vhost ports otherwise have nothing to match against.
func hasAnyDomain(t *model.Tunnel) bool {
	return strings.TrimSpace(t.CustomDomains) != "" || strings.TrimSpace(t.Subdomain) != ""
}

// sortItems orders advice deterministically: required first, then
// recommended, info, warn. Within a severity bucket we keep insertion
// order so related fields stay grouped (e.g. vhostHTTPPort + subdomain).
func sortItems(items []Item) []Item {
	rank := map[Severity]int{
		SeverityRequired:    0,
		SeverityRecommended: 1,
		SeverityInfo:        2,
		SeverityWarn:        3,
	}
	indexed := make([]struct {
		i    int
		item Item
	}, len(items))
	for i, it := range items {
		indexed[i] = struct {
			i    int
			item Item
		}{i, it}
	}
	sort.SliceStable(indexed, func(a, b int) bool {
		ra, rb := rank[indexed[a].item.Severity], rank[indexed[b].item.Severity]
		if ra != rb {
			return ra < rb
		}
		return indexed[a].i < indexed[b].i
	})
	out := make([]Item, len(indexed))
	for i, w := range indexed {
		out[i] = w.item
	}
	return out
}

// renderSnippet emits a TOML fragment of just the new top-level keys
// the user should append to their frps.toml. Re-uses the structured
// `Field=Value` info collected during Advise so the snippet stays in
// lock-step with the advice items the UI shows.
//
// Output is deliberately minimal: only `bindPort` is fixed by the
// endpoint; everything else is derived from the advice items so the
// helper does not start opining on values that have no obvious
// "right" choice.
func renderSnippet(ep *model.Endpoint, _ *model.Tunnel, items []Item) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# frps.toml 建议附加片段（端点 %s）\n", ep.Name))
	b.WriteString(fmt.Sprintf("bindPort = %d\n", ep.Port))
	for _, it := range items {
		if it.Field == "" || it.Value == "" {
			continue
		}
		// Heuristic: numeric fields render bare; boolean too; strings
		// quoted. We also collapse nested keys into TOML dotted form
		// so users can paste verbatim.
		v := it.Value
		switch v {
		case "true", "false":
			fmt.Fprintf(&b, "%s = %s\n", it.Field, v)
		default:
			if isAllDigits(v) {
				fmt.Fprintf(&b, "%s = %s\n", it.Field, v)
			} else {
				fmt.Fprintf(&b, "%s = %q\n", it.Field, v)
			}
		}
	}
	return b.String()
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
