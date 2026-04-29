# FrpDeck — 飞牛 fnOS 应用包

> Native 模式 `.fpk`，对应飞牛官方文档：[Native 应用构建](https://developer.fnnas.com/docs/core-concepts/native/) ｜ [架构概述](https://developer.fnnas.com/docs/core-concepts/framework/)

## 目录布局

```text
nas/fnos/
├── README.md                # 本文件
├── build.sh                 # 双架构打包脚本（x86 + arm）
└── frpdeck/                 # fnpack create 风格的应用源
    ├── manifest             # key=value，platform 由 build.sh 在打包时改写
    ├── ICON.PNG             # 64×64
    ├── ICON_256.PNG         # 256×256
    ├── LICENSE              # MIT，安装前展示
    ├── cmd/
    │   ├── main             # start/stop/status，PID 管理 + TEMP_LOGFILE 错误展示
    │   ├── install_init / install_callback
    │   ├── uninstall_init / uninstall_callback
    │   ├── upgrade_init / upgrade_callback
    │   └── config_init / config_callback
    ├── config/
    │   ├── privilege        # run-as: package，专用用户 frpdeck
    │   └── resource         # data-share: FrpDeck（用户存自己的 frpc.toml 用）
    └── app/
        ├── server/          # frpdeck-server 二进制（打包时注入）
        └── ui/
            ├── config       # 桌面图标 → http://<host>:18080/
            └── images/{icon_64.png, icon_256.png}
```

## 构建产物

打包后生成（不入 git）：

```text
nas/fnos/dist/
├── com.teacat.frpdeck-<version>-x86.fpk    # 飞牛 x86_64 设备
└── com.teacat.frpdeck-<version>-arm.fpk    # 飞牛 ARM 设备
```

## 本地构建

### 前置依赖

| 工具 | 版本 | 备注 |
|---|---|---|
| Go | 1.25+ | 内置 `cmd/server` 编译 |
| Node | 20+ | `frontend` 构建 |
| Python | 3.x（标准库） | 用 `zipfile` 模块代替 `zip` 命令打包，规避「主机没装 zip」的部署坑 |

### 命令

```bash
# 全套（x86 + arm，含 frontend）
bash nas/fnos/build.sh

# 单架构
bash nas/fnos/build.sh x86
bash nas/fnos/build.sh arm

# 跳过前端构建（开发时反复打包用，复用上次产物）
FRPDECK_SKIP_FRONTEND_BUILD=1 bash nas/fnos/build.sh

# 显式指定版本号（覆盖 manifest 默认）
VERSION=v0.7.1 bash nas/fnos/build.sh
```

`build.sh` 会自动把 `VERSION=v0.7.0` 这种 git 风格剥离前导 `v`，写到 manifest 与产物文件名中（飞牛 manifest 规范要求 semver 不带 `v`），同时保留 `v` 前缀传给 Go ldflags 注入到二进制版本输出。

## CI 自动产线

`.github/workflows/release.yml` 已加入 `fnos-fpk` job：

1. push tag `v*` 触发
2. `docker` job 完成后启动
3. 装 Go 1.25 + Node 20，跑 `frontend` 构建
4. `bash nas/fnos/build.sh` 出 x86 + arm 双包
5. 用 `softprops/action-gh-release` 上传到 GitHub Release，文件名形如：
   - `com.teacat.frpdeck-0.7.0-x86.fpk`
   - `com.teacat.frpdeck-0.7.0-arm.fpk`

## 手工实机验证流程

> 任一飞牛 fnOS 设备（>= 0.9.0）即可。

1. **下载 fpk**：从 GitHub Release 下载对应架构的 `.fpk`（x86 NAS 选 `-x86.fpk`，ARM NAS 选 `-arm.fpk`）
2. **侧载安装**：
   - 飞牛桌面 → 应用中心 → 右上角「+」/「自定义安装」 → 选择刚下载的 `.fpk`
   - 阅读 LICENSE，确认安装位置（默认 `/vol1/@appcenter/frpdeck/`）
3. **桌面图标点开**：图标 `images/icon_64.png`，点击后浏览器打开 `http://<NAS_IP>:18080/`
4. **首次登录**：
   - 默认管理员账户尚未创建（`FRPDECK_AUTH_MODE=password`），首屏走「设置管理员」引导
   - 设置完成后正常跳到主界面
5. **冒烟用例**：
   - 添加 frps Endpoint（填一个真实 frps 地址 + token）
   - 创建 tcp 隧道（local_port=22, remote_port=7022）
   - 启动隧道 → 看 `audit log` 与 frps 端日志对应
   - 重启 NAS → 应用应自动恢复（fnOS 应用中心默认开机自启）
6. **卸载验证**：
   - 应用中心 → 卸载 FrpDeck
   - `target/` `tmp/` `home/` `etc/` 自动清空
   - **`var/` 与共享目录 `FrpDeck/` 内的 `data/frpdeck.db` 保留**（避免误删用户配置）；如需彻底清理，手动删 `/vol*/共享空间/FrpDeck/`

### 常见问题

| 现象 | 排查 |
|---|---|
| 应用安装后启动失败，提示「FrpDeck binary not found or not executable」 | fpk 内 `app/server/frpdeck-server` 的可执行位丢失。检查 `python3 -m zipfile -l xxx.fpk` 输出该文件 mode 是否 0o755；若否，重打 |
| 端口 18080 被占 | 编辑 `共享空间/FrpDeck/config.json`，改 `listen_port`；同时 fnOS「应用设置」改不到 `service_port`，只能这里改 |
| 升级后用户隧道丢失 | `cmd/upgrade_init` 已自动备份 `frpdeck.db` 到 `${TRIM_PKGVAR}/upgrade-backup/`；从那里恢复 |
| 应用中心显示「未运行」但 Web UI 能访问 | PID 文件被外部删除。`stop` + `start` 一次重置 |

## 上架飞牛应用市场

> 实测前先在自己的飞牛设备侧载验证完整跑通，再走审核。

### 准备材料

| 材料 | 说明 |
|---|---|
| `.fpk`（x86 + arm） | CI 产物，从 GitHub Release 下载 |
| 应用图标 | `frpdeck/ICON.PNG`（64）+ `ICON_256.PNG`（256） |
| 应用截图 | 主界面 / Endpoint 列表 / 隧道详情 / 设置页，4-6 张 1080P+ |
| 应用介绍 | manifest `desc` 字段已写好 HTML 版本，可直接复用 |
| 隐私协议 / 用户协议 | LICENSE（MIT）足以满足开源应用的基本要求 |

### 流程

1. 注册 https://developer.fnnas.com/ 开发者账号
2. 「应用管理」 → 「新建应用」 → 填基本信息 → 上传 fpk + 图标 + 截图
3. 提交审核（飞牛官方审核周期参考社区反馈，通常 3-5 个工作日）
4. 审核通过后应用中心可搜到「FrpDeck」

### 后续版本发布

1. 更新代码 → 改 `manifest` 中 `version` 字段（或在打 git tag 时 CI 自动同步）
2. push git tag `v0.7.1` → CI 自动产 fpk → GitHub Release
3. 飞牛开发者后台 → 「版本管理」 → 上传新 fpk → 提交审核
4. 用户在飞牛应用中心收到更新提示

## 设计要点

### 为什么不走 docker 模式

飞牛的 docker 模式让应用中心代管 `docker-compose.yaml`，但：

- frpdeck 升级时镜像版本与应用版本两条线，用户感受会与桌面/Linux 不一致
- 应用中心的 docker 沙箱对网络出口、bind mount 有自己的约定，会与 frpc 隧道路径打架
- native 模式与 EasyTier-Web 同形态，有现成可参考样板

### 为什么用 Python 而不是 `fnpack build`

- `fnpack` 仅在 fnOS 系统上可装，CI 跑不了
- `.fpk` 实际就是 zip 加固定目录约定，Python 标准库 `zipfile` 完全可写
- 关键在 `external_attr = (mode & 0xFFFF) << 16` 保留 cmd/* 的 0755 可执行位（fnOS 启停脚本必须有执行权限）

### 为什么 `service_port = 18080`

- 8080 容易被飞牛自带的某些应用（HomeAssistant 等）占用
- 18xxx 段是飞牛社区约定的「第三方应用偏好端口」（参考 EasyTier-Web 的 11211）
- 用户可在共享空间 `FrpDeck/config.json` 中改 `listen_port` 字段（无需 SSH）

### 数据目录

| 路径 | 内容 | 卸载行为 |
|---|---|---|
| `${TRIM_APPDEST}` (target/) | frpdeck-server 二进制 + ui 资源 | 卸载即删 |
| `${TRIM_PKGVAR}` (var/) | PID 文件 / 启动日志 / 升级备份 | 保留 |
| `${TRIM_DATA_SHARE_PATHS%%:*}` (FrpDeck 共享空间) | `config.json`（用户可改端口）+ `data/frpdeck.db`（隧道 / Endpoint / audit） | 保留（用户从 fnOS 文件管理可见） |

把数据库放在共享空间而不是 `var/` 是有意为之——用户能直接通过 fnOS 文件管理 SAF/SMB 备份 `frpdeck.db`，重装应用后把它丢回 `共享空间/FrpDeck/data/` 即可恢复全部隧道配置。

---

## 相关文件

- `/nas/fnos/build.sh` — 打包脚本
- `/nas/fnos/frpdeck/` — 应用源
- `/.github/workflows/release.yml` — CI fnos-fpk job
- `/cmd/server/` — frpdeck-server 入口（共用 Docker / 桌面 / Wails / 飞牛 / 群晖 同一份代码）
