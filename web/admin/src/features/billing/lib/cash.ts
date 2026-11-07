// Close-session wizard of the cash page (m5-plan §5.1, §9.1): the operator types the counted amount per method;
// the difference is counted − expected, per method and in total, integer cents only (as the Z summary).

export const COUNTED_METHODS = ['cash', 'card_pos', 'bank_transfer', 'cheque', 'other'] as const;
export type CountedMethod = (typeof COUNTED_METHODS)[number];

export type MethodAmount = { method: string; amount_cents: number };

/** Expected amount of a method (0 when the summary has no row for it). */
export function expectedOf(expected: readonly MethodAmount[], method: string): number {
  return expected.find((e) => e.method === method)?.amount_cents ?? 0;
}

/** Methods shown in the wizard: cash always, the others when something is expected. */
export function closeMethods(expected: readonly MethodAmount[]): CountedMethod[] {
  return COUNTED_METHODS.filter((m) => m === 'cash' || expectedOf(expected, m) !== 0);
}

export type Difference = {
  rows: { method: CountedMethod; expected: number; counted: number; difference: number }[];
  totalExpected: number;
  totalCounted: number;
  total: number;
};

/** Per-method and total difference; a method not typed yet counts as 0. */
export function sessionDifference(
  expected: readonly MethodAmount[],
  counted: Readonly<Partial<Record<CountedMethod, number | null>>>,
): Difference {
  const rows = closeMethods(expected).map((method) => {
    const e = expectedOf(expected, method);
    const c = counted[method] ?? 0;
    return { method, expected: e, counted: c, difference: c - e };
  });
  const totalExpected = rows.reduce((s, r) => s + r.expected, 0);
  const totalCounted = rows.reduce((s, r) => s + r.counted, 0);
  return { rows, totalExpected, totalCounted, total: totalCounted - totalExpected };
}

/** Body of closeCashSession: every shown method with its counted cents. */
export function countedBody(d: Difference): { method: CountedMethod; amount_cents: number }[] {
  return d.rows.map((r) => ({ method: r.method, amount_cents: r.counted }));
}
