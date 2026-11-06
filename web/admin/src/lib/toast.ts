import type { TFunction } from 'i18next';
import { toast } from 'sonner';

import { toProblem } from '@porto/shared/api';

/** Shows an API error as a toast, through its `errors:<slug>` message. */
export function toastProblem(t: TFunction, error: unknown): void {
  toast.error(t(toProblem(error).i18nKey));
}
