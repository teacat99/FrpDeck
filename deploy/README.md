# FrpDeck 部署指南（Docker / NAS / 反代）

> 本文档面向 **Headless Web UI** 形态的 FrpDeck —— 也就是 NAS / 服务器 / 无桌面 Linux
> 上跑的 Docker 部署。桌面 GUI 形态（Wails 单文件）走另一条路，不在本文范围。
>
> 当前阶段 v0.1.x（P0–P3 完成），覆盖 **多 frps endpoint + 隧道 CRUD + 临时隧道生命周期 +
> 实时事件 WebSocket + 系统服务安装**；远程代管、场景模板、Android 等高阶能力在后续 Phase。

## 目录

- [快速上手（最简 docker run）](#快速上手最简-docker-run)
- [docker-compose 推荐部署](#docker-compose-推荐部署)
- [群晖 Synology — Container Manager](#群晖-synology--container-manager)
- [飞牛 OS（fnOS）— 应用 / Docker](#飞牛-osfnos-应用--docker)
- [反向代理与 HTTPS](#反向代理与-https)
- [常见踩坑：UID 10001 与 bind mount 权限](#常见踩坑uid-10001-与-bind-mount-权限)
- [备份、升级、回滚](#备份升级回滚)
- [运维诊断](#运维诊断)
- [环境变量参考](#环境变量参考)

---

## 镜像

| Registry | 路径 |
|---|---|
| GitHub Container Registry | `ghcr.io/teacat99/frpdeck:latest` |
| Docker Hub | `teacat99/frpdeck:latest` |

支持平台：`linux/amd64` + `linux/arm64`（NAS 常见的 Intel Atom / 飞牛 N100 / Synology DS x+ 系列 / 群晖 ARM 机型都覆盖）。

每个 release 同时打三个 tag：

- `latest`（不固定）
- `vX.Y.Z`（精确版本）
- `vX.Y`（minor 通道，会随 patch 跟进）

NAS 部署推荐固定到 `vX.Y` 通道：既能拿到安全 patch，又不会踩到 minor 升级的 breaking change。

---

## 快速上手（最简 docker run）

最小可运行命令：

```bash
docker run -d --name frpdeck \
  -p 8080:8080 \
  -v frpdeck-data:/data \
  -e FRPDECK_ADMIN_PASSWORD='请改成强密码' \
  --restart unless-stopped \
  ghcr.io/teacat99/frpdeck:latest
```

打开浏览器访问 `http://<host>:8080`，使用 `admin` + 上面设置的密码登录。

> ⚠️ **首启后立即在 UI 里改密码**。`FRPDECK_ADMIN_PASSWORD` 仅作种子值（数据库为空时落库），后续以
> UI 修改为准。

---

## docker-compose 推荐部署

复制项目根目录的 [`docker-compose.yaml`](../docker-compose.yaml) 到任意工作目录，按下面步骤改：

```bash
mkdir -p /opt/frpdeck && cd /opt/frpdeck
curl -fsSL https://raw.githubusercontent.com/teacat99/FrpDeck/main/docker-compose.yaml -o docker-compose.yaml
# 必改：admin 密码 + JWT secret
sed -i "s/change-me/$(openssl rand -base64 24)/" docker-compose.yaml
docker compose up -d
docker compose logs -f frpdeck   # 看启动日志
```

确认 `health: healthy` 后即可访问 UI：

```bash
docker compose ps
# NAME      ... STATUS
# frpdeck   ... Up X seconds (healthy)
```

`docker-compose.yaml` 已经做了下面这些事，**不需要手动调**：

| 项 | 配置 | 作用 |
|---|---|---|
| `restart: unless-stopped` | 是 | 故障 / 重启自动起 |
| `healthcheck` | `wget --spider /api/version` | NAS 面板能看到健康状态 |
| `cap_drop: [ALL]` | 是 | FrpDeck 不需要任何 Linux capability，掉光 |
| `security_opt: no-new-privileges` | 是 | 阻止 setuid 提权 |
| Volume 路径 | `./data:/data`（bind mount） | 备份只需复制 `./data` |

---

## 群晖 Synology — Container Manager

> 适用 DSM 7.2+ 自带的 **Container Manager**（旧版 Docker 套件操作类似）。

### 路径选型

| 路径 | 适用 | 备注 |
|---|---|---|
| **A. 项目（Project）→ 粘贴 docker-compose.yaml** | 推荐 | 一份配置文件带走所有设置，备份 / 迁移最方便 |
| B. 注册中心拉镜像 → 手动配置容器 | 喜欢 GUI 流程的 | 健康检查、cap_drop 等需要在「容器」高级设置里逐个配 |

### A 路径：Project（推荐）

1. **准备数据目录**（SSH 进 NAS 或用 File Station）：

   ```bash
   mkdir -p /volume1/docker/frpdeck/data
   chown 10001:10001 /volume1/docker/frpdeck/data
   ```

   注意 UID **必须** 改成 `10001`，否则容器会因为没写入权限崩溃（SQLite 报 `unable to open database file`，
   实际是 permission denied —— 见 [UID 踩坑](#常见踩坑uid-10001-与-bind-mount-权限)）。

2. Container Manager → **项目 → 新增**：
   - 项目名称：`frpdeck`
   - 路径：`/volume1/docker/frpdeck`
   - 来源：**从 docker-compose.yaml 创建**
   - 把仓库根目录的 `docker-compose.yaml` 内容粘贴进去
   - 把 `volumes:` 改成绝对路径：

     ```yaml
     volumes:
       - /volume1/docker/frpdeck/data:/data
     ```

3. 改密码、确认端口（若 8080 被占用，改 `ports: - "18080:8080"`），点 **下一步 → 完成**。

4. 项目状态 → 等到「运行中」+「健康」即完成。

5. 群晖控制面板的 **应用程序门户 / 反向代理** 把 `https://frpdeck.example.com` 反代到
   `http://localhost:8080`（详见下方 [反向代理](#反向代理与-https)）。

### B 路径：注册中心 + 容器

简要步骤（不推荐，给习惯 GUI 流程的用户兜底）：

1. 注册中心 → 搜索 `ghcr.io/teacat99/frpdeck` → 下载 `latest`
2. 容器 → 新增 → 选刚下载的镜像
3. 高级设置：
   - **常规**：勾「启用资源限制」按需，**勾「自动重启」**
   - **卷**：`/volume1/docker/frpdeck/data` → 容器路径 `/data` → **不要勾「只读」**
   - **网络**：bridge
   - **端口**：本地 8080 → 容器 8080（TCP）
   - **环境**：参照 [环境变量](#环境变量参考)，至少加：
     - `FRPDECK_ADMIN_PASSWORD=<强密码>`
     - `FRPDECK_AUTH_MODE=password`
   - **能力**：把所有 capability 拖到「不允许」
4. 应用 → 启动。

---

## 飞牛 OS（fnOS）— 应用 / Docker

> 飞牛 OS 自带的「应用商店」目前没有 FrpDeck 官方上架（计划在 P9）。两条路：

### 路径 A：飞牛 OS 自带 Docker（推荐）

1. **应用 → Docker → Compose 部署**（路径与群晖 Container Manager 类似）。
2. 数据目录：`/vol1/docker/frpdeck/data`（飞牛默认存储池前缀），SSH 后 `chown 10001:10001`。
3. 粘贴 `docker-compose.yaml`，把 `volumes` 改成 `/vol1/docker/frpdeck/data:/data`。
4. 启动后在「应用 → 网络 → 反向代理」配 `frpdeck.example.com → 127.0.0.1:8080`。

### 路径 B：SSH 直接 docker compose

完全等同 [docker-compose 推荐部署](#docker-compose-推荐部署) 的步骤，只是工作目录换成
`/vol1/docker/frpdeck`。

> 飞牛 OS 的 Docker 在 ZFS 存储池上时偶现 bind-mount 权限失效，遇到的话改用 named
> volume：`docker volume create frpdeck-data`，把 `./data:/data` 换成
> `frpdeck-data:/data` 即可。

---

## 反向代理与 HTTPS

FrpDeck 的 Web UI **必须**走 HTTPS 才能让 PWA、ServiceWorker、安全 cookie 行为完整工作；
直接公网暴露 `:8080` 不推荐。

### WebSocket 反代（重要）

FrpDeck 的实时事件通道是 `/api/ws`，反代必须显式开 WebSocket upgrade：

#### Nginx / 群晖反代

群晖控制面板 → 应用程序门户 → 反向代理 → 编辑 → **自定义标头**：

| Header 名 | 值 |
|---|---|
| `Upgrade` | `$http_upgrade` |
| `Connection` | `upgrade` |

群晖 DSM 7.2 起预置「WebSocket」按钮，一键勾选即可。

手写 nginx 站点：

```nginx
server {
  listen 443 ssl http2;
  server_name frpdeck.example.com;

  ssl_certificate     /path/to/fullchain.pem;
  ssl_certificate_key /path/to/privkey.pem;

  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 1d;
  }
}
```

并把 nginx / 群晖 / 飞牛的反代主机 IP 写进环境变量：

```yaml
FRPDECK_TRUSTED_PROXIES: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
```

否则审计日志里所有客户端 IP 会变成反代主机 IP，触发限速也会误伤。

#### Caddy（自动签证书最省事）

```Caddyfile
frpdeck.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

Caddy 默认正确处理 WebSocket，不需要额外配置。

### 不走反代的内网场景

只在内网用且设备少的情况下，可以保持 `http://nas.local:8080` 直连——但务必：

1. 把 `FRPDECK_AUTH_MODE` 设为 `password`（默认）；
2. 配 `FRPDECK_ADMIN_IP_WHITELIST`，例如 `192.168.0.0/16`；
3. 不要把 `:8080` 端口转发到公网。

---

## 常见踩坑：UID 10001 与 bind mount 权限

镜像内部用非 root 账号 `frpdeck`（UID/GID = `10001`）跑，提高了运行时安全等级，
但代价是 **bind mount 路径需要把属主改成 10001**，否则启动直接挂掉。

### 症状

容器日志里看到：

```
bootstrap: store: open sqlite: unable to open database file: out of memory (14)
```

—— 这不是真的 OOM，而是 SQLite 把 EACCES 翻译成 14 号错误的迷惑性输出。

### 三种修复

#### A（推荐）：宿主机 chown

```bash
mkdir -p /opt/frpdeck/data
sudo chown -R 10001:10001 /opt/frpdeck/data
```

#### B：用 named volume，让 Docker 自己管权限

```yaml
volumes:
  - frpdeck-data:/data
volumes:
  frpdeck-data: {}
```

代价：备份要用 `docker run --rm -v frpdeck-data:/from -v "$PWD":/to alpine cp -a /from/. /to/`，
没有 bind mount 那种「直接把目录复制走」方便。

#### C（不推荐）：让容器以 root 跑

可以在 compose 里加 `user: "0:0"`，但会破坏镜像的 hardening 设计，仅做临时排障。

---

## 备份、升级、回滚

### 备份（每天 1 次足够）

只需要保护 `/data` 目录里的 `frpdeck.db`，里面包含 endpoints / tunnels / 用户 / JWT secret /
审计日志。

```bash
# bind mount 部署：
tar czf "frpdeck-backup-$(date +%Y%m%d).tgz" -C /opt/frpdeck data

# named volume 部署：
docker run --rm \
  -v frpdeck-data:/from \
  -v "$PWD":/to \
  alpine \
  tar czf "/to/frpdeck-backup-$(date +%Y%m%d).tgz" -C /from .
```

群晖 Hyper Backup / 飞牛 OS 的备份套件直接选 `docker/frpdeck/data` 目录即可，不需要停容器
（SQLite 默认是 WAL 模式，热备份安全）。

### 升级

```bash
cd /opt/frpdeck
docker compose pull          # 拉新版镜像
docker compose up -d          # 重新创建容器，volume 不动
docker compose logs -f frpdeck
```

升级前**强烈建议**先备份。GORM AutoMigrate 一般是只加不减，但跨大版本（v0.1 → v0.2）的
schema 调整可能改字段，遇事不决先打 tar。

### 回滚

```bash
# 指定老版本：
sed -i 's|frpdeck:latest|frpdeck:v0.1.0|' docker-compose.yaml
docker compose up -d

# 数据回滚（先停服务）：
docker compose stop
tar xzf frpdeck-backup-YYYYMMDD.tgz -C /opt/frpdeck/
docker compose start
```

---

## 运维诊断

### 容器健康

```bash
docker inspect --format '{{.State.Health.Status}}' frpdeck
docker inspect --format '{{json .State.Health}}' frpdeck | jq
```

`healthy` = `/api/version` 返回 200；`unhealthy` 持续 3 次说明进程或监听端口出问题。

### 看运行日志

```bash
docker compose logs --tail=200 -f frpdeck
```

关键日志行（确认健康）：

```
frpcd driver: embedded (frp v0.68.x)
FrpDeck listening on :8080 (auth=password, driver=embedded)
```

### 看 frp 隧道连接日志

UI 里 **审计 / 仪表盘** 看运行态；命令行可订阅 WebSocket：

```bash
TOKEN=$(curl -sS -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"...."}' | jq -r .token)

# 项目 tools/wssmoke 是 Go 写的事件订阅 CLI，跑：
go run ./tools/wssmoke -url ws://localhost:8080/api/ws -token "$TOKEN" \
  -topics tunnels,endpoints,logs:all
```

### 进容器排障

```bash
docker compose exec frpdeck /bin/sh
ls -la /data
/usr/local/bin/frpdeck version
```

---

## 环境变量参考

> 完整列表见 `internal/config/config.go`。下面只列 NAS / Docker 部署关心的项。

### 核心

| 变量 | 默认 | 说明 |
|---|---|---|
| `FRPDECK_LISTEN` | `:8080` | 监听地址，反代场景常改成 `127.0.0.1:8080` 限本机 |
| `FRPDECK_DATA_DIR` | `/data` | SQLite 与运行时数据目录；改这个一定要同步改 volume mount |
| `FRPDECK_AUTH_MODE` | `password` | `password` / `ipwhitelist` / `none`；NAS 强烈建议 `password` |
| `FRPDECK_FRPCD_DRIVER` | `embedded` | 当前仅 `embedded`；P8 加 `subprocess` |
| `FRPDECK_HEALTH_URL` | `http://127.0.0.1:8080/api/version` | Dockerfile HEALTHCHECK 使用，改 listen 时同步改这里 |

### 鉴权 / 安全

| 变量 | 默认 | 说明 |
|---|---|---|
| `FRPDECK_ADMIN_USERNAME` | `admin` | 种子管理员用户名（仅库为空时落库） |
| `FRPDECK_ADMIN_PASSWORD` | _（必填）_ | 种子管理员密码；首启后请在 UI 改掉 |
| `FRPDECK_JWT_SECRET` | _（自动生成）_ | 32 字节 hex；不设则每次重启 rotate（强制重登） |
| `FRPDECK_ADMIN_IP_WHITELIST` | _（空）_ | 逗号分隔 CIDR 列表，限定能打开 admin 操作的源 IP |
| `FRPDECK_TRUSTED_PROXIES` | _（空）_ | 反代场景下务必填，否则审计日志全是 nginx IP |
| `FRPDECK_RATELIMIT_PER_MINUTE` | `10` | 单 IP 每分钟登录尝试上限 |
| `FRPDECK_HISTORY_RETENTION_DAYS` | `30` | 审计日志保留天数 |

### 通知 / Ntfy

| 变量 | 说明 |
|---|---|
| `FRPDECK_NTFY_URL` | ntfy server URL，例 `https://ntfy.sh/my-topic` |
| `FRPDECK_NTFY_TOKEN` | ntfy access token（私有 server 必填） |

### 系统

| 变量 | 默认 | 说明 |
|---|---|---|
| `TZ` | `UTC` | 时区；NAS 部署一般设 `Asia/Shanghai` |
| `FRPDECK_INSTANCE_NAME` | _（hostname）_ | 多实例时区分用，远程代管会用到（P5） |

---

## 反馈与问题

- 部署相关 issue：[github.com/teacat99/FrpDeck/issues](https://github.com/teacat99/FrpDeck/issues)
- 群晖 / 飞牛 / 威联通的 step-by-step 截图教程会在 P9（套件商店上架）阶段补齐。
