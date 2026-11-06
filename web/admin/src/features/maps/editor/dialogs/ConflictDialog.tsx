import type { MapDocument } from '@porto/shared/api';
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

import { conflictDiff } from '../model/conflict';
import { countsOf, type EditorDocument } from '../model/document';

/**
 * 412 on save (spec 8.5): someone saved first. Shows server vs local counts and the changed codes;
 * "Ricarica" discards the local changes, "Salva comunque" re-applies the local document on the new version.
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
  local: EditorDocument | null;
  server: MapDocument | null;
  onReload: () => void;
  onOverwrite: () => void;
  onCancel: () => void;
}) {
  const { t } = useT();
  const lc = local ? countsOf(local) : null;
  const diff = local && server ? conflictDiff(local, server) : null;
  return (
    <AlertDialog open={open} onOpenChange={(o) => (o ? null : onCancel())}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('editor:conflict.title')}</AlertDialogTitle>
          <AlertDialogDescription>{t('editor:conflict.description')}</AlertDialogDescription>
        </AlertDialogHeader>
        {server && lc && diff ? (
          <div className="flex flex-col gap-2 text-sm" data-testid="conflict-details">
            <p>
              {t('editor:conflict.server', {
                version: server.version,
                berths: server.berths.length,
                structures: server.structures.length,
              })}
            </p>
            <p>{t('editor:conflict.local', { berths: lc.berths, structures: lc.structures })}</p>
            {diff.onlyLocal.length > 0 ? (
              <p>
                {t('editor:conflict.onlyLocal', { codes: diff.onlyLocal.slice(0, 20).join(', ') })}
              </p>
            ) : null}
            {diff.onlyServer.length > 0 ? (
              <p>
                {t('editor:conflict.onlyServer', {
                  codes: diff.onlyServer.slice(0, 20).join(', '),
                })}
              </p>
            ) : null}
          </div>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common:actions.cancel')}</AlertDialogCancel>
          <Button variant="outline" onClick={onReload}>
            {t('editor:conflict.reload')}
          </Button>
          <Button variant="destructive" onClick={onOverwrite} disabled={!server}>
            {t('editor:conflict.overwrite')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
