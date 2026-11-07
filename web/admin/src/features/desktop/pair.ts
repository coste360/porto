// Pure helpers of the browser pairing page (m6a-plan §4.1).
import type { Device } from '@porto/shared/api';

type Platform = Device['platform'];

/** `#c=<code>&fp=<fingerprint>` of the QR link (the code never travels in a request line). */
export function parsePairFragment(hash: string): { code: string; fingerprint: string } {
  const params = new URLSearchParams(hash.replace(/^#/, ''));
  return {
    code: normalizeCode(params.get('c') ?? ''),
    fingerprint: (params.get('fp') ?? '').trim(),
  };
}

/** Upper case, no spaces, dash after the fourth character: "abcd efgh" → "ABCD-EFGH". */
export function normalizeCode(raw: string): string {
  const compact = raw
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, '')
    .slice(0, 8);
  return compact.length > 4 ? `${compact.slice(0, 4)}-${compact.slice(4)}` : compact;
}

/** Platform and kind of this browser from its user agent (only to prefill the device name). */
export function detectDevice(ua: string): { platform: Platform; kind: Device['kind'] } {
  const s = ua.toLowerCase();
  if (/android/.test(s)) return { platform: 'android', kind: 'mobile' };
  if (/iphone|ipad|ipod/.test(s)) return { platform: 'ios', kind: 'mobile' };
  if (/windows/.test(s)) return { platform: 'windows', kind: 'desktop' };
  if (/mac os x|macintosh/.test(s)) return { platform: 'macos', kind: 'desktop' };
  if (/linux|cros/.test(s))
    return { platform: 'linux', kind: /mobi/.test(s) ? 'mobile' : 'desktop' };
  return { platform: 'other', kind: /mobi/.test(s) ? 'mobile' : 'desktop' };
}
