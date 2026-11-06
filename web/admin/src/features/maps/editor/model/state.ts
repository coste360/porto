// Editor state: document, history, selection and the transient document of a drag in progress.
import { useReducer } from 'react';

import type { Command } from './commands';
import type { EditorDocument } from './document';
import { emptyHistory, execute, redo, undo, type History } from './history';
import { emptySelection, reconcile, type Selection } from './selection';

export type EditorState = {
  doc: EditorDocument | null;
  /** Shown instead of doc while a drag is in progress; the final command is committed on release. */
  transient: EditorDocument | null;
  history: History;
  selection: Selection;
  /** A local draft was restored: the document differs from the server even with an empty history. */
  restored: boolean;
};

export type EditorAction =
  | { type: 'load'; doc: EditorDocument; idMap?: Record<string, string>; restored?: boolean }
  | { type: 'execute'; command: Command; select?: Selection }
  | { type: 'undo' }
  | { type: 'redo' }
  | { type: 'select'; selection: Selection }
  | { type: 'transient'; doc: EditorDocument | null };

export const initialEditorState: EditorState = {
  doc: null,
  transient: null,
  history: emptyHistory,
  selection: emptySelection,
  restored: false,
};

export function editorReducer(state: EditorState, action: EditorAction): EditorState {
  switch (action.type) {
    case 'load':
      return {
        doc: action.doc,
        transient: null,
        history: emptyHistory,
        selection: reconcile(state.selection, action.doc, action.idMap),
        restored: action.restored ?? false,
      };
    case 'execute': {
      if (!state.doc) return state;
      const r = execute(state.doc, state.history, action.command);
      return {
        ...state,
        doc: r.doc,
        history: r.history,
        transient: null,
        selection: reconcile(action.select ?? state.selection, r.doc),
      };
    }
    case 'undo':
    case 'redo': {
      if (!state.doc) return state;
      const r =
        action.type === 'undo' ? undo(state.doc, state.history) : redo(state.doc, state.history);
      return {
        ...state,
        doc: r.doc,
        history: r.history,
        transient: null,
        selection: reconcile(state.selection, r.doc),
      };
    }
    case 'select':
      return { ...state, selection: action.selection };
    case 'transient':
      return { ...state, transient: action.doc };
  }
}

export function isDirty(state: EditorState): boolean {
  return state.restored || state.history.past.length > 0;
}

export function useEditorState() {
  return useReducer(editorReducer, initialEditorState);
}
