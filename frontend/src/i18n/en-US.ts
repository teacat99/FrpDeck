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
    settings: 'Settings',
    remote: 'Remote',
    profiles: 'Profiles',
    android: 'Android'
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
    android_vpn_takeover: 'Takes over device traffic',
    android_vpn_takeover_hint: 'When this visitor + socks5 tunnel goes active, FrpDeck will request system VPN consent and route the entire device through this SOCKS5 endpoint.',
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
    import: {
      action: 'Import frpc.toml',
      title: 'Import tunnels from frpc.toml',
      subtitle: 'Upload or paste an existing frpc.toml/yaml/json. FrpDeck dry-runs the parse first so you can review the plan before any tunnels are created. Legacy INI is not supported.',
      upload_label: 'Pick a file',
      paste_label: 'Or paste here',
      placeholder: 'Paste your frpc.toml contents…',
      preview: 'Parse & preview',
      parsed_format: 'Detected format: {format}',
      file_warnings: 'File-level notes (these fields will not be imported)',
      endpoint_section: 'Parsed endpoint (reference only)',
      target_endpoint: 'Target endpoint',
      target_endpoint_placeholder: 'Select an existing endpoint',
      target_endpoint_hint: 'For safety, FrpDeck never creates endpoints implicitly. Add the target frps in the Endpoints page first, then choose it here.',
      tunnels_section: 'Tunnels ({count} parsed, {selected} selected)',
      tunnels_empty: 'No tunnels found in this configuration.',
      commit: 'Commit import ({count})',
      success: 'Imported {ok} tunnel(s) successfully',
      partial: 'Import done: {ok} ok, {fail} failed',
      partial_fail: 'Import failed: {ok} ok, {fail} failed',
      partial_with_skip: 'Imported {ok} tunnel(s); skipped {skipped} colliding name(s)',
      row_ok: 'Created',
      row_failed: 'Failed',
      row_skipped: 'Skipped',
      row_renamed: 'Renamed to {name}',
      default_conflict: 'Default on conflict',
      conflict: {
        badge: 'Name conflict',
        error: 'Error out',
        rename: 'Auto-rename',
        skip: 'Skip'
      },
      errors: {
        empty: 'Please upload a file or paste content first',
        endpoint_required: 'Please select a target endpoint',
        no_tunnels_selected: 'Please select at least one tunnel'
      }
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
  remote: {
    title: 'Remote management',
    subtitle: 'Pair another FrpDeck through an stcp tunnel and drive its UI from here.',
    auth_mode_required_title: 'Remote management requires password auth mode',
    auth_mode_required_hint: 'For audit traceability FrpDeck only enables this when the runtime auth mode is `password`.',
    tabs: {
      managed_by_me: 'Remotes I manage',
      manages_me: 'Hosts that can reach me'
    },
    refresh: 'Refresh',
    invite_action: 'Generate invitation',
    redeem_action: 'Add remote',
    invite: {
      title: 'Generate remote-management invitation',
      subtitle: 'Hand the invitation to another FrpDeck so it can take over this control panel through stcp.',
      endpoint: 'Transit frps',
      endpoint_hint: 'stcp traffic flows via this frps. Make sure both peers can reach it.',
      node_name: 'Node name',
      node_name_hint: 'Display only; leave blank to use the default',
      ui_scheme: 'Local UI scheme',
      ui_scheme_http: 'HTTP (default)',
      ui_scheme_https: 'HTTPS',
      submit: 'Generate',
      submitting: 'Generating…',
      result_title: 'Invitation ready',
      result_hint: 'Valid for 5 minutes. Hand it over fast — generate a fresh one if it expires.',
      result_qr_hint: 'Scan the QR or copy the invitation below',
      copy: 'Copy invitation',
      copied: 'Copied to clipboard',
      driver_warning: 'frpc driver not yet pushed: {msg}',
      close: 'Close'
    },
    redeem: {
      title: 'Add a new FrpDeck',
      subtitle: 'Paste the invitation; FrpDeck wires up an stcp visitor and signs you in automatically.',
      input_label: 'Invitation code',
      input_placeholder: 'Paste the full string supplied by the peer here',
      node_name: 'Local label',
      node_name_hint: 'Defaults to the name on the invitation',
      qr_upload: 'Scan QR image',
      qr_upload_hint: 'Upload the QR screenshot the peer shared and FrpDeck decodes it for you.',
      qr_decoding: 'Decoding…',
      qr_decoded: 'Invitation decoded from QR image',
      qr_failed: 'Could not decode a QR code from this image, retry or paste manually',
      submit: 'Connect',
      submitting: 'Connecting…',
      success: 'Connected to {name}',
      open_remote: 'Open remote console',
      driver_warning: 'frpc driver not yet pushed: {msg}',
      close: 'Close'
    },
    table: {
      name: 'Name',
      endpoint: 'Transit endpoint',
      bind_port: 'Local port',
      status: 'Status',
      last_seen: 'Last seen',
      created_at: 'Created',
      actions: 'Actions'
    },
    direction: {
      managed_by_me: 'I manage',
      manages_me: 'They reach me'
    },
    status: {
      pending: 'Pending',
      active: 'Active',
      offline: 'Offline',
      revoked: 'Revoked',
      expired: 'Expired'
    },
    open: 'Open remote',
    open_unavailable: 'No UI port or peer offline',
    refresh_invite: 'Refresh invitation',
    refresh_confirm: 'Rotate SK + management token and reissue the invitation? The previous code becomes invalid immediately.',
    refresh_success: 'Re-generated invitation for {name}',
    revoke: 'Revoke',
    revoke_confirm: 'Revoking deletes the auto-created stcp tunnel and invalidates the invitation. Continue?',
    revoked: 'Remote node revoked',
    revoke_token: 'Revoke token',
    revoke_token_confirm: 'Invalidate the currently issued management token without tearing down the pairing. Anyone holding the previous QR/token will be locked out. Continue?',
    token_revoked: 'Management token revoked; generate a new invitation when ready',
    empty_managed: 'No remote FrpDeck connected yet',
    empty_managed_hint: 'Click "Add remote" to paste an invitation',
    empty_manages: 'No outbound pairings',
    empty_manages_hint: 'Generate an invitation to let another FrpDeck reach this one',
    auto_login_busy: 'Logging in via invitation token…',
    auto_login_failed: 'Auto login via invitation failed: {msg}'
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
  },
  plugin: {
    empty_hint: 'Select a plugin to reveal its fields.',
    show_raw: 'Show / edit raw TOML',
    hide_raw: 'Hide raw TOML',
    raw_placeholder: 'e.g. localPath = "/srv"\nstripPrefix = "static"',
    extras_warning: 'Unknown lines were preserved verbatim; this editor cannot manage them.',
    unknown_hint: 'No schema bundled for "{plugin}" yet, edit the raw TOML manually.',
    field: {
      localPath: 'Local static file path',
      stripPrefix: 'Strip prefix',
      httpUser: 'HTTP user',
      httpPassword: 'HTTP password',
      unixPath: 'Unix socket path',
      username: 'Username',
      password: 'Password',
      localAddr: 'Local address',
      hostHeaderRewrite: 'Host header rewrite',
      crtPath: 'Certificate path',
      keyPath: 'Private key path'
    }
  },
  profile: {
    title: 'Profiles',
    subtitle: 'Switch the active (endpoint, tunnel) selection in one click — home / office / demo scenarios.',
    empty: 'No profiles yet — create one to bundle frequently used endpoints / tunnels.',
    new: 'New profile',
    edit: 'Edit profile',
    name: 'Profile name',
    activate: 'Activate',
    deactivate: 'Deactivate',
    deactivate_all: 'Clear active',
    bindings: 'Bindings',
    bindings_hint: 'Checked rows stay enabled when this profile is active; the rest are disabled.',
    binding_endpoint: 'Whole endpoint',
    binding_tunnel: 'Single tunnel',
    activate_success: 'Switched to “{name}”',
    deactivated: 'Active profile cleared',
    delete_active_blocked: 'Cannot delete the active profile — switch or deactivate first.',
    confirm_delete: 'Delete profile “{name}”?',
    saved: 'Profile saved',
    no_active: 'No profile is active; endpoint / tunnel toggles stay manual.',
    active_label: 'Active',
    edit_active_warn: 'Editing the active profile re-applies the binding set immediately.'
  },
  frpc: {
    download: 'Download frpc',
    download_hint: 'FrpDeck will fetch the frpc binary from GitHub into <data_dir>/bin.',
    downloading: 'Downloading…',
    downloaded: 'Downloaded: {path}',
    probe: 'Probe frpc version',
    probe_ok: 'Detected frpc {version}',
    probe_incompatible: 'Detected frpc {version} below required {min}',
    custom_path: 'Custom frpc path',
    custom_path_hint: 'Leave blank to use the FrpDeck-downloaded binary; otherwise the binary at this path.'
  },
  android: {
    title: 'Android settings',
    subtitle: 'Visible only inside the FrpDeck Android app — used to grant native permissions and manage backups',
    vpn_section: 'VPN takeover',
    vpn_explainer: 'When a tunnel with role=visitor + plugin=socks5 goes active, FrpDeck automatically asks for system VPN consent so device-wide traffic can route through the SOCKS5 visitor. Pre-authorising here avoids the consent dialog interrupting you later.',
    vpn_request_permission: 'Request VPN permission',
    vpn_permission_granted: 'VPN permission already granted',
    vpn_permission_pending: 'Not yet authorised — the system dialog reappears when a socks5 visitor activates',
    vpn_permission_ok: 'Permission granted',
    vpn_permission_denied: 'User denied permission',
    vpn_permission_failed: 'Permission request failed: {msg}',
    backup_section: 'Backup',
    backup_export: 'Export backup…',
    backup_import: 'Import backup…',
    backup_export_hint: 'Pack SQLite + settings into a zip and save to a user-picked location (Storage Access Framework)',
    backup_import_hint: 'Import stops the frpc engine, replaces local data, then auto-restarts',
    backup_export_ok: 'Exported {bytes} bytes',
    backup_import_ok: 'Restored {entries} entries; engine restarted',
    backup_failed: 'Operation failed: {msg}',
    about_section: 'About',
    about_version: 'Native version',
    about_engine: 'Engine endpoint'
  }
}
