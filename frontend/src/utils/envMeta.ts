// Maps environment variable names to human-readable descriptions in both
// Chinese and English. Rendered on the Settings > "运行时参数" tab so
// operators understand what each variable controls without leaving the UI.
export interface EnvHint {
  zh: string
  en: string
}

export const ENV_META: Record<string, EnvHint> = {
  FRPDECK_LISTEN: {
    zh: '监听地址（格式 :8080 或 0.0.0.0:8080）',
    en: 'Listen address (e.g. :8080 or 0.0.0.0:8080)'
  },
  FRPDECK_DATA_DIR: {
    zh: 'SQLite 数据目录，需挂载持久化卷',
    en: 'SQLite data directory; mount a persistent volume'
  },
  FRPDECK_FRPCD_DRIVER: {
    zh: 'frp 客户端驱动：embedded（内嵌 frpc，默认） / subprocess（外部 frpc 可执行文件） / mock',
    en: 'frp client driver: embedded (in-process, default) / subprocess (external frpc binary) / mock'
  },
  FRPDECK_MAX_DURATION_HOURS: {
    zh: '单条临时隧道最大持续时长（小时），兜底上限',
    en: 'Global max duration per temporary tunnel (hours), hard cap'
  },
  FRPDECK_HISTORY_RETENTION_DAYS: {
    zh: '审计/历史记录保留天数，到期自动清理',
    en: 'Audit/history retention window (days), auto-purged'
  },
  FRPDECK_MAX_RULES_PER_IP: {
    zh: '单个 (用户,客户端 IP) 并发隧道数上限（保留兼容字段）',
    en: 'Max concurrent tunnels per (user, client-IP) pair (legacy key)'
  },
  FRPDECK_RATE_LIMIT_PER_MINUTE_PER_IP: {
    zh: '每 IP 每分钟的写操作频控上限',
    en: 'Write-ops rate cap per minute per client IP'
  },
  FRPDECK_AUTH_MODE: {
    zh: '鉴权模式：password / ipwhitelist / none',
    en: 'Auth mode: password / ipwhitelist / none'
  },
  FRPDECK_TRUSTED_PROXIES: {
    zh: '可信反代 CIDR 列表，用于解析真实客户端 IP',
    en: 'Trusted proxy CIDRs used to recover the real client IP'
  },
  FRPDECK_ADMIN_USERNAME: {
    zh: '首次启动播种的管理员用户名（落库后可忽略）',
    en: 'Seeded admin username on first boot (ignored afterwards)'
  },
  FRPDECK_ADMIN_PASSWORD: {
    zh: '首次启动播种的管理员密码（不传则默认 passwd）',
    en: 'Seeded admin password on first boot (defaults to "passwd")'
  },
  FRPDECK_JWT_SECRET: {
    zh: 'JWT 签名密钥，部署时请自行生成 32+ 字节',
    en: 'JWT signing secret; set to ≥32 random bytes in production'
  },
  FRPDECK_IP_WHITELIST: {
    zh: 'ipwhitelist 模式下允许直通的 CIDR 列表',
    en: 'CIDRs allowed to bypass password in ipwhitelist mode'
  }
}

export function envHint(key: string, locale: 'zh-CN' | 'en-US'): string {
  const meta = ENV_META[key]
  if (!meta) return ''
  return locale === 'zh-CN' ? meta.zh : meta.en
}
