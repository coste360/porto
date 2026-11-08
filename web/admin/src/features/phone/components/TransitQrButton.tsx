import { QrCodeIcon } from 'lucide-react';
import { useState } from 'react';

import type { Contract } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';

import { transitQrUrl } from '../api';

/**
 * "QR per l'ormeggiatore" of the desk transit detail (m6b-plan §6.4): the transit QR (payload PT1:<id>, no
 * personal data) to print or show; the dock worker scans it with the phone. Transits only.
 */
export function TransitQrButton({
  marinaId,
  transit,
}: {
  marinaId: string;
  transit: Pick<Contract, 'id' | 'kind' | 'code'>;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  if (transit.kind !== 'transit') return null;
  return (
    <>
      <Button variant="outline" onClick={() => setOpen(true)} data-testid="transit-qr-button">
        <QrCodeIcon />
        {t('phone:transitQr.button')}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent data-testid="transit-qr-dialog">
          <DialogHeader>
            <DialogTitle>{t('phone:transitQr.title', { code: transit.code })}</DialogTitle>
            <DialogDescription>{t('phone:transitQr.description')}</DialogDescription>
          </DialogHeader>
          {open ? (
            <img
              src={transitQrUrl(marinaId, transit.id)}
              alt={t('phone:transitQr.alt', { code: transit.code })}
              className="mx-auto aspect-square w-64 max-w-full rounded-md border p-2"
              data-testid="transit-qr-image"
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}
