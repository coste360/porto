import { useQuery } from '@tanstack/react-query';
import { SailboatIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  queryKeys,
  unwrap,
  useApi,
  useCreateRentalBooking,
  useRentalBookingQuote,
  useRentalSettings,
  useRentalUnits,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { useOnlineStatus } from '@porto/shared/offline';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { EmptyState, ProblemAlert } from '@porto/shared/ui/patterns';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { preselect, rangeText } from '../../rental-planner/lib/plan';
import { slotNowIn, walkInHours, walkInRange } from './lib/walkin';

/**
 * "Nuovo" tab of the rental phone (m11-plan §1.1, §14.3): a walk-in booking from now. Unit (available units), a
 * duration in whole hours within the open window, the formula among those that price the range (quoteRental),
 * the renter's name and phone, confirm: the booking is confirmed and the check-out wizard opens. Online only (a
 * booking takes slots, INV-18); a taken unit answers 409 rental_slot_taken.
 */
export function WalkInTab({
  marinaId,
  onCreated,
}: {
  marinaId: string;
  onCreated: (bookingId: string) => void;
}) {
  const { t, i18n } = useT();
  const api = useApi();
  const online = useOnlineStatus().online;
  const marina = useQuery({
    queryKey: queryKeys.marina(marinaId),
    queryFn: () =>
      unwrap(api.GET('/api/admin/v1/marinas/{marinaId}', { params: { path: { marinaId } } })),
    staleTime: 5 * 60_000,
  });
  const units = useRentalUnits(marinaId);
  const settings = useRentalSettings(marinaId);
  const create = useCreateRentalBooking(marinaId);
  const [unitId, setUnitId] = useState('');
  const [hours, setHours] = useState('');
  const [chosen, setChosen] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');

  const start = slotNowIn(marina.data?.timezone ?? 'Europe/Rome');
  const available = (units.data?.items ?? []).filter((u) => u.status === 'available');
  const choices = settings.data
    ? walkInHours(start, settings.data.open_from_slot, settings.data.open_to_slot)
    : [];
  const range = hours ? walkInRange(start, Number(hours)) : null;
  const quote = useRentalBookingQuote(
    marinaId,
    unitId && range ? { unit_id: unitId, start: range.start, end: range.end } : null,
  );
  const options = quote.data?.items ?? [];
  const formulaId = chosen ?? preselect(options) ?? null;

  if (settings.data && choices.length === 0) {
    return <EmptyState icon={<SailboatIcon />} title={t('rentalHandover:walkIn.closed')} />;
  }
  const submit = () => {
    if (!unitId || !range || !formulaId) return;
    create.mutate(
      {
        unit_id: unitId,
        formula_id: formulaId,
        start: range.start,
        end: range.end,
        hold: false,
        ...(name || phone
          ? { renter: { ...(name ? { name } : {}), ...(phone ? { phone } : {}) } }
          : {}),
      },
      {
        onSuccess: (b) => {
          toast.success(t('rentalBookings:toast.confirmed', { number: b.number }));
          onCreated(b.id);
        },
      },
    );
  };
  return (
    <div className="flex flex-col gap-4" data-testid="rental-walk-in">
      {!online ? (
        <Alert>
          <AlertDescription>{t('rentalHandover:walkIn.offline')}</AlertDescription>
        </Alert>
      ) : null}
      <Field>
        <FieldLabel htmlFor="walk-in-unit">{t('rentalBookings:fields.unit')}</FieldLabel>
        <Select
          value={unitId}
          onValueChange={(v) => {
            setUnitId(v);
            setChosen(null);
          }}
        >
          <SelectTrigger id="walk-in-unit" className="min-h-11">
            <SelectValue placeholder={t('rentalHandover:walkIn.pickUnit')} />
          </SelectTrigger>
          <SelectContent>
            {available.map((u) => (
              <SelectItem key={u.id} value={u.id}>
                {u.code} · {u.vessel_name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Field>
        <FieldLabel htmlFor="walk-in-hours">{t('rentalHandover:walkIn.duration')}</FieldLabel>
        <Select
          value={hours}
          onValueChange={(v) => {
            setHours(v);
            setChosen(null);
          }}
        >
          <SelectTrigger id="walk-in-hours" className="min-h-11">
            <SelectValue placeholder={t('rentalHandover:walkIn.pickDuration')} />
          </SelectTrigger>
          <SelectContent>
            {choices.map((h) => (
              <SelectItem key={h} value={String(h)}>
                {t('rentalHandover:walkIn.hours', { count: h })}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      {range ? <p className="text-muted-foreground text-sm">{rangeText(range)}</p> : null}
      {unitId && range ? (
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1 text-sm font-medium">{t('rentalBookings:dialog.formula')}</legend>
          {quote.isLoading ? (
            <p className="text-muted-foreground text-sm">{t('rentalBookings:dialog.quoting')}</p>
          ) : null}
          {quote.isSuccess && !options.some((o) => o.ok) ? (
            <Alert variant="destructive">
              <AlertDescription>{t('rentalBookings:dialog.noFormula')}</AlertDescription>
            </Alert>
          ) : null}
          <RadioGroup
            value={formulaId ?? ''}
            onValueChange={setChosen}
            aria-label={t('rentalBookings:dialog.formula')}
          >
            {options
              .filter((o) => o.ok)
              .map((o) => (
                <div key={o.formula_id} className="flex min-h-11 items-center gap-2">
                  <RadioGroupItem value={o.formula_id} id={`wf-${o.formula_id}`} />
                  <Label htmlFor={`wf-${o.formula_id}`}>
                    {o.formula_code} · {formatCents(o.total_cents, 'EUR', i18n.language)}
                  </Label>
                </div>
              ))}
          </RadioGroup>
        </fieldset>
      ) : null}
      <Field>
        <FieldLabel htmlFor="walk-in-name">{t('rentalBookings:fields.renterName')}</FieldLabel>
        <Input
          id="walk-in-name"
          value={name}
          maxLength={80}
          autoComplete="off"
          onChange={(e) => setName(e.target.value)}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="walk-in-phone">{t('rentalBookings:fields.renterPhone')}</FieldLabel>
        <Input
          id="walk-in-phone"
          type="tel"
          value={phone}
          maxLength={40}
          autoComplete="off"
          onChange={(e) => setPhone(e.target.value)}
        />
      </Field>
      <ProblemAlert error={create.error} />
      <Button
        className="min-h-11"
        data-testid="walk-in-confirm"
        disabled={!online || !unitId || !range || !formulaId || create.isPending}
        onClick={submit}
      >
        {t('rentalHandover:walkIn.confirm')}
      </Button>
    </div>
  );
}
