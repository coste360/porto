import { PrinterIcon } from 'lucide-react';

import { usePrintMap } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';

import { useCurrentMarina } from '../../../auth/hooks';
import { printLens } from '../lib/print';
import {} from '../../contracts-m4/components/kit';
import { openBlob } from '../../contracts-m4/lib/files';

/** "Stampa" of the operational map (M3 D25): PDF of the map for the night, opened in a new tab. */
export function PrintMapButton({
  mapId,
  date,
  lens,
}: {
  mapId: string;
  date: string;
  lens?: string;
}) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const print = usePrintMap(marinaId);
  if (!can('map.view') || !mapId) return null;
  const printed = printLens(lens);
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={print.isPending}
      title={print.isError ? t('templates:print.failed') : undefined}
      onClick={() =>
        print.mutate(
          { mapId, date, ...(printed ? { lens: printed } : {}) },
          { onSuccess: openBlob },
        )
      }
    >
      <PrinterIcon />
      {t('templates:print.button')}
    </Button>
  );
}
