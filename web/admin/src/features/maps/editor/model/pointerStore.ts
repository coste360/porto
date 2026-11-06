// Pointer state shared by the canvas overlay and the status bar without re-rendering the whole editor on every
// mouse move (m1-plan §5.3 performance target).
import { useSyncExternalStore } from 'react';

import type { Box, Pt } from '@porto/shared/map';

import type { SnapResult } from './snap';

export type PointerState = {
  /** Cursor in map cm (floats). */
  cursor: Pt | null;
  /** Snapped point under the cursor for creation tools. */
  hover: SnapResult | null;
  marquee: Box | null;
};

export function createPointerStore() {
  let state: PointerState = { cursor: null, hover: null, marquee: null };
  const listeners = new Set<() => void>();
  return {
    get: () => state,
    set(patch: Partial<PointerState>) {
      state = { ...state, ...patch };
      for (const l of listeners) l();
    },
    subscribe(l: () => void) {
      listeners.add(l);
      return () => listeners.delete(l);
    },
  };
}

export type PointerStore = ReturnType<typeof createPointerStore>;

export function usePointer(store: PointerStore): PointerState {
  return useSyncExternalStore(store.subscribe, store.get, store.get);
}
