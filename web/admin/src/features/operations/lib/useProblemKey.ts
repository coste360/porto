import { toProblem } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';

/** i18n key of an API error: `errors:<slug>` when the slug has a message (M3 slugs included), else the generic one. */
export function useProblemKey() {
  const { i18n } = useT();
  return (error: unknown): string => {
    const p = toProblem(error);
    const key = `errors:${p.type}`;
    return i18n.exists(key) ? key : p.i18nKey;
  };
}
