export default {
  app: {
    title: 'FrpDeck',
    subtitle: 'frp control deck · multi-frps + ephemeral tunnels'
  },
  theme: {
    light: 'Light',
    dark: 'Dark',
    auto: 'System',
    switchTo: {
      light: 'Switch to light',
      dark: 'Switch to dark',
      auto: 'Switch to system'
    }
  },
  menu: {
    home: 'Home',
    endpoints: 'Endpoints',
    tunnels: 'Tunnels',
    history: 'History',
    users: 'Users',
    settings: 'Settings'
  },
  role: {
    admin: 'admin',
    user: 'user'
  },
  common: {
    cancel: 'Cancel',
    confirm: 'Confirm',
    save: 'Save',
    delete: 'Delete',
    edit: 'Edit',
    close: 'Close',
    refresh: 'Refresh',
    actions: 'Actions',
    created: 'Created',
    updated: 'Updated',
    deleted: 'Deleted',
    on: 'Enabled',
    off: 'Disabled',
    all: 'All'
  },
  pwa: {
    title: 'Install FrpDeck to your home screen',
    desc: 'Standalone window, faster launch and a desktop icon',
    install: 'Install',
    later: 'Later',
    iosHint: 'In Safari, tap "Share" → "Add to Home Screen"',
    dismissHint: 'Stop reminding for 14 days'
  },
  action: {
    submit: 'Submit',
    cancel: 'Cancel',
    confirm: 'Confirm',
    save: 'Save',
    saving: 'Saving…',
    refresh: 'Refresh',
    reset: 'Reset',
    login: 'Sign in',
    logout: 'Sign out',
    search: 'Search',
    change_password: 'Change password',
    reset_password: 'Reset password',
    enable: 'Enable',
    disable: 'Disable',
    new_user: 'New user'
  },
  password: {
    old: 'Current password',
    new: 'New password',
    confirm: 'Confirm new password',
    too_short: 'At least 6 characters',
    mismatch: 'Passwords do not match',
    changed: 'Password updated',
    strength: {
      weak: 'Too weak',
      fair: 'Fair',
      medium: 'Medium',
      good: 'Good',
      strong: 'Strong'
    }
  },
  login: {
    title: 'Sign in to FrpDeck',
    username: 'Username',
    usernamePlaceholder: 'Enter your username',
    password: 'Password',
    passwordPlaceholder: 'Enter your password',
    captcha: 'Captcha',
    captchaPlaceholder: 'Answer here',
    captchaRefresh: 'Refresh',
    failed: 'Sign in failed',
    submit: 'Sign in',
    submitting: 'Signing in…',
    lockedUntil: 'Sign in is temporarily locked due to repeated failures',
    retryIn: 'Retry in {seconds}s',
    welcomeBack: 'Welcome back',
    lastLoginAt: 'Last sign-in: {at}',
    lastLoginFrom: 'from {ip}'
  },
  home: {
    title: 'Welcome back',
    subtitle: 'See every frps endpoint and the tunnels riding on top of them',
    cards: {
      endpoints: 'frps endpoints',
      endpoints_hint: 'Configured remote servers',
      tunnels: 'Configured tunnels',
      tunnels_hint: 'All frp proxies / visitors',
      active: 'Active',
      active_hint: 'Tunnels currently up',
      expiring: 'Ephemeral',
      expiring_hint: 'Have an expiry timestamp'
    },
    next_steps: {
      title: 'Get started',
      subtitle: 'Begin by adding your first frps endpoint'
    }
  },
  endpoint: {
    title: 'frps endpoints',
    subtitle: 'Manage the frps servers FrpDeck connects to',
    add: 'New endpoint',
    edit: 'Edit endpoint',
    empty: 'No frps endpoint yet',
    empty_hint: 'Add the first endpoint, then you can mount tunnels on it',
    required: 'Name and address are required',
    invalid_port: 'Port must be between 1 and 65535',
    confirm_delete: 'Delete endpoint {name} and every tunnel under it?',
    advanced: 'Advanced',
    advanced_hide: 'Hide',
    field: {
      name: 'Name',
      group: 'Group',
      addr: 'Address',
      port: 'Port',
      protocol: 'Protocol',
      token: 'Auth token',
      token_keep: 'Leave blank to keep the existing value',
      meta_token: 'Meta token',
      meta_token_hint: 'Optional, used for multi-user metadata',
      user: 'Username',
      driver: 'Driver',
      tls_enable: 'Enable TLS',
      tls_enable_hint: 'frpc enables TLS by default when talking to frps',
      tls_config: 'TLS config (PEM path or content)',
      pool_count: 'Pool count',
      pool_count_hint: '0 means open connections on demand',
      heartbeat_interval: 'Heartbeat interval (s)',
      heartbeat_timeout: 'Heartbeat timeout (s)',
      enabled: 'Enabled',
      auto_start: 'Auto start',
      live_state: 'Live'
    },
    state: {
      disconnected: 'Disconnected',
      connecting: 'Connecting',
      connected: 'Connected',
      failed: 'Failed'
    }
  },
  tunnel: {
    title: 'Tunnels',
    subtitle: 'Configure proxy / visitor entries for each frps endpoint',
    add: 'New tunnel',
    edit: 'Edit tunnel',
    empty: 'No tunnel configured',
    empty_hint: 'Pick an endpoint to mount your first tunnel',
    no_endpoint: 'Add an frps endpoint first',
    no_endpoint_hint: 'Tunnels can only be mounted on top of an endpoint',
    required_name: 'Tunnel name is required',
    required_endpoint: 'Please pick an endpoint',
    confirm_delete: 'Delete tunnel {name}?',
    started: 'Tunnel started',
    stopped: 'Tunnel stopped',
    renewed: 'Renewed',
    renewed_permanent: 'Now permanent',
    advanced: 'Advanced',
    advanced_hide: 'Hide',
    renew: {
      label: 'Renew',
      plus_1h: '+1 hour',
      plus_1d: '+1 day',
      plus_7d: '+7 days',
      permanent: 'Make permanent'
    },
    notify: {
      expiring_title: 'Tunnel {name} expiring soon',
      expiring_body: 'Auto-stop in ~{minutes} minutes — renew from the Tunnels page.'
    },
    diag: {
      action: 'Run connectivity check',
      title: 'Connectivity check',
      subtitle: 'Probes DNS / frps port / session register / local service in order. Fix and re-run as needed.',
      running: 'Running checks…',
      rerun: 'Re-run',
      overall: 'Overall',
      status: {
        ok: 'OK',
        warn: 'Warning',
        fail: 'Failed',
        skipped: 'Skipped'
      },
      check: {
        dns: 'DNS resolution',
        tcp_probe: 'frps port probe',
        frps_register: 'frps session state',
        local_reach: 'Local service reachable'
      }
    },
    advice: {
      action: 'frps config helper',
      title: 'What your frps.toml needs',
      subtitle: 'Reverse-engineered from the "{name}" tunnel fields.',
      loading: 'Generating advice…',
      empty: 'No additional frps configuration required for this tunnel.',
      severity: {
        required: 'Required',
        recommended: 'Recommended',
        info: 'Info',
        warn: 'Warning'
      },
      docs: 'Open docs',
      caveats: 'Caveats',
      snippet: 'frps.toml snippet (paste-ready)',
      copy: 'Copy snippet',
      copied: 'Copied to clipboard'
    },
    section: {
      basic: 'Basic',
      proxy: 'Proxy (public ingress)',
      http: 'HTTP / HTTPS',
      secret: 'STCP / XTCP / SUDP',
      visitor: 'Visitor (dialer)',
      lifecycle: 'Lifecycle',
      advanced: 'Advanced'
    },
    role: {
      label: 'Role',
      server: 'Server (exposed side)',
      visitor: 'Visitor (dialer)'
    },
    expire: {
      label: 'Expires at',
      hint: 'FrpDeck stops this tunnel automatically once it expires',
      forever: 'Never',
      cleared: 'Expiry cleared',
      preset_2h: '+2 hours',
      preset_1d: '+1 day',
      preset_7d: '+7 days',
      remaining: '{value} left',
      expired: 'Expired'
    },
    status: {
      pending: 'Pending',
      active: 'Active',
      expired: 'Expired',
      stopped: 'Stopped',
      failed: 'Failed'
    },
    live: {
      pending: 'Pending',
      starting: 'Starting',
      running: 'Running',
      check_failed: 'Health-check failed',
      stopped: 'Stopped',
      error: 'Error'
    },
    validation: {
      type_required: 'Pick a tunnel type',
      visitor_only_for_secret: 'Visitor is only valid for stcp / xtcp / sudp',
      sk_required: 'SK is required',
      server_name_required: 'Server name is required',
      domains_required: 'HTTP/HTTPS needs at least a subdomain or custom_domains',
      port_range: 'Port must be between 0 and 65535',
      expire_in_past: 'Expiry must be in the future'
    },
    field: {
      name: 'Name',
      endpoint: 'Endpoint',
      type: 'Type',
      target: 'local → remote',
      status: 'Status',
      expire: 'Expires',
      local_ip: 'Local IP',
      local_port: 'Local port',
      remote_port: 'Remote port',
      subdomain: 'Subdomain',
      custom_domains: 'Custom domains (comma-separated)',
      locations: 'URL locations (comma-separated)',
      http_user: 'HTTP user',
      http_password: 'HTTP password',
      http_password_keep: 'Leave blank to keep existing',
      host_header_rewrite: 'Host header rewrite',
      sk: 'Shared secret (SK)',
      sk_keep: 'Leave blank to keep existing',
      allow_users: 'Allowed visitor users (comma-separated, * for all)',
      server_name: 'Target server name',
      encryption: 'Encryption',
      compression: 'Compression',
      bandwidth_limit: 'Bandwidth limit',
      bandwidth_limit_hint: 'e.g. 1MB, 512KB',
      group: 'Load-balance group',
      group_key: 'Group key',
      health_check_type: 'Health-check type',
      health_check_url: 'Health-check URL',
      plugin: 'Plugin',
      plugin_config: 'Plugin params (key=val,key=val)',
      enabled: 'Enabled',
      auto_start: 'Auto start'
    }
  },
  template: {
    audience: 'Best for',
    wizard: {
      action: 'Templates',
      title: 'Create from a scenario template',
      subtitle: 'Pick a common scenario and FrpDeck pre-fills the type / ports / role; you can still tweak details before save.',
      loading: 'Loading templates…',
      empty: 'No templates available'
    },
    'web-http': {
      name: 'Expose an HTTP website',
      desc: 'http + custom domain or subdomain. Good for personal blogs and dashboards over plain HTTP.',
      audience: 'You run an internal HTTP service and want to share it with friends/teammates.'
    },
    'web-http.prereq.vhost': 'frps.toml has vhostHTTPPort configured (default 80).',
    'web-http.prereq.dns': 'Target domain DNS points to the frps public IP.',
    'web-https': {
      name: 'Expose an HTTPS website',
      desc: 'https + custom domain. frps does SNI routing; the internal service terminates TLS itself.',
      audience: 'Your internal service already has TLS; expose it as https://your-domain directly.'
    },
    'web-https.prereq.vhost': 'frps.toml has vhostHTTPSPort configured (default 443).',
    'web-https.prereq.cert': 'Internal service has a TLS cert ready (or use wildcard cert + frps https2http plugin).',
    'web-https.prereq.dns': 'Target domain DNS points to the frps public IP.',
    rdp: {
      name: 'RDP remote desktop',
      desc: 'tcp + remote_port. Tunnel Windows Remote Desktop through frps.',
      audience: 'Connect to your home/office Windows desktop while travelling.'
    },
    'rdp.prereq.allowports': 'frps allowPorts includes 13389 (or it is empty / unrestricted).',
    'rdp.prereq.firewall': 'Windows firewall allows RDP (default port 3389).',
    ssh: {
      name: 'SSH bastion',
      desc: 'tcp + remote_port. Reach your internal Linux box with `ssh -p 22022 user@frps_addr`.',
      audience: 'Need remote SSH into your home/office Linux machine.'
    },
    'ssh.prereq.allowports': 'frps allowPorts includes 22022.',
    'ssh.prereq.sshd': 'Local sshd is running (default port 22).',
    'db-share': {
      name: 'Share MySQL/Redis with a teammate',
      desc: 'tcp + temporary tunnel (auto-stops after 4h by default). Burn after use; never leave a DB exposed long-term.',
      audience: 'Briefly let a teammate or client connect to your DB to debug.'
    },
    'db-share.prereq.allowports': 'frps allowPorts includes 13306.',
    'db-share.prereq.tempnote': 'Auto-expires in 4 hours; you can adjust or clear the expiry before saving.',
    'nas-p2p': {
      name: 'P2P access to home NAS',
      desc: 'xtcp visitor. frps coordinates the handshake; real traffic runs P2P directly to bypass relay bandwidth.',
      audience: 'Your home NAS is large and you do not want to relay traffic through frps.'
    },
    'nas-p2p.prereq.peer': 'The NAS side runs the matching xtcp server (same sk).',
    'nas-p2p.prereq.stun': 'frps.toml has natHoleStunServer configured.',
    'nas-p2p.prereq.shared-sk': 'Visitor and server must share the exact same sk.',
    socks5: {
      name: 'Private SOCKS5 proxy',
      desc: 'plugin: socks5. Expose a SOCKS5 endpoint via frps to drive system/browser traffic out through your home/office network.',
      audience: 'Want to use your home network egress (IP / region) from elsewhere.'
    },
    'socks5.prereq.allowports': 'frps allowPorts includes 11080.',
    'socks5.prereq.creds': 'Set plugin_user / plugin_passwd in plugin_config for auth.',
    'http-proxy': {
      name: 'HTTP forward proxy',
      desc: 'plugin: http_proxy. Lighter-weight HTTP proxy you can drop into a browser/curl http_proxy env var.',
      audience: 'You want an HTTP-only proxy egress; lighter than SOCKS5.'
    },
    'http-proxy.prereq.allowports': 'frps allowPorts includes 18888.',
    'http-proxy.prereq.creds': 'Set plugin_user / plugin_passwd in plugin_config for auth.',
    'static-file': {
      name: 'Static file sharing',
      desc: 'plugin: static_file. Expose a local directory over HTTP for download.',
      audience: 'One-off way to share large files without a cloud drive.'
    },
    'static-file.prereq.vhost': 'frps.toml has vhostHTTPPort configured and domain is resolved.',
    'static-file.prereq.path': 'plugin_local_path in plugin_config points to a real directory.',
    'frpdeck-self': {
      name: 'Remote-manage FrpDeck itself',
      desc: 'stcp (self-referencing). Expose your FrpDeck admin UI (127.0.0.1:8080) safely via frps.',
      audience: 'You want to log into your home FrpDeck admin while away.'
    },
    'frpdeck-self.prereq.shared-sk': 'The visitor side must use the same sk.',
    'frpdeck-self.prereq.password-mode': 'Strongly recommend FrpDeck auth=password (never `none`).',
    'frpdeck-self.prereq.visitor-side': 'Another machine must run a matching stcp visitor tunnel.'
  },
  history: {
    title: 'Audit log',
    subtitle: 'Every write touches this trail',
    empty: 'No audit record yet',
    empty_hint: 'Add / edit / delete actions show up here',
    field: {
      time: 'Time',
      action: 'Action',
      actor: 'Actor',
      detail: 'Detail'
    }
  },
  user: {
    add: 'New user',
    edit: 'Edit user',
    empty: 'No user yet',
    reset_password: 'Reset password',
    password_reset: 'Password reset',
    username_required: 'Username is required',
    password_too_short: 'Password must be at least 8 characters',
    confirm_delete: 'Delete user {name}?',
    field: {
      username: 'Username',
      password: 'Password',
      role: 'Role',
      disabled: 'Status',
      created_at: 'Created at'
    }
  },
  settings: {
    title: 'Settings',
    subtitle: 'Accounts, runtime parameters and audit',
    users_hint: 'Manage local sign-in accounts',
    tabs: {
      users: 'Accounts',
      runtime: 'Runtime',
      security: 'Security'
    },
    runtime: {
      intro: 'Changes apply live; the next cold start re-reads from SQLite.',
      dirty: '{n} change(s) pending',
      clean: 'All changes saved',
      saved: 'Saved',
      sectionRuleLimits: 'Tunnels & history',
      sectionLogin: 'Sign-in hardening',
      sectionDefence: 'Extra defence',
      sectionNotify: 'Notifications (ntfy)',
      sectionSystem: 'System info (read-only)',
      notConfigured: 'not configured',
      tunnelExpiringNotifyMinutes: 'Expiring notice (mins)',
      tunnelExpiringNotifyMinutesHelp: 'Toast + browser notification N mins before a tunnel expires; 0 disables, max 120'
    },
    notify: {
      test: 'Send test',
      testing: 'Sending…',
      testOk: 'Test sent',
      testFail: 'Test failed'
    },
    security: {
      filter_username: 'Filter by username',
      empty: 'No sign-in attempt recorded',
      field: {
        ip: 'IP',
        success: 'Result',
        reason: 'Reason'
      }
    }
  },
  msg: {
    loadFailed: 'Load failed',
    saveFailed: 'Save failed',
    deleteFailed: 'Delete failed',
    opFailed: 'Operation failed',
    invalidInput: 'Invalid input',
    saved: 'Saved',
    deleted: 'Deleted',
    durationExceeded: 'Duration exceeds the allowed maximum',
    expiryExceeded: 'Expiry exceeds the allowed maximum',
    rateLimitExceeded: 'Too many requests, please try again later',
    concurrentQuotaExceeded: 'Concurrent quota reached for this source'
  }
}
