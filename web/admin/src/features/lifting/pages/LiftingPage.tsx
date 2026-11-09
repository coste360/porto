import { CalendarClockIcon } from 'lucide-react';
import { useState } from 'react';

import { useEquipmentBookings, type EquipmentBooking } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { YardPlannerNav } from '../../yard-planner/components/kit';
import {
  BookingActionsDialog,
  BookingDialog,
  type BookingDraft,
} from '../components/BookingDialog';
import { DayGrid } from '../components/DayGrid';

/**
 * Lifting machines (m10-plan §11, §14.3): the day calendar of the bookable machines in 15-minute slots, the tray of
 * requests to plan, the booking dialog; a taken slot comes back as 409 with the taken slots.
 */
export function LiftingPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [date, setDate] = useState<string | null>(null);
  const d = date ?? today;
  const day = useEquipmentBookings(marinaId, date ?? undefined);
  const write = can('yard.manage') || can('storage.manage');
  const [draft, setDraft] = useState<BookingDraft | null>(null);
  const [actions, setActions] = useState<EquipmentBooking | null>(null);
  return (
    <RequirePermission allowed={can('yard.read') || can('storage.read') || write}>
      <div className="flex w-full min-w-0 flex-col gap-4">
        <PageHeader
          title={t('lifting:title')}
          description={t('lifting:description')}
          actions={
            write ? (
              <Button onClick={() => setDraft({})}>
                <CalendarClockIcon />
                {t('lifting:actions.new')}
              </Button>
            ) : null
          }
        />
        <YardPlannerNav marinaId={marinaId} current="lifting" />
        <Field className="w-48">
          <FieldLabel htmlFor="lifting-date">{t('lifting:fields.date')}</FieldLabel>
          <Input
            id="lifting-date"
            type="date"
            value={d}
            onChange={(e) => setDate(e.target.value || null)}
          />
        </Field>
        {day.isPending ? (
          <LoadingState rows={3} />
        ) : day.isError ? (
          <QueryErrorState error={day.error} onRetry={() => void day.refetch()} />
        ) : (
          <>
            <DayGrid
              day={day.data}
              onFree={
                write ? (equipmentId, slot) => setDraft({ equipmentId, fromSlot: slot }) : undefined
              }
              onBooking={write ? setActions : undefined}
            />
            <section className="flex flex-col gap-2" aria-label={t('lifting:requests')}>
              <h2 className="text-sm font-semibold">{t('lifting:requests')}</h2>
              {day.data.requests.length === 0 ? (
                <p className="text-muted-foreground text-sm">{t('lifting:noRequests')}</p>
              ) : (
                <ul className="flex flex-col gap-2" data-testid="lifting-requests">
                  {day.data.requests.map((r) => (
                    <li
                      key={r.id}
                      className="flex items-center justify-between gap-2 rounded-md border p-2 text-sm"
                    >
                      <span>
                        {t(`lifting:purposes.${r.purpose}`)}
                        {r.vessel_name ? ` · ${r.vessel_name}` : ''}
                        {r.date ? ` · ${formatIsoDate(r.date)}` : ''}
                        {` · ${t(`lifting:sources.${r.source}`)}`}
                      </span>
                      {write ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setDraft({ booking: r })}
                        >
                          {t('lifting:actions.plan')}
                        </Button>
                      ) : null}
                    </li>
                  ))}
                </ul>
              )}
            </section>
            {draft ? (
              <BookingDialog
                marinaId={marinaId}
                date={d}
                windowFrom={day.data.window_from_slot}
                windowTo={day.data.window_to_slot}
                equipment={day.data.equipment}
                draft={draft}
                onClose={() => setDraft(null)}
              />
            ) : null}
            {actions ? (
              <BookingActionsDialog
                marinaId={marinaId}
                booking={actions}
                onReschedule={() => {
                  setDraft({ booking: actions });
                  setActions(null);
                }}
                onClose={() => setActions(null)}
              />
            ) : null}
          </>
        )}
      </div>
    </RequirePermission>
  );
}
