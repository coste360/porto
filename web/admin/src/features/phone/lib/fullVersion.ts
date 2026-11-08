// "Versione completa" (m6b-plan §6.1): a phone user may open the normal shell; the overview then stops sending
// them back to the phone screen for the rest of the browser session. A per-tab convenience only.
const KEY = 'porto.phone.full';

export function preferFullVersion(): boolean {
  try {
    return sessionStorage.getItem(KEY) === '1';
  } catch {
    return false;
  }
}

export function setPreferFullVersion(on: boolean): void {
  try {
    if (on) sessionStorage.setItem(KEY, '1');
    else sessionStorage.removeItem(KEY);
  } catch {
    // Storage unavailable (private mode): the redirect simply happens again.
  }
}
