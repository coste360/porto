/** The common value of a field over a multi-selection, or MIXED when the values differ. */
export const MIXED = Symbol('mixed');
export type Common<T> = T | typeof MIXED | undefined;

export function common<T>(values: readonly T[]): Common<T> {
  if (values.length === 0) return undefined;
  const first = values[0];
  return values.every((v) => v === first) ? first : MIXED;
}
