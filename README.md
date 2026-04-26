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
