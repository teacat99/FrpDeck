export default {
  app: {
    title: 'FrpDeck',
    subtitle: 'frp 控制甲板 · 多 frps 服务端 + 临时隧道'
  },
  theme: {
    light: '浅色模式',
    dark: '深色模式',
    auto: '跟随系统',
    switchTo: {
      light: '切换到浅色',
      dark: '切换到深色',
      auto: '切换到跟随系统'
    }
  },
  menu: {
    home: '首页',
    endpoints: '服务端',
    tunnels: '隧道',
    history: '历史',
    users: '用户',
    settings: '设置'
  },
  role: {
    admin: '管理员',
    user: '普通用户'
  },
  common: {
    cancel: '取消',
    confirm: '确认',
    save: '保存',
    delete: '删除',
    edit: '编辑',
    close: '关闭',
    refresh: '刷新',
    actions: '操作',
    created: '已创建',
    updated: '已更新',
    deleted: '已删除',
    on: '启用',
    off: '禁用',
    all: '全部'
  },
  pwa: {
    title: '安装 FrpDeck 到主屏幕',
    desc: '获得独立窗口、更快启动和桌面图标',
    install: '立即安装',
    later: '稍后再说',
    iosHint: '在 Safari 点击"分享" → "添加到主屏幕"',
    dismissHint: '14 天内不再提醒'
  },
  action: {
    submit: '提交',
    cancel: '取消',
    confirm: '确认',
    save: '保存',
    saving: '保存中…',
    refresh: '刷新',
    reset: '重置',
    login: '登录',
    logout: '退出',
    search: '搜索',
    change_password: '修改密码',
    reset_password: '重置密码',
    enable: '启用',
    disable: '禁用',
    new_user: '新建用户'
  },
  password: {
    old: '原密码',
    new: '新密码',
    confirm: '确认新密码',
    too_short: '密码至少 6 位',
    mismatch: '两次输入不一致',
    changed: '密码已更新',
    strength: {
      weak: '太弱',
      fair: '一般',
      medium: '中等',
      good: '不错',
      strong: '很强'
    }
  },
  login: {
    title: '登录到 FrpDeck',
    username: '用户名',
    usernamePlaceholder: '请输入用户名',
    password: '密码',
    passwordPlaceholder: '请输入密码',
    captcha: '人机验证',
    captchaPlaceholder: '请输入答案',
    captchaRefresh: '换一题',
    failed: '登录失败',
    submit: '登录',
    submitting: '登录中…',
    lockedUntil: '由于多次失败，登录已临时锁定',
    retryIn: '{seconds} 秒后重试',
    welcomeBack: '欢迎回来',
    lastLoginAt: '上次登录：{at}',
    lastLoginFrom: '来自 {ip}'
  },
  home: {
    title: '欢迎回来',
    subtitle: '一眼看清所有 frps 服务端和正在工作的隧道',
    cards: {
      endpoints: 'frps 服务端',
      endpoints_hint: '已配置的远端服务',
      tunnels: '已配置隧道',
      tunnels_hint: '所有 frp proxy / visitor',
      active: '运行中',
      active_hint: '当前 active 状态',
      expiring: '临时隧道',
      expiring_hint: '设置了到期时间'
    },
    next_steps: {
      title: '继续上手',
      subtitle: '从添加一个 frps 服务端开始'
    }
  },
  endpoint: {
    title: 'frps 服务端',
    subtitle: '管理 FrpDeck 连接的 frps 服务端集合',
    add: '新增服务端',
    edit: '编辑服务端',
    empty: '尚未添加任何 frps 服务端',
    empty_hint: '添加第一个服务端后即可在其上创建隧道',
    required: '名称、地址必填',
    invalid_port: '端口需在 1-65535 之间',
    confirm_delete: '确定要删除服务端 {name} 及其下所有隧道吗？',
    advanced: '高级',
    advanced_hide: '收起',
    field: {
      name: '名称',
      group: '分组',
      addr: '地址',
      port: '端口',
      protocol: '协议',
      token: '鉴权 Token',
      token_keep: '留空表示保留原值',
      meta_token: '附加 Token',
      meta_token_hint: '可选，用于多用户元数据',
      user: '用户名',
      driver: '驱动',
      tls_enable: '启用 TLS',
      tls_enable_hint: 'frp 默认对接 frps 时启用 TLS',
      tls_config: 'TLS 配置（PEM 路径或内容）',
      pool_count: '连接池大小',
      pool_count_hint: '为 0 表示按需建立连接',
      heartbeat_interval: '心跳间隔（秒）',
      heartbeat_timeout: '心跳超时（秒）',
      enabled: '启用',
      auto_start: '随启动',
      live_state: '运行态'
    },
    state: {
      disconnected: '未连接',
      connecting: '连接中',
      connected: '已连接',
      failed: '失败'
    }
  },
  tunnel: {
    title: '隧道',
    subtitle: '为每个 frps 服务端配置 proxy / visitor',
    add: '新增隧道',
    edit: '编辑隧道',
    empty: '尚未配置任何隧道',
    empty_hint: '从已添加的 frps 服务端创建一条隧道',
    no_endpoint: '请先添加 frps 服务端',
    no_endpoint_hint: '至少需要一个服务端，隧道才能挂载',
    required_name: '隧道名称必填',
    required_endpoint: '请选择所属 frps 服务端',
    confirm_delete: '确定要删除隧道 {name} 吗？',
    started: '隧道已启动',
    stopped: '隧道已停止',
    renewed: '已续期',
    renewed_permanent: '已设为永久',
    advanced: '高级',
    advanced_hide: '收起',
    renew: {
      label: '续期',
      plus_1h: '+1 小时',
      plus_1d: '+1 天',
      plus_7d: '+7 天',
      permanent: '设为永久'
    },
    notify: {
      expiring_title: '隧道 {name} 即将到期',
      expiring_body: '约 {minutes} 分钟后将自动停止，可在「隧道」页快速续期。'
    },
    diag: {
      action: '连通性自检',
      title: '连通性自检',
      subtitle: '依次检查 DNS / frps 端口 / 会话注册 / 本地服务，按需修复后重跑。',
      running: '正在执行自检…',
      rerun: '重新检查',
      overall: '总体',
      status: {
        ok: '正常',
        warn: '警告',
        fail: '失败',
        skipped: '跳过'
      },
      check: {
        dns: 'DNS 解析',
        tcp_probe: 'frps 端口探测',
        frps_register: 'frps 会话状态',
        local_reach: '本地服务可达'
      }
    },
    advice: {
      action: 'frps 配置助手',
      title: 'frps 侧需要的配置',
      subtitle: '基于隧道「{name}」的字段反推 frps.toml 应当如何配置。',
      loading: '正在生成建议…',
      empty: '该隧道无需 frps 端额外配置',
      severity: {
        required: '必须',
        recommended: '建议',
        info: '提示',
        warn: '注意'
      },
      docs: '查看官方文档',
      caveats: '其他注意事项',
      snippet: '可粘贴到 frps.toml 的片段',
      copy: '复制片段',
      copied: '已复制到剪贴板'
    },
    section: {
      basic: '基础',
      proxy: 'Proxy（公网入口）',
      http: 'HTTP / HTTPS',
      secret: 'STCP / XTCP / SUDP',
      visitor: 'Visitor（拨号端）',
      lifecycle: '生命周期',
      advanced: '高级'
    },
    role: {
      label: '角色',
      server: 'Server（被访问端）',
      visitor: 'Visitor（拨号端）'
    },
    expire: {
      label: '过期时间',
      hint: '到期后 FrpDeck 会自动停止此隧道',
      forever: '永久',
      cleared: '已清除过期时间',
      preset_2h: '+2 小时',
      preset_1d: '+1 天',
      preset_7d: '+7 天',
      remaining: '剩余 {value}',
      expired: '已过期'
    },
    status: {
      pending: '待启动',
      active: '运行中',
      expired: '已过期',
      stopped: '已停止',
      failed: '失败'
    },
    live: {
      pending: '待启动',
      starting: '启动中',
      running: '运行中',
      check_failed: '健康检查失败',
      stopped: '已停止',
      error: '错误'
    },
    validation: {
      type_required: '请选择隧道类型',
      visitor_only_for_secret: 'Visitor 仅支持 stcp / xtcp / sudp',
      sk_required: '需要填写 SK',
      server_name_required: '请填写 Server 名称',
      domains_required: 'HTTP/HTTPS 需要至少一个 subdomain 或 custom_domains',
      port_range: '端口需在 0-65535 之间',
      expire_in_past: '过期时间需为未来时刻'
    },
    field: {
      name: '名称',
      endpoint: '所属服务端',
      type: '类型',
      target: '本地 → 远端',
      status: '状态',
      expire: '到期',
      local_ip: '本地 IP',
      local_port: '本地端口',
      remote_port: '远端端口',
      subdomain: '子域名',
      custom_domains: '自定义域名（逗号分隔）',
      locations: 'URL 路径（逗号分隔）',
      http_user: 'HTTP 用户名',
      http_password: 'HTTP 密码',
      http_password_keep: '留空保持不变',
      host_header_rewrite: 'Host Header 重写',
      sk: 'SK 共享密钥',
      sk_keep: '留空保持不变',
      allow_users: '允许的访客用户（逗号分隔，* 为全部）',
      server_name: '目标 Server 名称',
      encryption: '加密',
      compression: '压缩',
      bandwidth_limit: '带宽限制',
      bandwidth_limit_hint: '例如 1MB、512KB',
      group: '负载分组',
      group_key: '分组密钥',
      health_check_type: '健康检查类型',
      health_check_url: '健康检查 URL',
      plugin: '插件',
      plugin_config: '插件参数（key=val,key=val）',
      enabled: '启用',
      auto_start: '随启动'
    }
  },
  template: {
    audience: '适用人群',
    wizard: {
      action: '场景模板',
      title: '从场景模板创建',
      subtitle: '选择一个常见场景，FrpDeck 会预填好对应类型/端口/角色，再让你确认细节。',
      loading: '正在加载模板…',
      empty: '暂无模板'
    },
    'web-http': {
      name: '暴露 Web 网站到公网',
      desc: 'HTTP + 自定义域名/子域名，适合个人博客、内网仪表盘等纯 HTTP 站点',
      audience: '在内网跑了一个 HTTP 服务，想给朋友/同事访问'
    },
    'web-http.prereq.vhost': 'frps.toml 已配置 vhostHTTPPort（默认 80）',
    'web-http.prereq.dns': '已把目标域名 DNS 解析到 frps 公网 IP',
    'web-https': {
      name: '暴露 HTTPS 网站到公网',
      desc: 'HTTPS + 自定义域名，frps 走 SNI 路由，TLS 证书在内网服务自行处理',
      audience: '内网服务已自带 HTTPS，想直接以 https://your-domain 暴露'
    },
    'web-https.prereq.vhost': 'frps.toml 已配置 vhostHTTPSPort（默认 443）',
    'web-https.prereq.cert': '内网服务已就绪 TLS 证书（或使用通配符证书 + frps https2http）',
    'web-https.prereq.dns': '已把目标域名 DNS 解析到 frps 公网 IP',
    rdp: {
      name: 'RDP 远程桌面',
      desc: 'TCP + remote_port，把 Windows 远程桌面经 frps 暴露',
      audience: '出差/在外想连家里 Windows 桌面'
    },
    'rdp.prereq.allowports': 'frps.toml allowPorts 已包含 13389（或为空白名单）',
    'rdp.prereq.firewall': 'Windows 防火墙已放行 RDP（默认 3389）',
    ssh: {
      name: 'SSH 跳板',
      desc: 'TCP + remote_port，远程 ssh -p 22022 user@frps_addr 即可登入内网机',
      audience: '需要远程 ssh 登录家里/办公室的 Linux 机器'
    },
    'ssh.prereq.allowports': 'frps.toml allowPorts 已包含 22022',
    'ssh.prereq.sshd': '本机 sshd 服务正常运行（默认 22 端口监听）',
    'db-share': {
      name: '暴露 MySQL/Redis 给同事',
      desc: 'TCP + 临时隧道（默认 4 小时自动停），用完即焚，避免长期暴露数据库',
      audience: '临时让同事/客户连一下数据库做调试'
    },
    'db-share.prereq.allowports': 'frps.toml allowPorts 已包含 13306',
    'db-share.prereq.tempnote': '默认 4 小时后自动到期；可在保存前调整或清空过期时间',
    'nas-p2p': {
      name: 'p2p 访问家里 NAS',
      desc: 'xtcp visitor，握手用 frps 协调，真正流量走 P2P 直连，绕开带宽限制',
      audience: '家里 NAS 文件多/大，不想走 frps 中转浪费带宽'
    },
    'nas-p2p.prereq.peer': 'NAS 端已部署 server 角色的 xtcp（同一个 sk）',
    'nas-p2p.prereq.stun': 'frps.toml 已配置 natHoleStunServer',
    'nas-p2p.prereq.shared-sk': 'visitor 与 server 必须使用完全相同的 sk',
    socks5: {
      name: '私有 SOCKS5 代理',
      desc: 'plugin: socks5，通过 frps 暴露一个 SOCKS5 代理，配合系统/浏览器代理使用',
      audience: '想在外网用家里/办公室网络出口（IP/区域专属）'
    },
    'socks5.prereq.allowports': 'frps.toml allowPorts 已包含 11080',
    'socks5.prereq.creds': '建议在 plugin_config 配置 plugin_user / plugin_passwd 鉴权',
    'http-proxy': {
      name: 'HTTP 反向代理跳板',
      desc: 'plugin: http_proxy，通过 frps 暴露一个 HTTP 代理；浏览器/curl 设置 http_proxy 即可使用',
      audience: '需要 HTTP 协议的代理出口，比 SOCKS5 更轻量'
    },
    'http-proxy.prereq.allowports': 'frps.toml allowPorts 已包含 18888',
    'http-proxy.prereq.creds': '建议在 plugin_config 配置 plugin_user / plugin_passwd 鉴权',
    'static-file': {
      name: '静态文件分享',
      desc: 'plugin: static_file，把本地一个目录通过 HTTP 暴露给外网下载',
      audience: '一次性给别人发大文件，又不想用云盘'
    },
    'static-file.prereq.vhost': 'frps.toml 已配置 vhostHTTPPort 且域名已解析',
    'static-file.prereq.path': 'plugin_config 中的 plugin_local_path 指向真实存在的目录',
    'frpdeck-self': {
      name: '远程代管 FrpDeck',
      desc: 'stcp（自指），把 FrpDeck 自己的管理面（127.0.0.1:8080）通过 frps 安全暴露',
      audience: '在外想登录家里那台 FrpDeck 看运行状态'
    },
    'frpdeck-self.prereq.shared-sk': 'visitor 端必须使用相同的 sk',
    'frpdeck-self.prereq.password-mode': '强烈建议把 FrpDeck 切到 password 鉴权（绝不能 none）',
    'frpdeck-self.prereq.visitor-side': '另一台机器上需有对应 stcp visitor 隧道'
  },
  history: {
    title: '操作历史',
    subtitle: '审计日志：每一次写入都会留痕',
    empty: '暂无审计记录',
    empty_hint: '当有人添加 / 修改 / 删除资源时会出现在这里',
    field: {
      time: '时间',
      action: '动作',
      actor: '操作人',
      detail: '详情'
    }
  },
  user: {
    add: '新建用户',
    edit: '编辑用户',
    empty: '尚无用户',
    reset_password: '重置密码',
    password_reset: '密码已重置',
    username_required: '用户名必填',
    password_too_short: '密码至少 8 位',
    confirm_delete: '确定要删除用户 {name} 吗？',
    field: {
      username: '用户名',
      password: '密码',
      role: '角色',
      disabled: '启用状态',
      created_at: '创建时间'
    }
  },
  settings: {
    title: '系统设置',
    subtitle: '账号、运行时参数与安全审计',
    users_hint: '管理本实例的登录账号',
    tabs: {
      users: '账号',
      runtime: '运行时参数',
      security: '安全审计'
    },
    runtime: {
      intro: '修改后立即生效，无需重启进程；下次冷启动会从 SQLite 重新读取。',
      dirty: '有 {n} 项尚未保存',
      clean: '所有修改均已保存',
      saved: '已保存',
      sectionRuleLimits: '隧道与历史',
      sectionLogin: '登录加固',
      sectionDefence: '附加防御',
      sectionNotify: '通知（ntfy）',
      sectionSystem: '系统信息（不可修改）',
      notConfigured: '未配置',
      tunnelExpiringNotifyMinutes: '到期提前通知',
      tunnelExpiringNotifyMinutesHelp: '隧道到期前提前 N 分钟弹窗 + 浏览器通知，0 关闭，最多 120'
    },
    notify: {
      test: '发送测试',
      testing: '发送中…',
      testOk: '测试已发送',
      testFail: '测试发送失败'
    },
    security: {
      filter_username: '按用户名过滤',
      empty: '暂无登录尝试记录',
      field: {
        ip: 'IP',
        success: '结果',
        reason: '原因'
      }
    }
  },
  msg: {
    loadFailed: '加载失败',
    saveFailed: '保存失败',
    deleteFailed: '删除失败',
    opFailed: '操作失败',
    invalidInput: '输入无效',
    saved: '已保存',
    deleted: '已删除',
    durationExceeded: '时长超过该端口允许的最大值',
    expiryExceeded: '到期时间超过最大限制',
    rateLimitExceeded: '请求过于频繁，请稍后再试',
    concurrentQuotaExceeded: '同一来源并发数已达上限'
  }
}
