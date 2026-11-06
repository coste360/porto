import { apply, invert, isEmpty, type Command } from './commands';
import type { EditorDocument } from './document';

export const MAX_HISTORY = 200;

export type History = { readonly past: readonly Command[]; readonly future: readonly Command[] };

export const emptyHistory: History = { past: [], future: [] };

/** Runs a command and records it (the redo stack is dropped). */
export function execute(
  doc: EditorDocument,
  history: History,
  cmd: Command,
): { doc: EditorDocument; history: History } {
  if (isEmpty(cmd)) return { doc, history };
  const past = [...history.past, cmd];
  if (past.length > MAX_HISTORY) past.splice(0, past.length - MAX_HISTORY);
  return { doc: apply(doc, cmd), history: { past, future: [] } };
}

export function undo(
  doc: EditorDocument,
  history: History,
): { doc: EditorDocument; history: History } {
  const cmd = history.past[history.past.length - 1];
  if (!cmd) return { doc, history };
  return {
    doc: invert(doc, cmd),
    history: { past: history.past.slice(0, -1), future: [cmd, ...history.future] },
  };
}

export function redo(
  doc: EditorDocument,
  history: History,
): { doc: EditorDocument; history: History } {
  const cmd = history.future[0];
  if (!cmd) return { doc, history };
  return {
    doc: apply(doc, cmd),
    history: { past: [...history.past, cmd], future: history.future.slice(1) },
  };
}

export const canUndo = (h: History) => h.past.length > 0;
export const canRedo = (h: History) => h.future.length > 0;

/** Keyboard shortcut of the history: Ctrl/Cmd+Z undo, Shift+Ctrl/Cmd+Z or Ctrl+Y redo. */
export function historyShortcut(e: {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}): 'undo' | 'redo' | null {
  const mod = e.ctrlKey || e.metaKey;
  if (!mod) return null;
  const key = e.key.toLowerCase();
  if (key === 'z') return e.shiftKey ? 'redo' : 'undo';
  if (key === 'y') return 'redo';
  return null;
}
