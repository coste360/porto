import type { EquipmentBookingDay, EquipmentBooking } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';

import { lanes, slotLabel, windowSlots } from '../lib/slots';

/**
 * Day calendar of the lifting machines (m10-plan §14.3): rows = machines, columns = 15-minute slots of the lift
 * window, a booking spans its slots. Free slots are buttons when `onFree` is given (office); bookings are buttons
 * when `onBooking` is given. Read only on the phone.
 */
export function DayGrid({
  day,
  onFree,
  onBooking,
}: {
  day: EquipmentBookingDay;
  onFree?: (equipmentId: string, slot: number) => void;
  onBooking?: (b: EquipmentBooking) => void;
}) {
  const { t } = useT();
  const slots = windowSlots(day.window_from_slot, day.window_to_slot);
  const byEq = lanes(day.bookings);
  if (day.equipment.length === 0) {
    return <p className="text-muted-foreground text-sm">{t('lifting:noEquipment')}</p>;
  }
  return (
    <div className="overflow-x-auto rounded-md border" data-testid="lifting-grid">
      <table className="border-collapse text-xs">
        <thead>
          <tr>
            <th className="bg-muted sticky left-0 p-2 text-left font-medium">
              {t('lifting:fields.equipment')}
            </th>
            {slots.map((s) => (
              <th key={s} className="bg-muted min-w-10 p-1 text-left font-normal">
                {s % 4 === 0 ? slotLabel(s) : ''}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {day.equipment.map((e) => {
            const lane = byEq.get(e.id);
            const cells = [];
            for (let i = 0; i < slots.length; i++) {
              const s = slots[i]!;
              const b = lane?.bySlot.get(s);
              if (b) {
                const end = Math.min(b.to_slot ?? s + 1, day.window_to_slot);
                const span = Math.max(end - s, 1);
                cells.push(
                  <td key={s} colSpan={span} className="p-0.5">
                    <button
                      type="button"
                      data-testid="lifting-booking"
                      disabled={!onBooking}
                      onClick={() => onBooking?.(b)}
                      className={`w-full truncate rounded px-1 py-1 text-left ${b.status === 'done' ? 'bg-muted text-muted-foreground' : 'bg-primary text-primary-foreground'}`}
                    >
                      {slotLabel(b.from_slot ?? s)}{' '}
                      {b.vessel_name ?? t(`lifting:purposes.${b.purpose}`)}
                    </button>
                  </td>,
                );
                i += span - 1;
                continue;
              }
              cells.push(
                <td key={s} className="border-l p-0.5">
                  {onFree && !e.out_of_service ? (
                    <button
                      type="button"
                      data-testid={`lifting-free-${e.code}-${slotLabel(s)}`}
                      aria-label={t('lifting:bookAt', { equipment: e.code, time: slotLabel(s) })}
                      onClick={() => onFree(e.id, s)}
                      className="hover:bg-muted block h-6 w-full rounded"
                    />
                  ) : (
                    <span className="block h-6" />
                  )}
                </td>,
              );
            }
            return (
              <tr key={e.id} className="border-t">
                <th
                  scope="row"
                  className="bg-card sticky left-0 min-w-28 p-2 text-left font-medium"
                >
                  <div>{e.code}</div>
                  <div className="text-muted-foreground font-normal">{e.name}</div>
                  {e.out_of_service ? (
                    <Badge variant="destructive">{t('lifting:outOfService')}</Badge>
                  ) : null}
                </th>
                {cells}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
