// Keyboard-wedge QR readers (USB HID, m6b-plan §6.4): the reader "types" the payload very fast and ends with
// Enter. A burst of at least WEDGE_MIN_CHARS printable keys, each within WEDGE_MAX_GAP_MS of the previous one,
// followed by Enter is a scan; a person typing is slower and never triggers it. Keys typed inside text inputs
// are left alone (the field gets them, as usual).
import { useEffect, useRef } from 'react';

export const WEDGE_MIN_CHARS = 8;
export const WEDGE_MAX_GAP_MS = 50;

export type WedgeDetector = {
  /** Feeds one key; returns the scanned text when the key completes a scan, else null. */
  key(key: string, at: number): string | null;
  reset(): void;
};

/** Pure detector of keyboard-wedge bursts (unit tested with explicit timestamps). */
export function createWedgeDetector(
  minChars = WEDGE_MIN_CHARS,
  maxGapMs = WEDGE_MAX_GAP_MS,
): WedgeDetector {
  let buffer = '';
  let last = -Infinity;
  return {
    key(key, at) {
      const gap = at - last;
      last = at;
      if (key === 'Enter') {
        const text = gap <= maxGapMs && buffer.length >= minChars ? buffer : null;
        buffer = '';
        return text;
      }
      if (gap > maxGapMs) buffer = '';
      // Shift and other non-printable keys keep a burst going without adding to it.
      if (key.length === 1) buffer += key;
      return null;
    },
    reset() {
      buffer = '';
      last = -Infinity;
    },
  };
}

/** True when the event target edits text (inputs, text areas, selects, content-editable elements). */
export function isTextTarget(target: EventTarget | null): boolean {
  if (!target || typeof (target as HTMLElement).tagName !== 'string') return false;
  const el = target as HTMLElement;
  if (el.isContentEditable) return true;
  const tag = el.tagName;
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag !== 'INPUT') return false;
  const type = (el as HTMLInputElement).type;
  return ![
    'button',
    'checkbox',
    'radio',
    'submit',
    'reset',
    'file',
    'image',
    'range',
    'color',
  ].includes(type);
}

/** Keyboard-wedge (USB HID) QR readers: >= 8 characters within 50 ms each, ending with Enter; keys typed inside
 * text inputs are ignored. The Enter that ends a scan is swallowed (it must not submit a focused form). */
export function useKeyboardWedge(onScan: (text: string) => void): void {
  const cb = useRef(onScan);
  useEffect(() => {
    cb.current = onScan;
  });
  useEffect(() => {
    const detector = createWedgeDetector();
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey || isTextTarget(e.target)) {
        detector.reset();
        return;
      }
      const text = detector.key(e.key, e.timeStamp || performance.now());
      if (text !== null) {
        e.preventDefault();
        cb.current(text);
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, []);
}
