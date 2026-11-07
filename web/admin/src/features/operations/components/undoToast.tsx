import type { TFunction } from 'i18next';
import { toast } from 'sonner';

import { Button } from '@porto/shared/ui/button';

/** Visible time of the undo toast; the undo stays available in the quick card for 24 h (m3-plan §8.5). */
export const UNDO_TOAST_MS = 30_000;

/** Toast after a move, swap or layout save with the "Annulla" action (test id ops-undo). */
export function showUndoToast(t: TFunction, message: string, onUndo: () => void): void {
  toast.custom(
    (id) => (
      <div
        className="bg-popover text-popover-foreground flex items-center gap-3 rounded-md border p-3 shadow-md"
        role="status"
      >
        <span className="text-sm">{message}</span>
        <Button
          size="sm"
          variant="outline"
          data-testid="ops-undo"
          onClick={() => {
            toast.dismiss(id);
            onUndo();
          }}
        >
          {t('opsMap:undo.action')}
        </Button>
      </div>
    ),
    { duration: UNDO_TOAST_MS },
  );
}
