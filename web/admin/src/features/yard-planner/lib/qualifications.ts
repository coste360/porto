import { useQualificationOptions } from '@porto/shared/api';

/** Qualification id -> code (and the lift-operator flag) for labels. */
export function useQualificationIndex(marinaId: string) {
  const q = useQualificationOptions(marinaId);
  const byId = new Map<string, { code: string; liftOperator: boolean }>();
  for (const x of q.data?.items ?? [])
    byId.set(x.id, { code: x.code, liftOperator: x.lift_operator });
  return { list: q.data?.items ?? [], byId };
}
