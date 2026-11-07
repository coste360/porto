// Matching table of the record-payment dialog (m5-plan §4.2, §9.1): integer cents only. The auto-fill spreads the
// amount over the open charges oldest due date first; a row never takes more than its residual and the rows
// never take more than the payment; what is left becomes customer credit, shown live. The server enforces the
// same limits (INV-52): this is the preview the operator edits.

export type MatchRow = {
  chargeId: string;
  number: string;
  dueOn: string;
  residualCents: number;
};

export type Allocation = Readonly<Record<string, number>>;

/** Rows sorted oldest due date first, then by number (the order of `auto_match: oldest_due_first`). */
export function oldestFirst<R extends Pick<MatchRow, 'dueOn' | 'number'>>(rows: readonly R[]): R[] {
  return [...rows].sort((a, b) =>
    a.dueOn === b.dueOn ? a.number.localeCompare(b.number) : a.dueOn < b.dueOn ? -1 : 1,
  );
}

/** Fills the rows oldest first with the payment amount. */
export function autoFill(rows: readonly MatchRow[], amountCents: number): Record<string, number> {
  const out: Record<string, number> = {};
  let left = Math.max(0, amountCents);
  for (const r of oldestFirst(rows)) {
    const take = Math.min(left, Math.max(0, r.residualCents));
    out[r.chargeId] = take;
    left -= take;
  }
  return out;
}

/** Sum of the allocated cents. */
export function allocatedTotal(alloc: Allocation): number {
  let s = 0;
  for (const v of Object.values(alloc)) s += v;
  return s;
}

/**
 * Sets one row, clamped to [0, residual] and to what the other rows leave of the payment, so the table can never
 * exceed the residual of a charge or the amount of the payment.
 */
export function setRow(
  rows: readonly MatchRow[],
  alloc: Allocation,
  amountCents: number,
  chargeId: string,
  cents: number,
): Record<string, number> {
  const row = rows.find((r) => r.chargeId === chargeId);
  if (!row) return { ...alloc };
  const others = allocatedTotal(alloc) - (alloc[chargeId] ?? 0);
  const room = Math.max(0, amountCents - others);
  const clamped = Math.max(0, Math.min(cents, row.residualCents, room));
  return { ...alloc, [chargeId]: clamped };
}

/** Rows above the residual or a total above the payment (after the amount was lowered). */
export function isValidAllocation(
  rows: readonly MatchRow[],
  alloc: Allocation,
  amountCents: number,
): boolean {
  for (const r of rows) {
    const v = alloc[r.chargeId] ?? 0;
    if (v < 0 || v > r.residualCents) return false;
  }
  return allocatedTotal(alloc) <= amountCents;
}

/** What remains unallocated and becomes customer credit. */
export function creditPreview(alloc: Allocation, amountCents: number): number {
  return Math.max(0, amountCents - allocatedTotal(alloc));
}

/** The `matches` body of RecordPayment: only rows with cents, in table order. */
export function toMatches(
  rows: readonly MatchRow[],
  alloc: Allocation,
): { charge_id: string; amount_cents: number }[] {
  return oldestFirst(rows)
    .filter((r) => (alloc[r.chargeId] ?? 0) > 0)
    .map((r) => ({ charge_id: r.chargeId, amount_cents: alloc[r.chargeId]! }));
}
