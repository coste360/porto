import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  toProblem,
  useCreateRentalBooking,
  useCustomers,
  useRentalBookingQuote,
  useRentalExtras,
  useRentalFormulas,
  useRentalSkippers,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import type { PlannerBand, SlotRange } from '@porto/shared/planner';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
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
import { Label } from '@porto/shared/ui/label';
import { ProblemAlert } from '@porto/shared/ui/patterns';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { preselect, rangeText, suggestFormula, suggestKind } from '../lib/plan';

const NONE = '__none__';

export type BookingDraft = {
  unitId: string;
  unitCode: string;
  category?: string;
  range: SlotRange;
};

/**
 * Booking dialog of the rental planner (m11-plan §14.2): opened by releasing a drag on the planner. The formula is
 * suggested by the range among the quoteRental alternatives; renter, skipper, extras and invoice request; "Opzione"
 * holds the slots for desk_hold_minutes, "Conferma" confirms. A 409 names the bookings in the way (highlighted on
 * the planner by onConflict).
 */
export function BookingDialog({
  marinaId,
  draft,
  openFrom,
  openTo,
  bands,
  onClose,
  onConflict,
}: {
  marinaId: string;
  draft: BookingDraft;
  openFrom: number;
  openTo: number;
  bands: readonly PlannerBand[];
  onClose: (bookingId?: string) => void;
  onConflict: (ids: string[]) => void;
}) {
  const { t, i18n } = useT();
  const formulas = useRentalFormulas(marinaId);
  const skippers = useRentalSkippers(marinaId);
  const extrasQ = useRentalExtras(marinaId);
  const create = useCreateRentalBooking(marinaId);
  const [skipperId, setSkipperId] = useState(NONE);
  const [extras, setExtras] = useState<string[]>([]);
  const [chosen, setChosen] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [email, setEmail] = useState('');
  const [customerQ, setCustomerQ] = useState('');
  const [customerId, setCustomerId] = useState(NONE);
  const [invoice, setInvoice] = useState(false);
  const customers = useCustomers(
    marinaId,
    customerQ.trim().length >= 2 ? { q: customerQ.trim() } : {},
  );
  const suggested = useMemo(
    () =>
      suggestFormula(
        formulas.data?.items ?? [],
        draft.category,
        suggestKind(draft.range, openFrom, openTo, bands),
      )?.id,
    [formulas.data, draft, openFrom, openTo, bands],
  );
  const quote = useRentalBookingQuote(marinaId, {
    unit_id: draft.unitId,
    start: draft.range.start,
    end: draft.range.end,
    ...(skipperId !== NONE ? { skipper_id: skipperId, with_skipper: true } : {}),
    ...(extras.length > 0
      ? { extras: extras.map((id) => ({ extra_id: id, qty_milli: 1000 })) }
      : {}),
  });
  const options = quote.data?.items ?? [];
  const formulaId = chosen ?? preselect(options, suggested) ?? null;
  const conflict = create.error ? toProblem(create.error) : null;
  const conflictIds =
    conflict && (conflict.type === 'rental_slot_taken' || conflict.type === 'skipper_busy')
      ? ((conflict.details.booking_ids as string[] | undefined) ?? [])
      : [];

  const submit = (hold: boolean) => {
    if (!formulaId) return;
    create.mutate(
      {
        unit_id: draft.unitId,
        formula_id: formulaId,
        start: draft.range.start,
        end: draft.range.end,
        hold,
        ...(skipperId !== NONE ? { skipper_id: skipperId } : {}),
        ...(customerId !== NONE ? { customer_id: customerId, invoice_requested: invoice } : {}),
        ...(name || phone || email
          ? {
              renter: {
                ...(name ? { name } : {}),
                ...(phone ? { phone } : {}),
                ...(email ? { email } : {}),
              },
            }
          : {}),
        ...(extras.length > 0
          ? { extras: extras.map((id) => ({ extra_id: id, qty_milli: 1000 })) }
          : {}),
      },
      {
        onSuccess: (b) => {
          toast.success(
            t(hold ? 'rentalBookings:toast.held' : 'rentalBookings:toast.confirmed', {
              number: b.number,
            }),
          );
          onClose(b.id);
        },
        onError: (err) => {
          const p = toProblem(err);
          if (p.type === 'rental_slot_taken' || p.type === 'skipper_busy') {
            onConflict((p.details.booking_ids as string[] | undefined) ?? []);
          }
        },
      },
    );
  };

  const activeExtras = (extrasQ.data?.items ?? []).filter(
    (x) =>
      x.active &&
      (x.categories.length === 0 ||
        (draft.category && x.categories.includes(draft.category as never))),
  );
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        className="max-h-screen overflow-y-auto sm:max-w-xl"
        data-testid="rental-booking-dialog"
      >
        <DialogHeader>
          <DialogTitle>{t('rentalBookings:dialog.title')}</DialogTitle>
          <DialogDescription>
            {t('rentalBookings:dialog.description', {
              unit: draft.unitCode,
              range: rangeText(draft.range),
            })}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-sm font-medium">
              {t('rentalBookings:dialog.formula')}
            </legend>
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
              {options.map((o) => (
                <div key={o.formula_id} className="flex items-center gap-2">
                  <RadioGroupItem value={o.formula_id} id={`rf-${o.formula_id}`} disabled={!o.ok} />
                  <Label
                    htmlFor={`rf-${o.formula_id}`}
                    className={o.ok ? '' : 'text-muted-foreground'}
                  >
                    {o.formula_code}
                    {o.ok
                      ? ` · ${formatCents(o.total_cents, 'EUR', i18n.language)}`
                      : ` · ${t(`errors:${o.problem ?? 'unknown'}`, { defaultValue: o.problem ?? '' })}`}
                    {o.formula_id === suggested ? ` · ${t('rentalBookings:dialog.suggested')}` : ''}
                  </Label>
                </div>
              ))}
            </RadioGroup>
          </fieldset>
          <Field>
            <FieldLabel htmlFor="rb-skipper">{t('rentalBookings:fields.skipper')}</FieldLabel>
            <Select value={skipperId} onValueChange={setSkipperId}>
              <SelectTrigger id="rb-skipper">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>{t('rentalBookings:fields.noSkipper')}</SelectItem>
                {(skippers.data?.items ?? [])
                  .filter((s) => s.active)
                  .map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </Field>
          {activeExtras.length > 0 ? (
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 text-sm font-medium">
                {t('rentalBookings:fields.extras')}
              </legend>
              {activeExtras.map((x) => (
                <div key={x.id} className="flex items-center gap-2">
                  <Checkbox
                    id={`rx-${x.id}`}
                    checked={extras.includes(x.id)}
                    onCheckedChange={(c) =>
                      setExtras((cur) =>
                        c === true ? [...cur, x.id] : cur.filter((v) => v !== x.id),
                      )
                    }
                  />
                  <Label htmlFor={`rx-${x.id}`}>{x.item_code}</Label>
                </div>
              ))}
            </fieldset>
          ) : null}
          <div className="grid gap-3 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="rb-name">{t('rentalBookings:fields.renterName')}</FieldLabel>
              <Input
                id="rb-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={80}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="rb-phone">{t('rentalBookings:fields.renterPhone')}</FieldLabel>
              <Input
                id="rb-phone"
                type="tel"
                value={phone}
                onChange={(e) => setPhone(e.target.value)}
                maxLength={40}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="rb-email">{t('rentalBookings:fields.renterEmail')}</FieldLabel>
              <Input
                id="rb-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </Field>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="rb-customer-q">
                {t('rentalBookings:fields.customerSearch')}
              </FieldLabel>
              <Input
                id="rb-customer-q"
                value={customerQ}
                onChange={(e) => setCustomerQ(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="rb-customer">{t('rentalBookings:fields.customer')}</FieldLabel>
              <Select value={customerId} onValueChange={setCustomerId}>
                <SelectTrigger id="rb-customer">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>{t('rentalBookings:fields.noCustomer')}</SelectItem>
                  {customerQ.trim().length >= 2
                    ? (customers.data?.pages.flatMap((pg) => pg.items) ?? []).map((c) => (
                        <SelectItem key={c.id} value={c.id}>
                          {c.display_name}
                        </SelectItem>
                      ))
                    : null}
                </SelectContent>
              </Select>
            </Field>
          </div>
          <div className="flex items-center gap-2">
            <Checkbox
              id="rb-invoice"
              checked={invoice}
              disabled={customerId === NONE}
              onCheckedChange={(c) => setInvoice(c === true)}
            />
            <Label htmlFor="rb-invoice">{t('rentalBookings:fields.invoice')}</Label>
          </div>
          {conflictIds.length > 0 ? (
            <Alert variant="destructive" data-testid="rental-booking-conflict">
              <AlertDescription>{t(`rentalBookings:conflict.${conflict!.type}`)}</AlertDescription>
            </Alert>
          ) : (
            <ProblemAlert error={create.error} />
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onClose()}>
            {t('rentalBookings:actions.close')}
          </Button>
          <Button
            variant="secondary"
            disabled={!formulaId || create.isPending}
            onClick={() => submit(true)}
          >
            {t('rentalBookings:actions.hold')}
          </Button>
          <Button disabled={!formulaId || create.isPending} onClick={() => submit(false)}>
            {t('rentalBookings:actions.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
