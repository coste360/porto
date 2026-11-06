import { useT } from '@porto/shared/i18n';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@porto/shared/ui/alert-dialog';
import { buttonVariants } from '@porto/shared/ui/button';

/** Router blocker prompt: leaving the editor with unsaved changes. */
export function UnsavedChangesDialog({
  open,
  onStay,
  onLeave,
}: {
  open: boolean;
  onStay: () => void;
  onLeave: () => void;
}) {
  const { t } = useT();
  return (
    <AlertDialog open={open} onOpenChange={(o) => (o ? null : onStay())}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('editor:unsaved.title')}</AlertDialogTitle>
          <AlertDialogDescription>{t('editor:unsaved.description')}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onStay}>{t('editor:unsaved.stay')}</AlertDialogCancel>
          <AlertDialogAction
            className={buttonVariants({ variant: 'destructive' })}
            onClick={onLeave}
          >
            {t('editor:unsaved.leave')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** A local draft exists for this map version: restore it or discard it. */
export function DraftRestoreDialog({
  open,
  savedAt,
  onRestore,
  onDiscard,
}: {
  open: boolean;
  savedAt: string;
  onRestore: () => void;
  onDiscard: () => void;
}) {
  const { t, i18n } = useT();
  return (
    <AlertDialog open={open} onOpenChange={(o) => (o ? null : onDiscard())}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('editor:draft.title')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('editor:draft.description', {
              when: new Date(savedAt).toLocaleString(i18n.language),
            })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onDiscard}>{t('editor:draft.discard')}</AlertDialogCancel>
          <AlertDialogAction onClick={onRestore}>{t('editor:draft.restore')}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
