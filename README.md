# FrpDeck

> 多服务端、跨平台、带临时隧道生命周期管理的 frp 控制甲板 — 让 frp 配置不再是查文档马拉松。

[English](./README.en.md) | 中文

## 这是什么

FrpDeck 是一个 **frp 客户端管理器**，目标用户是自托管 / 个人服务器场景下使用 frp 做内网穿透的开发者与运维。

它和 [frpc-desktop](https://github.com/luckjiawei/frpc-desktop) 类项目的核心区别：

| 维度 | frpc-desktop | **FrpDeck** |
|---|---|---|
| 多 frps 服务端 | ❌ 仅单 frps | ✅ 任意多 frps，按 endpoint 组织 |
| 临时隧道（到期自动停） | ❌ | ✅ 与 [PortPass](https://github.com/teacat99/PortPass) 同源的生命周期机制 |
| Headless / NAS / Docker | ❌ Electron 必须有桌面 | ✅ 单二进制 + Docker，群晖/飞牛/无桌面 Linux 均可 |
| Win 系统服务 | ❌ | ✅ 一键 install/start/stop |
| 远程代管对端 FrpDeck | ❌ | ✅ 通过 stcp 互相管理（P5） |
| Android | ❌ | ✅ 普通模式 + 可选 VPN 模式（P6+） |
| 配置助手 | ❌ | ✅ 反向推导 frps 配置 / 内置场景模板 |
| UI | Electron Vue | shadcn-vue + Tailwind 4 + PWA |
| 安装包大小 | ~80–150 MB | ~20 MB（外部 WebView2） |
| 常驻内存 | 200–400 MB | ~40–60 MB |

## 设计哲学

1. **配置即解释**：每个 frp 字段都附带"它在做什么 / frps 那边要怎么配"的提示，把分散的官方文档**反向汇总到 UI 里**。
2. **临时业务自动收口**：开个 RDP 给同事看半小时，到点自动关——不需要手动来停。
3. **一份代码，N 个发布形态**：Win/Linux 桌面、Win Service、systemd、Docker、Android，全平台共用同一套后端 + 同一套 Web UI。
4. **多服务端是一等公民**：复杂场景里多台 frps 协同（不同地域中转 / 不同业务隔离）是常态，不是特例。

## 状态

🚧 **早期开发中**。当前进度见 GitHub Issues / Releases。

## 核心特性（规划中，按 Phase 推进）

- **多 frps 服务端**：任意多个 endpoint，每个独立的 `*client.Service` 实例运行
- **临时隧道**：每条隧道可选 `expire_at`，到期自动停（精确 `time.AfterFunc` + 30s 周期对账 + 启动对账）
- **场景模板**：内置常用场景（暴露 Web / RDP / xtcp p2p / 内网穿透 socks5 等），一键生成
- **frps 配置助手**：根据当前隧道反向推导 frps.toml 应该怎么配，可复制
- **导入 frpc.toml**：从已有 TOML 一键导入，最低支持 frp v0.52.0
- **远程代管**：两台 FrpDeck 通过 stcp 互相管理，扫码配对（P5）
- **多用户 + JWT**：仅在服务化 / Docker 部署时启用，桌面默认无登录
- **审计日志**：所有变更可追溯
- **PWA + 中英双语 + 移动端响应式**

## 内嵌 frp 版本

| FrpDeck 版本 | 内嵌 frp 版本 |
|---|---|
| v0.x（开发中） | v0.68.x（始终跟随 frp 主线最新稳定版） |

如需用其他 frp 版本编译，见 [docs/build-with-custom-frp.md](./docs/build-with-custom-frp.md)。

外部 frpc 二进制挂载模式下，最低支持 **frp v0.52.0**（INI 配置废弃后的第一个版本）。

## 快速开始

### Docker / NAS（推荐，v0.1.x 已可用）

```bash
docker run -d --name frpdeck \
  -p 8080:8080 \
  -v frpdeck-data:/data \
  -e FRPDECK_ADMIN_PASSWORD='请改成强密码' \
  --restart unless-stopped \
  ghcr.io/teacat99/frpdeck:latest
```

打开 `http://<host>:8080`，用 `admin` + 上面密码登录后立即在 UI 改密码。

群晖 Container Manager / 飞牛 OS / 反代 + HTTPS 的完整部署流程见
[`deploy/README.md`](./deploy/README.md)。镜像同时发布到
`ghcr.io/teacat99/frpdeck` 与 `teacat99/frpdeck`，多 arch（amd64 + arm64）。

### Linux systemd（v0.1.x 已可用）

```bash
# 1. 拿到 release 下载或 docker cp 出来的二进制
sudo /usr/local/bin/frpdeck-server install \
  --listen :8080 \
  --admin-username admin \
  --admin-password '请改成强密码' \
  --auth-mode password
sudo systemctl enable --now frpdeck
sudo systemctl status frpdeck
```

服务相关子命令：`install / uninstall / start / stop / restart / status / version`。

### `frpdeck` CLI（v0.2.x，独立二进制）

`frpdeck-server` 是常驻进程；与之配套的还有一个独立的 **本机管理 CLI**
`frpdeck`，专为脚本化运维 / daemon 起不来时的自救场景设计。两个二进制
完全独立分发，按需安装。

CLI 默认走 Direct-DB（`/var/lib/frpdeck/frpdeck.db`，可用 `--data-dir` 覆盖）
+ 本机 Unix socket 控制通道（`<data_dir>/frpdeck.sock`，0600 权限）。
所有 mutating 命令做完 SQLite 写后会 best-effort ping 一下 daemon
触发 `lifecycle.Reconcile()`，daemon 没在跑就静默继续——SQLite 写已落库，
下次 daemon 启动自动看到。

```bash
# 自救（daemon 起不来时也能用）
frpdeck doctor                          # 4 项检查：data-dir / db / 控制通道 / frpc 二进制
frpdeck user passwd admin               # 直接改 SQLite 重置密码
frpdeck auth mode password              # 切换认证模式（修改 env 文件，需要 systemctl restart）
frpdeck db backup /tmp/before.db        # SQLite 在线热备份
frpdeck db restore /tmp/before.db       # 还原（daemon 在跑时拒绝，--force 跳过）

# 全量 CRUD（daemon 在跑时立即生效）
frpdeck endpoint add --name nas --addr nas.example.com --port 7000 --token sekret
frpdeck tunnel add --endpoint nas --name ssh --type tcp --local-port 22 --remote-port 22022
frpdeck tunnel add --endpoint nas --name demo --duration 30m \
  --type tcp --local-port 8080 --remote-port 18080      # 临时隧道
frpdeck tunnel extend demo --duration 1h                # 续期
frpdeck profile add --name homelab --bind-tunnel ssh --bind-endpoint office
frpdeck profile activate homelab                        # 一键切换 active set

# 模板 / 导入 / runtime 设置
frpdeck template list
frpdeck template apply ssh --endpoint nas --name homelab-ssh --remote-port 12022
frpdeck import frpc.toml --endpoint nas --default-on-conflict rename
frpdeck runtime set max_duration_hours 12               # 写 KV + ping reload

# 实时观测
frpdeck logs --follow                                   # 流式日志，ANSI 颜色，Ctrl+C 退出
frpdeck logs --type log --tunnel ssh --follow           # 带过滤
frpdeck watch tunnels                                   # 5s 刷新 tunnel 表

# 远端节点（P10-C 仅 list/get；invite/refresh/revoke 走 Web UI，P10-D 补 socket 路径）
frpdeck remote nodes list
frpdeck remote nodes get <id|name>

# Shell 补全 / man page
frpdeck completion bash > /etc/bash_completion.d/frpdeck
frpdeck completion zsh  > "${fpath[1]}/_frpdeck"
frpdeck completion fish > ~/.config/fish/completions/frpdeck.fish
frpdeck doc man /usr/local/share/man/man1/

# 输出格式（脚本友好）
frpdeck endpoint list -o json | jq '.[] | select(.enabled == true)'
frpdeck tunnel list   -o yaml --no-headers
```

引用形式：`<id>` / 大小写不敏感 `<name>` / 二义 tunnel 用 `<endpoint>/<name>` 消歧。
完整子命令清单 `frpdeck --help`，每个子命令均带 `--help` 与生成的 man page。

### Windows / macOS 桌面 GUI（Wails，v0.1.1 polish 中）

🚧 P1-D 代码已完成（headless / 系统服务路径已稳定），桌面 GUI 真机验收 + macOS 托盘
（NSStatusItem 重写）进入 v0.1.1 polish。当前可以用 `wails build` 自行构建尝鲜。

## 与 PortPass 的关系

FrpDeck 与 [PortPass](https://github.com/teacat99/PortPass) 是姊妹项目，共享底层工程范式（Go 单二进制 + 嵌入 Vue + SQLite + shadcn-vue + PWA + 同源 lifecycle 设计）：

- **PortPass** = 临时**放行端口**（防火墙规则到期自动撤销）
- **FrpDeck** = 临时**架设隧道**（frp 隧道到期自动停止）

两者解决的是不同侧面的"临时性"问题，可独立使用，也可配合使用。

## License

[MIT](./LICENSE)
