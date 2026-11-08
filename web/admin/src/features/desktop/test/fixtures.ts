import type {
  DesktopBackupList,
  DesktopSettings,
  DesktopStatus,
  InstanceMeta,
  LanStatus,
} from '@porto/shared/api';
import type { MockRoute } from '@porto/shared/testing';

export const D = '/api/desktop/v1';

export function meta(overrides: Partial<InstanceMeta> = {}): InstanceMeta {
  return {
    version: '1.0.0',
    api_version: '1',
    edition: 'desktop',
    capabilities: ['desktop.local'],
    demo_pairing: false,
    local_mode: true,
    pricing_engine_version: 1,
    ...overrides,
  };
}

export const status = (overrides: Partial<DesktopStatus> = {}): DesktopStatus => ({
  version: '1.0.0',
  mode: 'local',
  first_run_pending: false,
  update: null,
  secret_store: 'keyring',
  kiosk: false,
  ...overrides,
});

export const settings: DesktopSettings = {
  mode: 'local',
  port: 18090,
  lan_enabled: false,
  lan_port: 8443,
  lan_address: '',
  backup_dir: '',
  backup_keep_daily: 14,
  backup_keep_exit: 5,
  update_check: true,
  dismissed_update: '',
  data_dir: '/Users/test/Library/Application Support/Porto',
  logs_dir: '/Users/test/Library/Application Support/Porto/logs',
};

export const lanOff: LanStatus = {
  enabled: false,
  listening: false,
  url: '',
  address: '',
  addresses: ['192.168.1.20', '10.0.0.5'],
  port: 8443,
  fingerprint: '',
  ca_fingerprint: '',
  cert_expires_at: null,
  error: null,
  ca_constrained: false,
  ca_download_url: '',
};

export const lanOn: LanStatus = {
  ...lanOff,
  enabled: true,
  listening: true,
  url: 'https://192.168.1.20:8443',
  address: '192.168.1.20',
  fingerprint: 'ab12cd34',
  ca_fingerprint: 'ff00',
  cert_expires_at: '2027-12-01T10:00:00Z',
};

export const backups: DesktopBackupList = {
  items: [
    {
      id: 'porto-20261106-230000-daily.tar.gz',
      kind: 'daily',
      size_bytes: 12_345_678,
      created_at: '2026-11-06T22:00:00Z',
      sha256: 'aa',
    },
    {
      id: 'porto-20261105-180000-exit.tar.gz',
      kind: 'exit',
      size_bytes: 2_000,
      created_at: '2026-11-05T17:00:00Z',
      sha256: 'bb',
    },
  ],
  status: {
    dir: '/Users/test/Library/Application Support/Porto/backups',
    fallback: false,
    last_daily_at: '2026-11-06T22:00:00Z',
    last_error: null,
    next_due_at: '2026-11-07T22:00:00Z',
  },
};

/** Routes of the desktop window (loopback): meta, status and the owner settings. */
export function desktopRoutes(over: { status?: DesktopStatus; lan?: LanStatus } = {}): MockRoute[] {
  return [
    { path: '/api/admin/v1/meta', body: meta() },
    { path: `${D}/status`, body: over.status ?? status() },
    { path: `${D}/settings`, body: settings },
    { path: `${D}/lan`, body: over.lan ?? lanOff },
    { path: `${D}/backups`, body: backups },
  ];
}
