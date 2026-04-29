# FrpDeck — 群晖 DSM 7 部署

> 本目录提供 **SPK 套件** 与 **Container Manager 双轨部署** 两种路径，用户按 NAS 习惯任选。

## 推荐路径速查

| 你的情况 | 推荐路径 | 原因 |
|---|---|---|
| DSM 7.2+ 已经在用 Container Manager 跑 Docker | **Container Manager**（见下文「方案 B」） | 零额外学习成本，复用主仓库 `docker-compose.yaml` |
| 想要套件中心一键安装 + 桌面图标体验 | **SPK 套件**（见下文「方案 A」） | DSM 原生套件形态，开机自启、Web 入口由 DSM 代管 |
| 需要在多台群晖批量部署 | Container Manager + 自建 compose 仓 | 配置文件集中管理 |

---

## 方案 A：SPK 套件

### 目录布局

```text
nas/synology/
├── README.md            # 本文件
├── build.sh             # 双架构 SPK 打包脚本
└── frpdeck/             # SPK 源
    ├── INFO.template    # platform / version 由 build.sh 动态注入
    ├── PACKAGE_ICON.PNG       # 64×64
    ├── PACKAGE_ICON_256.PNG   # 256×256
    ├── LICENSE          # MIT
    ├── scripts/
    │   ├── start-stop-status  # 主启停（DSM 7 必需）
    │   ├── preinst / postinst
    │   ├── preuninst / postuninst
    │   └── preupgrade / postupgrade
    └── conf/
        ├── privilege    # JSON：run-as: package（用 sc-frpdeck 用户跑）
        └── resource     # JSON：data-share FrpDeck（用户存自己的 frpc.toml 用）
```

### 构建产物（不入 git）

```text
nas/synology/dist/
├── frpdeck-x86_64-<version>.spk     # x64 群晖（绝大多数家用 NAS）
└── frpdeck-aarch64-<version>.spk    # ARM 群晖（DS220+/ DS418j 等）
```

> 群晖架构对照表：[Synology 各机型架构](https://help.synology.com/developer-guide/appendix/platarchs.html)。本仓库目前覆盖的两套是覆盖度最高的主流，群晖 88f6281/armv7 老机型暂未支持。

### 本地构建

```bash
# 全套（x86_64 + aarch64，含前端）
bash nas/synology/build.sh

# 单架构
bash nas/synology/build.sh x86_64
bash nas/synology/build.sh aarch64

# 跳过前端构建（开发时反复打包用）
FRPDECK_SKIP_FRONTEND_BUILD=1 bash nas/synology/build.sh

# 显式版本号（覆盖默认 0.7.0-1）
VERSION=v0.7.0 bash nas/synology/build.sh
```

`build.sh` 自动把 `VERSION=v0.7.0` 这种 git 风格转成 DSM 7 INFO 要求的 `0.7.0-1`（`[feature]-[build]` 形态），同时保留 `v` 前缀传给 Go ldflags 注入二进制。

### CI 自动产线

`.github/workflows/release.yml` 已加 `synology-spk` job：

1. push tag `v*` 触发
2. `docker` job 完成后启动
3. 产 `frpdeck-{x86_64,aarch64}-<version>.spk` 上传 GitHub Release

### 安装到群晖

> DSM 7.2+ 默认会拒绝来路不明的第三方套件。需要先放开信任级别。

1. **放开第三方源**：DSM 桌面 → 套件中心 → 设置 → 套件来源 → 信任级别选「Synology Inc 及受信任的发行者」（不要选「任何发行者」，那个安全等级太低）
2. **下载 SPK**：从 [GitHub Releases](https://github.com/teacat99/FrpDeck/releases) 下载对应架构的 `.spk`（x64 NAS 选 `-x86_64.spk`，ARM NAS 选 `-aarch64.spk`）
3. **手动安装**：套件中心 → 右上角「手动安装」 → 选刚下载的 `.spk`
4. 安装过程中会弹出第三方套件警告 → 同意 → DSM 自动建 `sc-frpdeck` 用户、target / var / etc 目录
5. 安装完成后 DSM 桌面出现 FrpDeck 图标，点击直接打开 Web UI（默认端口 18080）
6. **首次登录**：默认 `password` 模式，按引导设置管理员账号 → 登录主界面 → 添加 frps Endpoint

### 用户配置

| 路径 | 内容 | 卸载行为 |
|---|---|---|
| `/var/packages/frpdeck/target/bin/frpdeck-server` | 主二进制 | 卸载即删 |
| `/var/packages/frpdeck/var/config.json` | `listen_port` 配置（用户可改）| 保留 |
| `/var/packages/frpdeck/var/data/frpdeck.db` | 隧道 / Endpoint / audit DB | 保留 |
| `/var/packages/frpdeck/var/upgrade-backup/frpdeck.db.<旧版本>` | 升级前快照 | 保留 |

要改监听端口：File Station 编辑 `/var/packages/frpdeck/var/config.json`，改 `listen_port` 字段，然后套件中心「停止 → 启动」。

> 数据库放在 var/ 而不是放进 data-share 是有意为之 —— 群晖 DSM 7 沙箱限制 sc-frpdeck 不能写共享文件夹除非显式声明。如果未来需要让用户从 File Station 可见 frpdeck.db，会通过 `conf/resource` 的 `data-share` 暴露。

### 卸载

套件中心 → FrpDeck → 卸载。DSM 自动清 `target/` `tmp/` `home/` `etc/`，**保留 `var/`**（包含 `frpdeck.db`），重装后隧道配置自动恢复。要彻底清，SSH 登录 NAS 后执行：

```bash
sudo rm -rf /var/packages/frpdeck/var
```

### 升级

下一版 SPK 直接「手动安装」覆盖即可。`scripts/preupgrade` 已自动备份 `frpdeck.db` 到 `var/upgrade-backup/`，万一新版迁移失败可手动回滚。

---

## 方案 B：Container Manager（DSM 7.2+ 推荐）

> Container Manager 是 DSM 7.2+ 自带的 Docker Compose UI（旧称 Docker 套件）。如果你的群晖已经在用它跑别的容器，FrpDeck 的现成 `docker-compose.yaml` 可以直接复用。

### 步骤

1. 套件中心装好 Container Manager（DSM 7.2+ 通常默认已装）
2. SSH 到 NAS（或通过 File Station 上传文件）：
   ```bash
   sudo mkdir -p /volume1/docker/frpdeck/data
   sudo chown -R 10001:10001 /volume1/docker/frpdeck
   ```
3. 把仓库根目录的 `docker-compose.yaml` 复制到 `/volume1/docker/frpdeck/docker-compose.yaml`，按需调整：
   ```yaml
   services:
     frpdeck:
       image: ghcr.io/teacat99/frpdeck:latest
       container_name: frpdeck
       restart: unless-stopped
       ports:
         - "18080:8080"
       volumes:
         - ./data:/data
       environment:
         FRPDECK_LISTEN: ":8080"
         FRPDECK_DATA_DIR: "/data"
         FRPDECK_FRPCD_DRIVER: "embedded"
         FRPDECK_AUTH_MODE: "password"
   ```
4. Container Manager → 项目 → 新增 → 路径选 `/volume1/docker/frpdeck/` → 系统自动 detect compose 文件 → 启动
5. 浏览器访问 `http://<NAS_IP>:18080/` → 走管理员引导

### Container Manager vs SPK 选哪个

| 维度 | SPK 套件 | Container Manager |
|---|---|---|
| 安装路径 | 套件中心一键 | 需准备 compose 文件 + Container Manager 项目 |
| 桌面图标 | DSM 自动生成 | 无（要从浏览器收藏夹手动收） |
| 升级路径 | 手动下载新 SPK 覆盖 | `docker compose pull && docker compose up -d` |
| 资源占用 | 单进程，约 50 MB RSS | 含 Docker overhead，多 30-50 MB |
| 多实例 | 不支持（DSM 套件单例约束） | 支持（多 compose 项目） |
| 备份 | DSM Hyper Backup 自动 | 手工备份 `/volume1/docker/frpdeck/data` |

---

## 设计要点

### 为什么不用 spksrc

- spksrc 是基于 Makefile 的 cross-compile 农场，需要装一整套 Synology toolchain（aarch64-7.0、x64-7.0、armv7-7.0、...），单 host 体积巨大
- 我们的 frpdeck-server 已经走 `CGO_ENABLED=0 + Go 1.25` 纯 Go 路径，所有 GOARCH 直接 cross-compile，无需 toolchain
- spksrc 还要求按它的 Makefile 体系组织源码，本仓库的目录结构跟它不兼容
- spksrc 的核心价值是「PR 进 SynoCommunity 仓库统一分发」，但当前 P9-B 决策是自 host，不走社区分发

### 为什么不用 PkgCreate.py

- PkgCreate.py 在 Synology 官方 Package Toolkit 里，需要 DSM 系统 / 模拟器才能跑
- SPK 实际就是「外层 tar + 内层 tar.gz + 几个 sidecars」，Python 标准库 `tarfile` 完全可写
- 关键控制点：scripts/* 必须 0o755 + INFO 要在 tar 第一个位置（DSM stat 优化）+ inner tar.gz 用稳定 mtime（reproducible build）

### 为什么 service_port = 18080

- 8080 容易跟 DSM 的反代 / nginx / 老 Web Station 撞车
- 18xxx 段是 DSM 套件社区约定的「第三方应用首选段」（例如 Synology 自家 Audio Station 用 18002，自建套件多在 18000-18999）
- 用户随时可在 File Station 改 `var/config.json` 里的 `listen_port`

### 与 P9-A 飞牛包的关系

| 维度 | P9-A 飞牛 fpk | P9-B 群晖 SPK |
|---|---|---|
| 包格式 | zip | 外层 tar + 内层 tar.gz |
| 启停脚本 | `cmd/main {start\|stop\|status}` | `scripts/start-stop-status {start\|stop\|status\|log}` |
| 生命周期 | 9 个钩子（cmd/install_init...config_callback） | 6 个钩子（preinst/postinst/preuninst/postuninst/preupgrade/postupgrade） |
| 环境变量 | `TRIM_*` 系列 | `SYNOPKG_*` 系列 |
| 数据目录 | `${TRIM_DATA_SHARE_PATHS%%:*}` 共享空间 | `${SYNOPKG_PKGVAR}` 包私有 |
| 用户体系 | manifest 内声明，安装时建 `frpdeck` 用户 | DSM 自动建 `sc-frpdeck` 用户 |
| 上架审核 | 飞牛开发者平台审核 | 自 host（GitHub Release），不走 SynoCommunity |

两个 NAS 端走同一套 frpdeck-server 二进制 + 同一份 web/dist embed，差异仅在外壳。

---

## 相关文件

- `/nas/synology/build.sh` — 打包脚本
- `/nas/synology/frpdeck/` — SPK 源
- `/.github/workflows/release.yml` — CI synology-spk job
- `/cmd/server/` — frpdeck-server 入口（Docker / 桌面 / Wails / 飞牛 / 群晖共用）
- `/nas/fnos/README.md` — 飞牛 fnOS 应用包文档
