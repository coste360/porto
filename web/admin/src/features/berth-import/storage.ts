import type { MappingField } from './csv';

// Column mappings remembered per marina (m1-plan §5.5): header names only, never file content.
const key = (marinaId: string) => `porto.berth-import.mapping.${marinaId}`;

export function loadSavedMapping(marinaId: string): Partial<Record<MappingField, string>> | null {
  try {
    const raw = localStorage.getItem(key(marinaId));
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return null;
    const out: Partial<Record<MappingField, string>> = {};
    for (const [k, v] of Object.entries(parsed)) {
      if (typeof v === 'string') out[k as MappingField] = v.slice(0, 100);
    }
    return out;
  } catch {
    return null;
  }
}

export function saveMapping(marinaId: string, names: Partial<Record<MappingField, string>>): void {
  try {
    localStorage.setItem(key(marinaId), JSON.stringify(names));
  } catch {
    // Storage unavailable (private mode, quota): the mapping is just not remembered.
  }
}
