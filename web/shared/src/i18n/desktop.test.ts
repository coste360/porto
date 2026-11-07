import { describe, expect, it } from 'vitest';

import { resources } from './resources';

function keys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object') return [prefix];
  return Object.entries(value as Record<string, unknown>).flatMap(([k, v]) =>
    keys(v, prefix ? `${prefix}.${k}` : k),
  );
}

describe('M6a i18n', () => {
  it.each(['desktop', 'devices', 'errors'] as const)('%s has the same keys in it and en', (ns) => {
    expect(keys(resources.en[ns]).sort()).toEqual(keys(resources.it[ns]).sort());
  });

  it('has every M6a problem slug and the pairing/desktop keys', () => {
    for (const slug of [
      'first_run_token_invalid',
      'first_run_done',
      'lan_port_unavailable',
      'lan_no_address',
      'backup_in_progress',
      'restore_failed',
      'restore_backup_newer',
      'pairing_code_invalid',
    ]) {
      expect(resources.it.errors).toHaveProperty(slug);
      expect(resources.en.errors).toHaveProperty(slug);
    }
    for (const locale of ['it', 'en'] as const) {
      const d = resources[locale].desktop;
      for (const f of ['publicPage', 'onlinePayments', 'paymentLinks', 'remoteSignature'] as const)
        expect(d.localMode[f]).toBeTruthy();
      for (const k of ['daily', 'exit', 'manual', 'pre-restore'] as const)
        expect(d.backup.kinds[k]).toBeTruthy();
      for (const p of ['ios', 'android', 'windows', 'macos', 'linux', 'other'] as const) {
        expect(d.pair.platforms[p]).toBeTruthy();
        expect(resources[locale].devices.platforms[p]).toBeTruthy();
      }
      expect(resources[locale].devices.accessKinds.browser).toBeTruthy();
    }
  });
});
