// Per-viewer conveniences of the operational view (D24): last lens and draw mode, and the last undoable move.
// localStorage only, wrapped in try/catch; losing them is harmless.
import { isLensId, type LensId } from '@porto/shared/map';

const LENS_KEY = 'porto:ops:lens';
const MODE_KEY = 'porto:ops:mode';
const UNDO_KEY = 'porto:ops:undo';

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null): void {
  try {
    if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    // Storage blocked: nothing to remember.
  }
}

export function rememberedLens(userId: string): LensId | null {
  const v = read(`${LENS_KEY}:${userId}`);
  return isLensId(v) ? v : null;
}

export function rememberLens(userId: string, lens: LensId): void {
  write(`${LENS_KEY}:${userId}`, lens);
}

export function rememberedMode(userId: string): 'scale' | 'schema' | null {
  const v = read(`${MODE_KEY}:${userId}`);
  return v === 'scale' || v === 'schema' ? v : null;
}

export function rememberMode(userId: string, mode: 'scale' | 'schema'): void {
  write(`${MODE_KEY}:${userId}`, mode);
}

export type LastUndo = { undoId: string; until: string; marinaId: string };

export function lastUndo(marinaId: string, now: Date = new Date()): LastUndo | null {
  try {
    const raw = read(UNDO_KEY);
    if (!raw) return null;
    const v = JSON.parse(raw) as LastUndo;
    if (v.marinaId !== marinaId || new Date(v.until) <= now) return null;
    return v;
  } catch {
    return null;
  }
}

export function rememberUndo(v: LastUndo | null): void {
  write(UNDO_KEY, v ? JSON.stringify(v) : null);
}
