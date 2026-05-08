# Changelog

FrpDeck 的所有重要变更都记录在这份文档里。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

> v1.0 之前，次版本号（`0.x.y` 中的 `x`）的提升表示新功能或较大调整，可能伴随小幅破坏性变更；修订号（`0.x.y` 中的 `y`）只做兼容修复。所有破坏性变更都会在本文件中显式标注 `BREAKING`。

> 项目仍处于活跃开发阶段，运行中数据库会通过 GORM AutoMigrate 自动迁移，但生产部署仍建议升级前备份 `frpdeck.db`。

## [Unreleased]

发布前会把开发分支上累积的变更归类到下方对应小节，并在打 tag 时切到正式版本号。

### Planned / Backlog

- 真机回归（macOS 托盘、Android arm64、群晖 SPK、飞牛 fpk、Wails Win 桌面）。
- v0.7.x 期 polish：access_token 黑名单与 TTL 缩短、`Tunnel.purpose` 字段、ntfy 推送事件类型扩展、RemoteNode reaper 事件驱动、audit prev_jti hash。
- v0.8 候选方向（待真机回归后定调）：配置版本管理（diff/rollback）、集群协同（多节点状态同步）、可观测性（Prometheus /metrics + Grafana 模板）。

完整 backlog 详见 [`plan.md` §14.2](./plan.md)。

## [0.7.2] - 2026-05-08

补齐 v0.7.1 release pipeline 中没出包的两类 polish 形态：Android APK 与 Wails 三平台桌面。

GitHub Release: <https://github.com/teacat99/FrpDeck/releases/tag/v0.7.2>

### Fixed

- **Android `gomobile bind` 失败**：`unable to import bind: no Go package in golang.org/x/mobile/bind`。原因是 `go.mod` 缺 `golang.org/x/mobile` 显式依赖（该 module 只被 `gomobile bind` 工具流隐式用到）；修复：`go get golang.org/x/mobile/bind@latest` + 新增 `mobile/frpdeckmobile/deps.go` 空导入 `_ "golang.org/x/mobile/bind"` 锚定 go.mod，避免后续 `go mod tidy` 把它当作"未使用"被 drop。
- **Wails 三平台 `wails build` 失败**：`no Go files in /home/runner/work/FrpDeck/FrpDeck`。原因是 Wails CLI 默认假设 cwd（项目根）就是 main.go 所在目录，而 FrpDeck 的桌面入口在 `cmd/server/`（与 headless / Wails / 托盘 cgo 桥接共用同一目录）；修复：`release.yml` `wails-desktop` job 在 `wails build` 前 `cd cmd/server`。Wails 会沿父级目录回找 `wails.json`，且其中的 `frontend:dir` / `wailsjsdir` 解析时相对 wails.json 自身位置而非 cwd，因此仓库根的 `wails.json` 与 `frontend/` 仍然按原样工作；只有 `Package artefact` step 中 `build/bin` 路径变成了 `cmd/server/build/bin`。

### Internal

- `go.mod` 新增 `golang.org/x/mobile v0.0.0-20260410095206-2cfb76559b7b`（连带 `golang.org/x/mod`、`golang.org/x/tools` 小版本上调）。
- 主线 commit / docker / NAS / CLI / Web UI / 协议层均无变化，与 v0.7.1 完全一致。

## [0.7.1] - 2026-05-08

**用户视角的首个可下载 release。** 内容等价 v0.7.0 + 一处 release CI 修复。

> v0.7.0 git tag 已 push 但 release pipeline 因 docker job 缺 Docker Hub 凭证（仓库未配 `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` secrets）22 秒内崩，4 个产物 jobs 直接 cancel；GitHub Release 页面与所有产物均未生成。v0.7.1 是 v0.7.0 之后第一个 release pipeline 跑通、产物真正可下载的版本。
>
> 严格遵循「不撤回已 push 的 tag」原则，v0.7.0 git ref 保留；后续语义等价的特性变更全部以 v0.7.1 为起点引用。

GitHub Release: <https://github.com/teacat99/FrpDeck/releases/tag/v0.7.1>

### Fixed

- `.github/workflows/release.yml` 的 `docker` job 在缺 `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` 时降级为仅推 GHCR；后续配置 secrets 时自动启用 Docker Hub mirror，不需再改 yml。
- `fnos-fpk` / `synology-spk` / `android-apk` / `wails-desktop` 4 个产物 job 去掉对 `docker` job 的 `needs:` 依赖，让 docker job 的稳定性问题不再阻塞 NAS / Wails / Android 出包。

### 同时包含 v0.7.0 段的全部 Added / Fixed / Internal

详见下方 v0.7.0 段。

## [0.7.0] - 2026-05-08 — *tag pushed, release pipeline failed*

> 这一版的 git tag 已 push 到远程，但 release pipeline 因 docker job 缺 Docker Hub 凭证 22 秒内失败，没有生成任何 GitHub Release artefact。用户视角的首个可下载 release 是 [v0.7.1](#071---2026-05-08)，内容等价 + 一处 CI 修复。
>
> 此段保留是为了让 git tag 与 CHANGELOG 段一一对应，不当作"已发布"语义。

FrpDeck 计划中的首个公开 Release。覆盖项目最初规划的 P0–P10 主线全部主干能力，构成一个可在 Docker / NAS / Linux service / Windows service / 桌面 / Android 上跑起来、并能通过独立 CLI 自救的多 frps 控制台。

### Highlights（v0.7.0 计划内容；实际通过 v0.7.1 发布）

- **多 frps 一等公民** + **临时隧道生命周期**两大核心理念落地，区别于现有「单 frps 桌面客户端」与「直接编辑 `frpc.toml`」两类方案。
- **6 种部署形态** 全部可用：Docker（amd64/arm64/armv7）、飞牛 fnOS Native fpk（x86 + ARM）、群晖 DSM 7 SPK（x86_64 + aarch64）、Linux systemd、Windows Service、Wails 桌面（Linux/macOS/Windows）、Android（5 ABI APK，含 universal）。
- **独立 `frpdeck` CLI**：Direct-DB + 本机 control socket 双通道，daemon 起不来时仍可重置密码、备份数据库、改 auth 模式；daemon 在线时可下发 reconcile / 远程代管邀请等 mutating RPC。
- **远程代管**：A 台 FrpDeck 通过 frp 自身的 stcp 通道管理 B 台，邀请码 / 令牌生命周期完整。
- **全形态 CI**：单一 git tag 即可触发 Docker / NAS / Wails / Android 全产物发版，`APP_VERSION` 全链透传保证版本号一致。

### Added

#### 核心 frpc 管理（P0 / P1）

- `internal/store` 基于 GORM + 纯 Go SQLite（`modernc.org/sqlite`），免 CGO 跨编译。
- `internal/frpcd`：`FrpDriver` 抽象 + 三种实现：
  - `EmbeddedDriver`：进程内 `client.Service`（默认；`github.com/fatedier/frp` v0.68.x，跟随上游）。
  - `SubprocessDriver`：拉起外部 `frpc` 二进制（兼容 v0.52+ TOML/YAML/JSON 配置）。
  - `MockDriver`：测试用内存驱动。
- `internal/lifecycle.Manager`：30s 周期对账 + 启动对账 + AfterFunc 主通道，三套 reconcile 路径让状态最终一致。
- Web UI：Vue 3 + Vite + TypeScript + shadcn-vue + Tailwind CSS 4，含 zh-CN / en-US 双语 + PWA。

#### 临时隧道生命周期（P4）

- `Tunnel.expire_at` 字段：到期自动停止，UI 倒计时与「行内续期」按钮。
- `tunnel_expiring` 事件提前通知（默认到期前 5 分钟）。

#### 远程代管 + 模板 + 导入 + 自检（P5）

- 远程代管：A/B 双节点 stcp 配对、邀请码（含二维码）、`mgmt_token` 撤销、节点状态汇总。
- 场景模板（10 个）：SSH、Web、RDP、socks5 visitor、stcp/xtcp 等。
- frps 配置助手：从已配的 Tunnel 反向推导 `frps.toml`。
- 连通性自检：DNS / TCP / frps 会话 / 本地服务四项检查。
- frpc 配置导入：把已有 `frpc.{toml,yaml,json}` 一次性迁进 FrpDeck。

#### Profile + SubprocessDriver（P8）

- `Profile`：一键切换当前启用的 Endpoint/Tunnel 组合，适合「家用 / 公司」「在家 / 在外」等多场景切换。
- `SubprocessDriver`：FrpDeck 后端可拖管外部 `frpc` 二进制，把内嵌 frp 与外部 frp 行为对齐。

#### Android（P6 / P6′ / P7′）

- 普通模式：gomobile bind + Jetpack Compose 壳 + ForegroundService + SAF 备份。
- WebView UI：复用 Web 前端代码，Android 壳通过 JavaScript Bridge 注入登录态与设置。
- VPN 能力：tun2socks + 配置驱动启停（仅当存在 socks5 visitor 类 Tunnel 时启用，避免无谓常驻 VPN）。
- 升级清旧 ServiceWorker、阻断 `src/` 残留 `.js`、WebView 调试常开。
- APK 5 文件发版：universal + arm64-v8a + armeabi-v7a + x86 + x86_64。

#### 独立 CLI（P10）

- `frpdeck` 子命令树：
  - 自救：`doctor`、`user passwd`、`db backup/restore`、`auth mode`。
  - CRUD：`endpoint`、`tunnel`、`profile`、`template`、`import`。
  - 运行时：`runtime get/set`。
  - 远程代管：`remote nodes/invite/refresh/revoke-token/revoke`。
  - 实时观测：`logs --follow`、`logs --since 5m`（daemon 端 1024 槽 ring buffer 回放）、`watch endpoints/tunnels [--once]`、`completion`、`doc man`。
- 控制通道：Unix domain socket（Linux/macOS）+ named pipe（Windows），权限 0600；命令包括 `ping`、`reconcile`、`reload_runtime`、`subscribe`、`invoke`（远程代管 mutating RPC 走 `invoke`）。
- 输出格式：`-o table | json | yaml`，便于脚本化与 jq/yq 流水线。

#### 部署形态（P2 / P3 / P9 / P1-D）

- Docker：amd64 / arm64 / armv7 多架构镜像，发布到 GHCR + Docker Hub。
- 飞牛 fnOS：Native 模式 `.fpk` 应用包，x86 与 ARM 双架构，应用中心一键安装。
- 群晖 DSM 7：自 host `.spk` 套件（x86_64 + aarch64）+ Container Manager 双轨说明。
- Linux systemd / Windows Service：基于 `kardianos/service`，`frpdeck-server install/start/stop/status` 一份代码三平台。
- Wails 桌面：Linux（webkit2gtk-4.1）/ macOS（universal，含 NSStatusItem 托盘）/ Windows（WebView2），CI 三平台同步发版。

#### 全形态 CI（v0.7.0 收尾）

- 单一 git tag 触发 5 个 release jobs：`docker` / `fnos-fpk` / `synology-spk` / `android-apk` / `wails-desktop`。
- `APP_VERSION=$GITHUB_REF_NAME` 全链透传：Docker label / fnOS manifest / 群晖 INFO / Android `versionName` / mobile `Version()` / Wails `main.appVersion` 完全同步。
- Wails / Android 两个 job 标 `continue-on-error`，避免单点失败拖累 NAS / Docker 链。
- `actionlint` workflow 守门：每次 PR / push 静态分析 GitHub Actions 配置 + 内嵌 shellcheck。

### Fixed

- `auth_mode=none` 时仍闪一下登录界面（前端路由守卫在拿到 `auth.required=false` 后立即 redirect）。
- `cmd/server/main_wails.go` 引用的 `appVersion` 变量在 P1-D 时期就因为 build tag 错配（声明在 `//go:build !wails` 文件里）一直链不上，本版本抽到无 tag 的 `cmd/server/version.go`。
- `frpdeck logs --since` 之前是 placeholder，本版本真正接到 daemon ring buffer。

### Internal

- `internal/remoteops`：把远程代管 mutating 业务（invite / refresh / revoke）抽成独立服务，HTTP API 与 control socket 共用一份实现。
- `internal/control`：协议从 5 个固定命令扩到 6 个，新增 `CmdInvoke` 作为通用 RPC 入口；`Response.Result json.RawMessage` 携带任意业务结构。
- `internal/frpcd.EventBus`：fan-out 旁加 1024 槽 ring buffer + `Replay(since)`；`FrpDriver` 接口增 `ReplayEvents(since)`。
- 默认 build 走 `CGO_ENABLED=0`，所有 NAS / Docker / Wails Linux 构建链统一从单一 source set 出二进制。

---

## 命名约定

- `vX.Y.Z` 是 Git tag；CI 在该 tag 推上时触发 `release.yml`。
- `0.x.y` 系列里：
  - `x` 上调 = 新功能或较大调整（例：`0.7 → 0.8`）。
  - `y` 上调 = 兼容修复（例：`0.7.0 → 0.7.1`）。
  - 破坏性变更默认 **不会** 在 `y` 跳。
- `Unreleased` 段会在每次打 tag 前清空、归并到新版本节。
