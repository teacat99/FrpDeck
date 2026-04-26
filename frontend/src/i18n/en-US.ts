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
    field: {
      name: 'Name',
      group: 'Group',
      addr: 'Address',
      port: 'Port',
      protocol: 'Protocol',
      token: 'Auth token',
      token_keep: 'Leave blank to keep the existing value',
      user: 'Username',
      driver: 'Driver',
      enabled: 'Enabled',
      auto_start: 'Auto start'
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
    status: {
      pending: 'Pending',
      active: 'Active',
      expired: 'Expired',
      stopped: 'Stopped',
      failed: 'Failed'
    },
    field: {
      name: 'Name',
      endpoint: 'Endpoint',
      type: 'Type',
      target: 'local → remote',
      status: 'Status',
      local_ip: 'Local IP',
      local_port: 'Local port',
      remote_port: 'Remote port',
      subdomain: 'Subdomain',
      custom_domains: 'Custom domains (comma-separated)'
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
      notConfigured: 'not configured'
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
