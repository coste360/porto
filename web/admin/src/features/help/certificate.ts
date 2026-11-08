// Helpers of the certificate help page (m6b-plan §8.5).

/** Download paths of the marina's local CA on the local-network listener (m6b-plan §8.5). */
export const CA_DOWNLOAD = '/lan/porto-ca.crt';
export const CA_PROFILE = '/lan/porto-ca.mobileconfig';

/** The CA fingerprint passed by the pairing page (?fp=), shown for comparison; hex and colons only. */
export function fingerprintFromSearch(search: string): string | null {
  const fp = new URLSearchParams(search).get('fp');
  return fp && /^[0-9A-Fa-f:]{16,128}$/.test(fp) ? fp.toUpperCase() : null;
}
