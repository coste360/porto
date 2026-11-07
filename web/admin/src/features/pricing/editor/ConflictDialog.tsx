import type { PriceListDocument } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@porto/shared/ui/alert-dialog';
import { Button } from '@porto/shared/ui/button';

import type { EditorDoc } from './model';

/**
 * 412 on save: someone saved the list first. "Ricarica" discards the local changes, "Salva comunque" saves the
 * local document on top of the newer version.
 */
export function ConflictDialog({
  open,
  local,
  server,
  onReload,
  onOverwrite,
  onCancel,
}: {
  open: boolean;
  local: EditorDoc;
  server: PriceListDocument | null;
  onReload: () => void;
  onOverwrite: () => void;
  onCancel: () => void;
}) {
  const { t } = useT();
  return (
    <AlertDialog open={open} onOpenChange={(o) => (o ? null : onCancel())}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('pricing:editor.conflict.title')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('pricing:editor.conflict.description')}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {server ? (
          <div className="flex flex-col gap-1 text-sm" data-testid="conflict-details">
            <p>
              {t('pricing:editor.conflict.server', {
                version: server.version,
                rules: server.rules.length,
                periods: server.periods.length,
                bands: server.bands.length,
              })}
            </p>
            <p>
              {t('pricing:editor.conflict.local', {
                rules: local.rules.length,
                periods: local.periods.length,
                bands: local.bands.length,
              })}
            </p>
            {server.status !== 'draft' ? (
              <p className="text-destructive">{t('pricing:editor.conflict.notDraft')}</p>
            ) : null}
          </div>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common:actions.cancel')}</AlertDialogCancel>
          <Button variant="outline" onClick={onReload}>
            {t('pricing:editor.conflict.reload')}
          </Button>
          <Button
            variant="destructive"
            onClick={onOverwrite}
            disabled={!server || server.status !== 'draft'}
          >
            {t('pricing:editor.conflict.overwrite')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
