// Per-tab storage of the public booking credentials (m8-plan §10): sessionStorage, which inside an iframe is
// partitioned by the embedding site, with an in-memory fallback when storage is blocked (private mode, third-party
// storage disabled). No cookies, no localStorage: a token never outlives the tab.

const memory = new Map<string, string>();

function session(): Storage | null {
  try {
    const s = window.sessionStorage;
    const probe = '__porto_probe__';
    s.setItem(probe, '1');
    s.removeItem(probe);
    return s;
  } catch {
    return null;
  }
}

export function readItem(key: string): string | null {
  const s = session();
  if (s) {
    try {
      return s.getItem(key);
    } catch {
      /* fall through to memory */
    }
  }
  return memory.get(key) ?? null;
}

export function writeItem(key: string, value: string): void {
  memory.set(key, value);
  const s = session();
  if (!s) return;
  try {
    s.setItem(key, value);
  } catch {
    /* quota or blocked: the memory copy is enough for this page */
  }
}

export function removeItem(key: string): void {
  memory.delete(key);
  const s = session();
  if (!s) return;
  try {
    s.removeItem(key);
  } catch {
    /* ignore */
  }
}

export function readJSON<T>(key: string, valid: (v: unknown) => v is T): T | null {
  const raw = readItem(key);
  if (!raw) return null;
  try {
    const v: unknown = JSON.parse(raw);
    return valid(v) ? v : null;
  } catch {
    return null;
  }
}

export function writeJSON(key: string, value: unknown): void {
  writeItem(key, JSON.stringify(value));
}
