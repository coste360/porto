import { SecretReveal } from '@porto/shared/ui/patterns';
import { useT } from '@porto/shared/i18n';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';

import { hmacExample } from '../lib';

/**
 * The key of a device or gateway, shown once (m12-plan §3.3): copy button and the signing example. The parent
 * drops the key from its state on close, so it can never be shown again.
 */
export function KeyRevealDialog({
  secret,
  code,
  onClose,
}: {
  secret: string;
  code: string;
  onClose: () => void;
}) {
  const { t } = useT();
  const base = typeof window === 'undefined' ? '' : window.location.origin;
  return (
    <Dialog open onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent className="sm:max-w-2xl" data-testid="key-reveal">
        <DialogHeader>
          <DialogTitle>{t('sensors:key.title', { code })}</DialogTitle>
          <DialogDescription>{t('sensors:key.hint')}</DialogDescription>
        </DialogHeader>
        <SecretReveal secret={secret} label={t('sensors:key.label')} onDone={onClose} />
        {secret.startsWith('po_sns_') || secret.startsWith('po_gw_') ? (
          <details className="text-sm">
            <summary className="cursor-pointer">{t('sensors:key.example')}</summary>
            <pre
              className="bg-muted mt-2 overflow-x-auto rounded-md p-3 text-xs"
              data-testid="key-example"
            >
              {hmacExample(secret, base)}
            </pre>
          </details>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
