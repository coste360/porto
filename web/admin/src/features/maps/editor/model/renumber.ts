// Renumbering (spec 8.5, m1-plan §11 D9): client-side, previewed, one command, ids never change.
import type { Berth } from '@porto/shared/api';

import type { Command } from './commands';
import { occupiableCodes, type EditorDocument } from './document';

export type RenumberScheme = 'continuous' | 'odd_left_even_right' | 'per_side';

export type RenumberParams = { scheme: RenumberScheme; prefix: string; start: number; pad: number };

export type RenumberRow = { ref: string; from: string; to: string; conflict: boolean };

const SIDE_ORDER: Record<string, number> = { left: 0, right: 1, head: 2, none: 3 };
const SIDE_LETTER: Record<string, string> = { left: 'L', right: 'R', head: 'H', none: '' };

function format(n: number, p: RenumberParams, infix = ''): string {
  return `${p.prefix}${infix}${String(n).padStart(p.pad, '0')}`.slice(0, 32);
}

/** Selected berths in renumbering order: structure, side (left, right, head), position, code. */
export function orderedBerths(
  doc: EditorDocument,
  refs: ReadonlySet<string>,
): { ref: string; el: Berth }[] {
  const out: { ref: string; el: Berth }[] = [];
  for (const ref of refs) {
    const e = doc.elements.get(ref);
    if (e?.kind === 'berth') out.push({ ref, el: e.el });
  }
  return out.sort(
    (a, b) =>
      (a.el.structure_id ?? '').localeCompare(b.el.structure_id ?? '') ||
      (SIDE_ORDER[a.el.side] ?? 9) - (SIDE_ORDER[b.el.side] ?? 9) ||
      a.el.position - b.el.position ||
      a.el.code.localeCompare(b.el.code),
  );
}

/** Old → new codes; `conflict` marks a new code already used by an element outside the selection. */
export function previewRenumber(
  doc: EditorDocument,
  refs: ReadonlySet<string>,
  p: RenumberParams,
): RenumberRow[] {
  const berths = orderedBerths(doc, refs);
  const taken = occupiableCodes(doc, new Set(berths.map((b) => b.ref)));
  const rows: RenumberRow[] = [];
  const counters: Record<string, number> = {};
  let odd = p.start;
  let even = p.start + 1;
  let n = p.start;
  for (const b of berths) {
    let to: string;
    if (p.scheme === 'continuous') to = format(n++, p);
    else if (p.scheme === 'odd_left_even_right') {
      if (b.el.side === 'right') {
        to = format(even, p);
        even += 2;
      } else {
        to = format(odd, p);
        odd += 2;
      }
    } else {
      const c = counters[b.el.side] ?? p.start;
      counters[b.el.side] = c + 1;
      to = format(c, p, SIDE_LETTER[b.el.side] ?? '');
    }
    rows.push({ ref: b.ref, from: b.el.code, to, conflict: taken.has(to) });
  }
  const seen = new Set<string>();
  for (const r of rows) {
    if (seen.has(r.to)) r.conflict = true;
    seen.add(r.to);
  }
  return rows;
}

export function renumberCommand(doc: EditorDocument, rows: readonly RenumberRow[]): Command {
  const changes: Command['changes'] = [];
  for (const r of rows) {
    const before = doc.elements.get(r.ref);
    if (before?.kind !== 'berth' || before.el.code === r.to) continue;
    changes.push({
      ref: r.ref,
      before,
      after: { kind: 'berth', el: { ...before.el, code: r.to } },
    });
  }
  return { label: 'editor:commands.renumber', changes };
}
