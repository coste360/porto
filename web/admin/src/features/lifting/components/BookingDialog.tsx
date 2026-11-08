import { useState } from 'react';
import { toast } from 'sonner';

import {
  useCancelEquipmentBooking,
  useCompleteEquipmentBooking,
  useCreateEquipmentBooking,
  useEmployees,
  useEquipmentOperators,
  useUpdateEquipmentBooking,
  useVessels,
  type DayEquipment,
  type EquipmentBooking,
  type EquipmentBookingPurpose,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { EnumSelect } from '../../contracts-m4/components/kit';
import { conflictText } from '../lib/conflict';
import { slotLabel, slotsFor, windowSlots } from '../lib/slots';

const PURPOSES: readonly EquipmentBookingPurpose[] = ['haul_out', 'launch', 'move', 'other'];
const NONE = '__none__';

export type BookingDraft = {
  /** Existing booking (book a request or reschedule), absent for a new one. */
  booking?: EquipmentBooking;
  equipmentId?: string;
  fromSlot?: number;
  workOrderId?: string;
  vesselId?: string;
  purpose?: EquipmentBookingPurpose;
};

/**
 * Booking dialog of the lifting calendar (m10-plan §11, §14.3): machine, purpose, vessel, operator, start and
 * duration; a request (no machine yet) or a booking. The server decides conflicts (409 with the taken slots).
 */
export function BookingDialog({
  marinaId,
  date,
  windowFrom,
  windowTo,
  equipment,
  draft,
  onClose,
}: {
  marinaId: string;
  date: string;
  windowFrom: number;
  windowTo: number;
  equipment: readonly DayEquipment[];
  draft: BookingDraft;
  onClose: () => void;
}) {
  const { t } = useT();
  const b = draft.booking;
  const create = useCreateEquipmentBooking(marinaId);
  const update = useUpdateEquipmentBooking(marinaId);
  const [equipmentId, setEquipmentId] = useState(
    b?.equipment_id ?? draft.equipmentId ?? equipment[0]?.id ?? '',
  );
  const eq = equipment.find((e) => e.id === equipmentId);
  const [purpose, setPurpose] = useState<EquipmentBookingPurpose | ''>(
    b?.purpose ?? draft.purpose ?? 'haul_out',
  );
  const [vesselQuery, setVesselQuery] = useState('');
  const vessels = useVessels(marinaId, vesselQuery.trim() ? { q: vesselQuery.trim() } : {});
  const [vesselId, setVesselId] = useState(b?.vessel_id ?? draft.vesselId ?? '');
  const operators = useEquipmentOperators(marinaId, equipmentId || undefined);
  const employees = useEmployees(marinaId, { active: true });
  const [operator, setOperator] = useState(b?.operator_employee_id ?? '');
  const [from, setFrom] = useState(b?.from_slot ?? draft.fromSlot ?? windowFrom);
  const [minutes, setMinutes] = useState(
    String(
      b?.from_slot !== undefined && b?.to_slot !== undefined
        ? (b.to_slot - b.from_slot) * 15
        : (eq?.default_minutes ?? 30),
    ),
  );
  const [day, setDay] = useState(b?.date ?? date);
  const [note, setNote] = useState(b?.note ?? '');
  const to = from + slotsFor(Number(minutes) || 15);
  const vesselList = vessels.data?.pages.flatMap((p) => p.items) ?? [];
  const opIds = operators.data?.employee_ids ?? [];
  const empCode = new Map(
    (employees.data?.items ?? []).map((e) => [e.id, `${e.code} ${e.first_name} ${e.last_name}`]),
  );
  const error = create.error ?? update.error;
  // A new booking needs a vessel (or the order's) unless its purpose is "other" (the server rule).
  const needsVessel = !b && purpose !== 'other' && !vesselId && !draft.workOrderId;
  const conflict = conflictText(t, error);
  const save = () => {
    const done = () => {
      toast.success(t('lifting:booked'));
      onClose();
    };
    if (b) {
      update.mutate(
        {
          id: b.id,
          version: b.version,
          body: {
            equipment_id: equipmentId,
            date: day,
            from_slot: from,
            to_slot: to,
            operator_employee_id: operator || null,
            note,
          },
        },
        { onSuccess: done },
      );
      return;
    }
    create.mutate(
      {
        equipment_id: equipmentId,
        date: day,
        from_slot: from,
        to_slot: to,
        purpose: purpose || 'other',
        ...(vesselId ? { vessel_id: vesselId } : {}),
        ...(draft.workOrderId ? { work_order_id: draft.workOrderId } : {}),
        ...(operator ? { operator_employee_id: operator } : {}),
        ...(note ? { note } : {}),
      },
      { onSuccess: done },
    );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent data-testid="booking-dialog">
        <DialogHeader>
          <DialogTitle>{b ? t('lifting:dialog.edit') : t('lifting:dialog.new')}</DialogTitle>
          <DialogDescription>{t('lifting:dialog.hint')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="lift-equipment">{t('lifting:fields.equipment')}</FieldLabel>
            <Select value={equipmentId} onValueChange={setEquipmentId}>
              <SelectTrigger id="lift-equipment">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {equipment.map((e) => (
                  <SelectItem key={e.id} value={e.id}>
                    {e.code} · {e.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="lift-date">{t('lifting:fields.date')}</FieldLabel>
            <Input
              id="lift-date"
              type="date"
              value={day}
              onChange={(e) => setDay(e.target.value)}
            />
          </Field>
          {!b ? (
            <EnumSelect
              id="lift-purpose"
              label={t('lifting:fields.purpose')}
              values={PURPOSES}
              value={purpose}
              onChange={setPurpose}
              labelOf={(p) => t(`lifting:purposes.${p}`)}
            />
          ) : null}
          {!b ? (
            <Field>
              <FieldLabel htmlFor="lift-vessel-q">{t('lifting:fields.vessel')}</FieldLabel>
              <Input
                id="lift-vessel-q"
                placeholder={t('lifting:dialog.vesselSearch')}
                value={vesselQuery}
                onChange={(e) => setVesselQuery(e.target.value)}
              />
              <Select
                value={vesselId || NONE}
                onValueChange={(v) => setVesselId(v === NONE ? '' : v)}
              >
                <SelectTrigger id="lift-vessel" aria-label={t('lifting:fields.vessel')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>{t('lifting:dialog.noVessel')}</SelectItem>
                  {vesselList.map((v) => (
                    <SelectItem key={v.id} value={v.id}>
                      {v.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          <Field>
            <FieldLabel htmlFor="lift-from">{t('lifting:fields.from')}</FieldLabel>
            <Select value={String(from)} onValueChange={(v) => setFrom(Number(v))}>
              <SelectTrigger id="lift-from">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {windowSlots(windowFrom, windowTo).map((s) => (
                  <SelectItem key={s} value={String(s)}>
                    {slotLabel(s)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="lift-minutes">{t('lifting:fields.minutes')}</FieldLabel>
            <Input
              id="lift-minutes"
              inputMode="numeric"
              value={minutes}
              onChange={(e) => setMinutes(e.target.value.replace(/\D/g, ''))}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="lift-operator">{t('lifting:fields.operator')}</FieldLabel>
            <Select
              value={operator || NONE}
              onValueChange={(v) => setOperator(v === NONE ? '' : v)}
            >
              <SelectTrigger id="lift-operator">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>{t('lifting:dialog.noOperator')}</SelectItem>
                {opIds.map((id) => (
                  <SelectItem key={id} value={id}>
                    {empCode.get(id) ?? id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="lift-note">{t('lifting:fields.note')}</FieldLabel>
            <Input
              id="lift-note"
              value={note}
              maxLength={500}
              onChange={(e) => setNote(e.target.value)}
            />
          </Field>
        </div>
        <p className="text-muted-foreground text-sm">
          {t('lifting:dialog.window', { from: slotLabel(from), to: slotLabel(to) })}
        </p>
        {conflict ? (
          <Alert variant="destructive" data-testid="booking-conflict">
            <AlertDescription>{conflict}</AlertDescription>
          </Alert>
        ) : (
          <ProblemAlert error={error} />
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('lifting:actions.cancel')}
          </Button>
          <Button
            onClick={save}
            disabled={!equipmentId || !day || needsVessel || create.isPending || update.isPending}
          >
            {b ? t('lifting:actions.save') : t('lifting:actions.book')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Actions on an existing booking: complete, cancel, reschedule. */
export function BookingActionsDialog({
  marinaId,
  booking,
  onReschedule,
  onClose,
}: {
  marinaId: string;
  booking: EquipmentBooking;
  onReschedule: () => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const complete = useCompleteEquipmentBooking(marinaId);
  const cancel = useCancelEquipmentBooking(marinaId);
  const live = booking.status === 'booked' || booking.status === 'requested';
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent data-testid="booking-actions">
        <DialogHeader>
          <DialogTitle>{t(`lifting:purposes.${booking.purpose}`)}</DialogTitle>
          <DialogDescription>
            {booking.vessel_name ?? ''}
            {booking.from_slot !== undefined && booking.to_slot !== undefined
              ? ` · ${slotLabel(booking.from_slot)}–${slotLabel(booking.to_slot)}`
              : ''}
            {` · ${t(`lifting:statuses.${booking.status}`)}`}
          </DialogDescription>
        </DialogHeader>
        {booking.note ? <p className="text-sm">{booking.note}</p> : null}
        <ProblemAlert error={complete.error ?? cancel.error} />
        <DialogFooter className="flex-wrap gap-2">
          {live ? (
            <Button
              variant="outline"
              onClick={() =>
                cancel.mutate({ id: booking.id, version: booking.version }, { onSuccess: onClose })
              }
            >
              {t('lifting:actions.cancelBooking')}
            </Button>
          ) : null}
          {live ? (
            <Button variant="outline" onClick={onReschedule}>
              {booking.status === 'requested'
                ? t('lifting:actions.plan')
                : t('lifting:actions.reschedule')}
            </Button>
          ) : null}
          {booking.status === 'booked' ? (
            <Button
              onClick={() =>
                complete.mutate(
                  { id: booking.id, version: booking.version },
                  { onSuccess: onClose },
                )
              }
            >
              {t('lifting:actions.complete')}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
