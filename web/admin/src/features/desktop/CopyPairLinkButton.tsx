import { CopyIcon } from 'lucide-react';
import { toast } from 'sonner';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';

/**
 * "Copia collegamento" of a desktop pairing code (m6b-plan §7.2): copies the porto://pair payload, which the
 * desktop app pastes in "Collega a un Porto online". The payload carries the code: it goes to the clipboard only.
 */
export function CopyPairLinkButton({ payload }: { payload: string }) {
  const { t } = useT();
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      data-testid="copy-pair-link"
      data-pair-link={payload}
      onClick={() =>
        void navigator.clipboard
          ?.writeText(payload)
          .then(() => toast.success(t('desktop:remote.copied')))
      }
    >
      <CopyIcon />
      {t('desktop:remote.copyLink')}
    </Button>
  );
}
