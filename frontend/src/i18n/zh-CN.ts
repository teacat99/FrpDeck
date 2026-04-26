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
    advanced: '高级',
    advanced_hide: '收起',
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
      notConfigured: '未配置'
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
