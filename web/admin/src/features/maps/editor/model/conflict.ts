import type { MapDocument } from '@porto/shared/api';

import type { EditorDocument } from './document';

/** Berth codes only on one side of the conflict (the "changed codes" of the dialog). */
export function conflictDiff(local: EditorDocument, server: MapDocument) {
  const localCodes = new Set<string>();
  for (const e of local.elements.values()) if (e.kind === 'berth') localCodes.add(e.el.code);
  const serverCodes = new Set(server.berths.map((b) => b.code));
  return {
    onlyLocal: [...localCodes].filter((c) => !serverCodes.has(c)).sort(),
    onlyServer: [...serverCodes].filter((c) => !localCodes.has(c)).sort(),
  };
}
