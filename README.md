# FrpDeck

> 多 frps、跨平台、带临时隧道生命周期管理的 frp 控制台。  
> 目标是让 frpc 管理从「手写配置 + 查文档」变成可视化、可脚本化、可自救的日常运维动作。

![status](https://img.shields.io/badge/status-active%20development-orange)
![license](https://img.shields.io/badge/license-MIT-blue)
![go](https://img.shields.io/badge/go-1.25%2B-00ADD8)
![frp](https://img.shields.io/badge/frp-v0.68.x-brightgreen)

> [!WARNING]
> FrpDeck 目前处于 **开发中（active development）**。核心功能已经按阶段落地，但 API、数据模型、发布形态仍可能在 v1.0 前调整。生产部署请固定镜像 tag / release 版本，并提前备份 SQLite 数据。

## 项目信息

| 项目 | 说明 |
|---|---|
| 定位 | 自托管场景下的 frpc 管理器 / 多 frps 控制台 |
| 当前状态 | 开发中，已完成 Web UI、服务化、Docker/NAS、远程代管、Android 基础、Profile、SubprocessDriver、独立 CLI 等主干能力 |
| 内嵌 frp | `github.com/fatedier/frp` v0.68.x，跟随上游稳定版 |
| 最低外部 frpc | v0.52.0（TOML/YAML/JSON 配置时代，不支持 INI 时代配置） |
| 默认数据目录 | Docker: `/data`；Linux service/CLI: `/var/lib/frpdeck`；可通过 `FRPDECK_DATA_DIR` 覆盖 |
| 默认认证 | Docker / service: `password`；桌面 / Android: `none` |
| 许可证 | [MIT](./LICENSE) |

## 项目介绍

FrpDeck 是一个面向个人服务器、NAS、家庭实验室和小团队内网穿透场景的 **frpc 控制甲板**。它把多个 frps 服务端抽象为 Endpoint，把每条 frp proxy/visitor 抽象为 Tunnel，并在此之上提供模板、导入、Profile、审计、自检、远程代管和 CLI 自救能力。

和传统 `frpc.ini` / 单 frps 桌面客户端相比，FrpDeck 的重点不是「替你启动一个 frpc」，而是管理一个会长期演进的 frp 环境：

- **多 frps 服务端是一等公民**：多个 Endpoint 可并存，适合不同地域中转、业务隔离和多账号环境。
- **临时隧道自动收口**：Tunnel 可设置到期时间，适合临时开放 SSH/RDP/Web 服务，过期自动停止。
- **配置助手与模板**：内置常用场景模板，并能从当前 Tunnel 反向推导 frps 侧配置。
- **远程代管**：两台 FrpDeck 可通过 stcp 建立互管通道，用邀请码/二维码配对。
- **脚本化运维与自救**：独立 `frpdeck` CLI 支持 Direct-DB + 本机 control socket，daemon 起不来时也能重置密码、备份数据库、排查状态。
- **一套后端，多种形态**：同一套 Go 后端 + Vue Web UI 覆盖 Docker/NAS、Linux systemd、Windows Service、Wails 桌面、Android WebView 壳。

## 功能概览

| 能力 | 状态 | 说明 |
|---|---|---|
| Endpoint / Tunnel CRUD | 可用 | 管理多个 frps 与所有主流 proxy/visitor 类型 |
| 临时隧道生命周期 | 可用 | `expire_at` 到期停止，启动/周期 reconcile |
| Profile 切换 | 可用 | 一键切换当前启用的 Endpoint/Tunnel 组合 |
| frpc.toml/yaml/json 导入 | 可用 | 从已有 frpc 配置迁移 |
| 场景模板 | 可用 | SSH、Web、RDP、socks5、stcp/xtcp 等常见场景 |
| 远程代管 | 可用 | stcp 配对、邀请码、token 撤销、节点状态 |
| 独立 CLI | 可用 | `frpdeck` 二进制，支持自救、CRUD、日志、watch、remote mutating RPC |
| Docker / NAS 部署 | 可用 | GHCR / Docker Hub 镜像，多架构 |
| Linux systemd / Windows Service | 可用 | `frpdeck-server install/start/stop/status` |
| 飞牛 fnOS 应用包 | 可用 | x86 + ARM `.fpk`，应用中心一键安装 |
| Android | 开发中 | WebView 复用 Web UI，VPN 能力按业务驱动 |
| 桌面 GUI | 开发中 | Wails 形态已接入，真机 polish 继续推进 |

## 快速部署

### Docker / NAS（推荐）

```bash
docker run -d --name frpdeck \
  -p 8080:8080 \
  -v frpdeck-data:/data \
  -e FRPDECK_ADMIN_PASSWORD='请改成强密码' \
  --restart unless-stopped \
  ghcr.io/teacat99/frpdeck:latest
```

访问 `http://<host>:8080`，使用 `admin` 和 `FRPDECK_ADMIN_PASSWORD` 登录。首次进入后建议立即修改管理员密码。

镜像：

- `ghcr.io/teacat99/frpdeck:latest`
- `teacat99/frpdeck:latest`

群晖 Container Manager、飞牛 OS、反向代理和 HTTPS 部署流程见 [`deploy/README.md`](./deploy/README.md)。

### Docker Compose

```yaml
services:
  frpdeck:
    image: ghcr.io/teacat99/frpdeck:latest
    container_name: frpdeck
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - frpdeck-data:/data
    environment:
      FRPDECK_ADMIN_PASSWORD: "请改成强密码"
      FRPDECK_AUTH_MODE: "password"

volumes:
  frpdeck-data:
```

### 飞牛 fnOS（fpk 应用包）

> 适用 fnOS >= 0.9.0，支持 x86_64 与 ARM 双架构

1. 从 [GitHub Releases](https://github.com/teacat99/FrpDeck/releases) 下载对应架构的 `.fpk`：
   - `com.teacat.frpdeck-<version>-x86.fpk`（飞牛 x86 NAS）
   - `com.teacat.frpdeck-<version>-arm.fpk`（飞牛 ARM NAS）
2. 飞牛桌面 → 应用中心 → 右上角「自定义安装」 → 上传 `.fpk`
3. 安装完成后桌面出现 FrpDeck 图标，点击直接打开 Web UI（默认端口 18080）
4. 用户配置目录在共享空间 `FrpDeck/`：`config.json` 改端口、`data/frpdeck.db` 是隧道数据库（可直接备份）

源码与打包脚本在 [`nas/fnos/`](./nas/fnos/)，发布说明见 [飞牛应用包 README](./nas/fnos/README.md)。

### Linux systemd

下载或构建 `frpdeck-server` 后安装为系统服务：

```bash
sudo /usr/local/bin/frpdeck-server install \
  --listen :8080 \
  --admin-username admin \
  --admin-password '请改成强密码' \
  --auth-mode password

sudo systemctl enable --now frpdeck
sudo systemctl status frpdeck
```

服务相关命令：

```bash
frpdeck-server install
frpdeck-server uninstall
frpdeck-server start
frpdeck-server stop
frpdeck-server restart
frpdeck-server status
frpdeck-server version
```

### 本机管理 CLI

`frpdeck-server` 是常驻 daemon；`frpdeck` 是独立 CLI。CLI 优先直接读写 SQLite，daemon 运行时再通过 `<data_dir>/frpdeck.sock` 通知运行时同步。

```bash
# 自救与诊断
frpdeck doctor
frpdeck user passwd admin
frpdeck db backup /tmp/frpdeck.db
frpdeck auth mode password

# Endpoint / Tunnel / Profile
frpdeck endpoint add --name nas --addr nas.example.com --port 7000 --token sekret
frpdeck tunnel add --endpoint nas --name ssh --type tcp --local-port 22 --remote-port 22022
frpdeck tunnel add --endpoint nas --name demo --type tcp --local-port 8080 --remote-port 18080 --duration 30m
frpdeck profile activate homelab

# 模板、导入、runtime 设置
frpdeck template list
frpdeck template apply ssh --endpoint nas --name homelab-ssh --remote-port 12022
frpdeck import frpc.toml --endpoint nas --default-on-conflict rename
frpdeck runtime set max_duration_hours 12

# 实时观测
frpdeck logs --follow
frpdeck watch tunnels

# 远程代管节点
frpdeck remote nodes list
frpdeck remote invite --endpoint nas --name laptop
frpdeck remote refresh laptop
frpdeck remote revoke-token laptop
frpdeck remote revoke laptop --yes

# 输出格式与补全
frpdeck endpoint list -o json
frpdeck completion bash > /etc/bash_completion.d/frpdeck
frpdeck doc man /usr/local/share/man/man1/
```

引用规则：`<id>`、大小写不敏感 `<name>`；Tunnel 名称二义时使用 `<endpoint>/<name>` 消歧。

## 配置

常用环境变量：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `FRPDECK_LISTEN` | `:8080` | HTTP/Web UI 监听地址 |
| `FRPDECK_DATA_DIR` | 形态相关 | SQLite、frpc 二进制、control socket 存放目录 |
| `FRPDECK_AUTH_MODE` | Docker/service: `password` | `password` / `ipwhitelist` / `none` |
| `FRPDECK_ADMIN_USERNAME` | `admin` | 首次启动时种子管理员用户名 |
| `FRPDECK_ADMIN_PASSWORD` | `passwd` | 首次启动时种子管理员密码，生产必须覆盖 |
| `FRPDECK_JWT_SECRET` | 自动生成 | JWT 签名密钥，集群/持久化部署建议固定 |
| `FRPDECK_INSTANCE_NAME` | 空 | 远程代管邀请码中显示的本机名称 |

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.25+、Gin、GORM、SQLite（pure Go） |
| frp 引擎 | `github.com/fatedier/frp` v0.68.x，EmbeddedDriver / SubprocessDriver / MockDriver |
| 前端 | Vue 3、Vite、TypeScript、Pinia、vue-i18n、shadcn-vue、Tailwind CSS 4 |
| CLI | Cobra、Direct-DB、Unix socket / Windows AF_UNIX control channel |
| 桌面 | Wails v2 |
| 服务化 | `kardianos/service`，支持 systemd / Windows Service |
| Android | gomobile、Jetpack、WebView、tun2socks |

## 开发

```bash
# 后端测试
go test ./...

# 后端构建
go build ./cmd/server
go build ./cmd/cli

# 前端构建
cd frontend
npm install
npm run build
```

开发注意：

- `web/web.go` 使用 `//go:embed all:dist`，Go 构建嵌入前需要先生成 `frontend/dist`。
- 数据库迁移通过 GORM AutoMigrate 完成。
- 默认使用内嵌 frpc；高级用户可切换到外部 `frpc` 二进制。

## 路线图

已完成主干：P0 脚手架、P1 桌面 MVP 主体、P2 服务化、P3 Docker/NAS、P4 生命周期、P5 远程代管/模板/导入/自检、P6/P7 Android 重写主线、P8 Profile/SubprocessDriver、P10 CLI。

后续重点：

- P9：群晖 SPK / 飞牛应用市场打包与分发。
- 桌面 GUI polish：真机验收、托盘细节、macOS 状态栏。
- Android 真机回归：WebView UI、自动登录、配置驱动 VPN。
- CLI polish：`logs --since` 历史回放、`watch --once`、更多脚本化输出细节。

## 与 PortPass 的关系

FrpDeck 与 [PortPass](https://github.com/teacat99/PortPass) 是姊妹项目，共享「Go 单二进制 + 嵌入 Vue + SQLite + shadcn-vue + PWA + lifecycle」工程范式：

- **PortPass**：临时放行端口，防火墙规则到期自动撤销。
- **FrpDeck**：临时架设 frp 隧道，Tunnel 到期自动停止。

两者解决的是不同层面的临时访问问题，可独立使用，也可配合使用。

## License

FrpDeck is licensed under the [MIT License](./LICENSE).
