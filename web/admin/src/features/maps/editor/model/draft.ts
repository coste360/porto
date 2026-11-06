// Local draft of unsaved changes (m1-plan §5.3), per map and version, in localStorage. Every access is
// wrapped: storage may be missing or blocked, and the editor works without it.
import type { MapSaveRequest } from '@porto/shared/api';

const PREFIX = 'porto:map-draft:';

export type Draft = { savedAt: string; body: MapSaveRequest };

export function draftKey(mapId: string, version: number): string {
  return `${PREFIX}${mapId}:${version}`;
}

export function saveDraft(
  mapId: string,
  version: number,
  body: MapSaveRequest,
  now: Date = new Date(),
): void {
  try {
    localStorage.setItem(
      draftKey(mapId, version),
      JSON.stringify({ savedAt: now.toISOString(), body }),
    );
  } catch {
    // Quota exceeded or storage blocked: the draft is a convenience only.
  }
}

export function loadDraft(mapId: string, version: number): Draft | null {
  try {
    const raw = localStorage.getItem(draftKey(mapId, version));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Draft;
    return parsed && typeof parsed.savedAt === 'string' && parsed.body ? parsed : null;
  } catch {
    return null;
  }
}

/** Removes the drafts of a map (every version). */
export function clearDrafts(mapId: string): void {
  try {
    const keys: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k?.startsWith(`${PREFIX}${mapId}:`)) keys.push(k);
    }
    for (const k of keys) localStorage.removeItem(k);
  } catch {
    // ignore
  }
}
