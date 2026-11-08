import { useState } from 'react';

import { useEquipmentBookings } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';

import { useCurrentMarina } from '../../../auth/hooks';
import { useMarinaToday } from '../../registry/lib/today';
import { slotLabel } from '../lib/slots';
import { BookingDialog, type BookingDraft } from './BookingDialog';

/** Lifting bookings and requests of a work order, with "Prenota mezzo" (yard.manage or storage.manage). */
export function WorkOrderLiftingBookings({
  workOrderId,
  vesselId,
}: {
  workOrderId: string;
  vesselId?: string;
}) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const list = useEquipmentBookings(marinaId, undefined, { work_order_id: workOrderId });
  const write = can('yard.manage') || can('storage.manage');
  const [draft, setDraft] = useState<BookingDraft | null>(null);
  const rows = [...(list.data?.requests ?? []), ...(list.data?.bookings ?? [])];
  return (
    <section
      className="flex flex-col gap-2"
      aria-label={t('lifting:orderBookings')}
      data-testid="order-lifting"
    >
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">{t('lifting:orderBookings')}</h3>
        {write ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => setDraft({ workOrderId, vesselId, purpose: 'haul_out' })}
          >
            {t('lifting:actions.new')}
          </Button>
        ) : null}
      </div>
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('lifting:noOrderBookings')}</p>
      ) : (
        <ul className="flex flex-col gap-1 text-sm">
          {rows.map((b) => (
            <li key={b.id} className="flex items-center justify-between gap-2">
              <span>
                {t(`lifting:purposes.${b.purpose}`)}
                {b.date ? ` · ${formatIsoDate(b.date)}` : ''}
                {b.from_slot !== undefined && b.to_slot !== undefined
                  ? ` · ${slotLabel(b.from_slot)}–${slotLabel(b.to_slot)}`
                  : ''}
              </span>
              <Badge variant={b.status === 'cancelled' ? 'outline' : 'secondary'}>
                {t(`lifting:statuses.${b.status}`)}
              </Badge>
            </li>
          ))}
        </ul>
      )}
      {draft && list.data ? (
        <BookingDialog
          marinaId={marinaId}
          date={today}
          windowFrom={list.data.window_from_slot}
          windowTo={list.data.window_to_slot}
          equipment={list.data.equipment}
          draft={draft}
          onClose={() => setDraft(null)}
        />
      ) : null}
    </section>
  );
}
