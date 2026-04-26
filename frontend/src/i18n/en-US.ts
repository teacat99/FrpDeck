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
