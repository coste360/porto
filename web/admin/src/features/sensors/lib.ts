// Pure helpers of the Sensori pages (m12-plan §16.1): metres ↔ integer centimetres for segments, the actions an
// alert dialog needs, the HMAC example shown next to a new key. No API, no clock.
import type { SensorAlert, SensorAlertAction, SensorSettingsInput } from '@porto/shared/api';

export const SENSOR_KINDS = [
  'ultrasonic',
  'radar',
  'magnetometer',
  'infrared',
  'camera',
  'other',
] as const;
export const RESOURCE_TYPES = ['berth', 'dynamic_area', 'pedestal'] as const;
export const GATEWAY_KINDS = ['lorawan', 'nbiot', 'mqtt_bridge', 'other'] as const;
export const ALERT_KINDS = [
  'unexpected_occupied',
  'transit_arrival',
  'return_detected',
  'release_proposal',
  'early_return',
  'device_silent',
  'battery_low',
] as const;
export const ALERT_STATUSES = ['open', 'confirmed', 'dismissed', 'resolved'] as const;
export const TASK_PRIORITIES = ['low', 'normal', 'high', 'urgent'] as const;

/** Parses metres typed by the operator ("12", "12,5", "12.50") into integer centimetres; null when invalid. */
export function metresToCm(text: string): number | null {
  const t = text.trim().replace(',', '.');
  if (!/^\d{1,4}(\.\d{1,2})?$/.test(t)) return null;
  const [whole = '0', frac = ''] = t.split('.');
  return Number(whole) * 100 + Number((frac + '00').slice(0, 2));
}

/** Formats integer centimetres as metres with two decimals and a comma ("12,50"). */
export function cmToMetres(cm: number): string {
  const sign = cm < 0 ? '-' : '';
  const a = Math.abs(Math.trunc(cm));
  return `${sign}${Math.trunc(a / 100)},${String(a % 100).padStart(2, '0')}`;
}

/** The segment check of the device form: from < to, at least 1 m, inside the area length. */
export function segmentIssue(
  from: number | null,
  to: number | null,
  lengthCm?: number,
): string | null {
  if (from === null || to === null) return 'segmentInvalid';
  if (from < 0 || to <= from) return 'segmentOrder';
  if (to - from < 100) return 'segmentShort';
  if (lengthCm !== undefined && to > lengthCm) return 'segmentOutside';
  return null;
}

/** Whether an action needs a dialog with fields (release range, task priority, dismiss note). */
export function actionNeedsDialog(a: SensorAlertAction): boolean {
  return a === 'confirm_release' || a === 'create_task' || a === 'dismiss';
}

/** The proposed release range of an alert (confirm_release), or null. */
export function proposalOf(a: Pick<SensorAlert, 'refs'>): { from: string; to: string } | null {
  const { proposed_from: from, proposed_to: to } = a.refs;
  return from && to ? { from, to } : null;
}

/** Whether a chosen range lies inside the proposal (ISO dates compare as strings). */
export function rangeInside(p: { from: string; to: string }, from: string, to: string): boolean {
  return from >= p.from && to <= p.to && from <= to;
}

/** Defaults of the settings (m12-plan §6) used before the first save. */
export const DEFAULT_SETTINGS: SensorSettingsInput = {
  hysteresis_readings: 2,
  hysteresis_minutes: 5,
  offline_after_minutes: 30,
  silent_alert_minutes: 180,
  battery_low_pct: 20,
  unexpected_after_minutes: 15,
  departure_cutoff_minutes: 720,
  presence_effect_enabled: false,
  presence_out_after_hours: 6,
  release_proposal_after_days: 3,
  release_proposal_nights: 7,
};

/** Range checks of the settings form: the field names that are out of range. */
export function settingsIssues(s: SensorSettingsInput): string[] {
  const out: string[] = [];
  const rng = (k: keyof SensorSettingsInput, lo: number, hi: number) => {
    const v = s[k];
    if (typeof v !== 'number' || !Number.isInteger(v) || v < lo || v > hi) out.push(k);
  };
  rng('hysteresis_readings', 1, 10);
  rng('hysteresis_minutes', 1, 60);
  rng('offline_after_minutes', 5, 1440);
  rng('silent_alert_minutes', 30, 10080);
  rng('battery_low_pct', 1, 90);
  rng('unexpected_after_minutes', 0, 240);
  rng('departure_cutoff_minutes', 0, 1439);
  rng('presence_out_after_hours', 1, 168);
  rng('release_proposal_after_days', 1, 60);
  rng('release_proposal_nights', 1, 60);
  if (!out.includes('silent_alert_minutes') && s.silent_alert_minutes < s.offline_after_minutes)
    out.push('silent_alert_minutes');
  return out;
}

/** The signing example shown with a new device key (docs/sensor-protocol.md §4.2), shell syntax. */
export function hmacExample(key: string, baseUrl: string): string {
  const keyId = key.split('_').slice(0, 3).join('_');
  return [
    `KEY='${key}'`,
    `BODY='{"readings":[{"device_id":"<external_id>","observed_at":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'","state":"occupied"}]}'`,
    `T=$(date +%s)`,
    `H=$(printf %s "$BODY" | openssl dgst -sha256 -hex | sed 's/^.* //')`,
    `SIG=$(printf 'v1\\n%s\\nPOST\\n/api/ingest/v1/readings\\n%s' "$T" "$H" | openssl dgst -sha256 -hmac "$KEY" -hex | sed 's/^.* //')`,
    `curl -X POST ${baseUrl}/api/ingest/v1/readings -H 'Content-Type: application/json' \\`,
    `  -H 'Porto-Key: ${keyId}' -H "Porto-Signature: t=$T,v1=$SIG" -d "$BODY"`,
  ].join('\n');
}
