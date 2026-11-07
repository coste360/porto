import { Link } from '@tanstack/react-router';
import { useState } from 'react';

import { useCreateTransit, type Contract } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatDateTime, formatIsoDate } from '@porto/shared/lib/format';
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
import { DatePicker } from '@porto/shared/ui/patterns';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { useMarinaToday } from '../../registry/lib/today';
import { CustomerField, type CustomerChoice } from '../components/CustomerField';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { ResourceField } from '../components/ResourceField';
import { StatusBadge } from '../components/StatusBadge';
import { VesselField, VesselLabel } from '../components/VesselField';
import { NO_VESSEL, newVesselInput, vesselDims, type VesselValue } from '../lib/vessel';
import { addDays, daysBetween, lastNightOf } from '../lib/dates';
import { solutionCodes } from '../lib/search';
import {
  TRANSIT_STEPS,
  stepReady,
  transitBody,
  type TransitDraft,
  type TransitStep,
} from '../lib/transit';
import type { DeskPrefill } from '../public';

type Props = {
  marinaId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  prefill?: DeskPrefill;
};

/** New transit wizard: vessel and customer, stay and placement, price and hold or manual confirmation. */
export function NewTransitDialog(props: Props) {
  // Remount on every opening so the wizard starts from the prefill.
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className="max-h-screen overflow-y-auto sm:max-w-2xl"
        data-testid="desk-transit-dialog"
      >
        {props.open ? <TransitWizard {...props} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function TransitWizard({ marinaId, onOpenChange, prefill }: Props) {
  const { t, i18n } = useT();
  const { can } = useCurrentMarina();
  const today = useMarinaToday();
  const create = useCreateTransit(marinaId);
  const [step, setStep] = useState<TransitStep>(prefill?.vesselId ? 'stay' : 'vessel');
  const [vessel, setVessel] = useState<VesselValue>(
    prefill?.vesselId
      ? { kind: 'existing', id: prefill.vesselId, label: '', dims: null }
      : NO_VESSEL,
  );
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const arrival0 = prefill?.arrival ?? prefill?.solution?.segments[0]?.first_night ?? today;
  const [draft, setDraft] = useState<Omit<TransitDraft, 'vesselId' | 'newVessel' | 'customerId'>>({
    arrival: arrival0,
    departure: prefill?.departure ?? addDays(arrival0, 1),
    solution: prefill?.solution ?? null,
    target: prefill?.target ?? null,
    expectedArrivalTime: '',
    notes: '',
    confirm: null,
    fitOverrideReason: null,
  });
  const [created, setCreated] = useState<Contract | null>(null);

  const full: TransitDraft = {
    ...draft,
    vesselId: vessel.kind === 'existing' ? vessel.id : null,
    newVessel: newVesselInput(vessel),
    customerId: customer?.id ?? null,
  };
  const set = (patch: Partial<typeof draft>) => setDraft((d) => ({ ...d, ...patch }));
  const setStay = (patch: { arrival?: string | null; departure?: string | null }) =>
    // Dates of a search solution belong to it: changing them keeps its first resource as a plain target.
    set({
      ...patch,
      ...(draft.solution
        ? {
            solution: null,
            target: draft.solution.segments[0]
              ? {
                  resource_type: draft.solution.segments[0].resource_type,
                  resource_id: draft.solution.segments[0].resource_id,
                  ...(draft.solution.segments[0].x_cm !== undefined
                    ? { x_cm: draft.solution.segments[0].x_cm }
                    : {}),
                }
              : null,
          }
        : {}),
    });

  const submit = (override?: string) => {
    const body = transitBody(override ? { ...full, fitOverrideReason: override } : full);
    if (!body) return;
    create.mutate(body, { onSuccess: (c) => setCreated(c) });
  };

  if (created) {
    return (
      <>
        <DialogHeader>
          <DialogTitle>{t('desk:transit.created', { code: created.code })}</DialogTitle>
          <DialogDescription>
            {created.status === 'held' && created.hold_expires_at
              ? t('desk:transit.heldUntil', {
                  at: formatDateTime(created.hold_expires_at, i18n.language),
                })
              : t('desk:transit.confirmedHint')}
          </DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2">
          <StatusBadge kind="transit" status={created.status} />
          <span className="tabular-nums">
            {formatCents(created.total_cents, 'EUR', i18n.language)}
          </span>
        </div>
        <DialogFooter>
          <Button variant="outline" asChild>
            <Link
              to="/m/$marinaId/transits/$contractId"
              params={{ marinaId, contractId: created.id }}
            >
              {t('desk:transit.open')}
            </Link>
          </Button>
          <Button onClick={() => onOpenChange(false)}>{t('common:actions.close')}</Button>
        </DialogFooter>
      </>
    );
  }

  const nights =
    draft.arrival && draft.departure ? Math.max(0, daysBetween(draft.arrival, draft.departure)) : 0;
  const idx = TRANSIT_STEPS.indexOf(step);

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t('desk:transit.newTitle')}</DialogTitle>
        <DialogDescription>
          {t('desk:transit.step', { n: idx + 1, total: TRANSIT_STEPS.length })} ·{' '}
          {t(`desk:transit.steps.${step}`)}
        </DialogDescription>
      </DialogHeader>
      {prefill?.vesselId ? (
        <VesselLabel marinaId={marinaId} value={vessel} onChange={setVessel} />
      ) : null}

      {step === 'vessel' ? (
        <div className="flex flex-col gap-4">
          <VesselField
            marinaId={marinaId}
            idPrefix="transit"
            value={vessel}
            onChange={setVessel}
            allowNew
          />
          {can('customers.read') ? (
            <Field>
              <FieldLabel htmlFor="transit-customer">
                {t('desk:fields.customerOptional')}
              </FieldLabel>
              <CustomerField
                marinaId={marinaId}
                id="transit-customer"
                value={customer}
                onChange={setCustomer}
              />
            </Field>
          ) : null}
        </div>
      ) : null}

      {step === 'stay' ? (
        <div className="flex flex-col gap-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="transit-arrival">{t('desk:fields.arrival')}</FieldLabel>
              <DatePicker
                id="transit-arrival"
                value={draft.arrival}
                onChange={(arrival) => setStay({ arrival })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="transit-departure">{t('desk:fields.departure')}</FieldLabel>
              <DatePicker
                id="transit-departure"
                value={draft.departure}
                onChange={(departure) => setStay({ departure })}
              />
            </Field>
          </div>
          <p className="text-muted-foreground text-sm">
            {t('desk:search.nights', { count: nights })}
          </p>
          {draft.solution ? (
            <p className="text-sm" data-testid="desk-transit-solution">
              {t('desk:transit.fromSearch', { codes: solutionCodes(draft.solution) })}
            </p>
          ) : (
            <ResourceField
              marinaId={marinaId}
              idPrefix="transit"
              value={draft.target}
              onChange={(target) => set({ target })}
              vessel={vesselDims(vessel)}
              date={draft.arrival ?? today}
              from={draft.arrival ?? today}
              to={draft.departure ? lastNightOf(draft.departure) : today}
            />
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="transit-time">{t('desk:fields.expectedArrivalTime')}</FieldLabel>
              <Input
                id="transit-time"
                type="time"
                value={draft.expectedArrivalTime}
                onChange={(e) => set({ expectedArrivalTime: e.target.value })}
              />
            </Field>
          </div>
          <Field>
            <FieldLabel htmlFor="transit-notes">{t('desk:fields.notes')}</FieldLabel>
            <Textarea
              id="transit-notes"
              value={draft.notes}
              maxLength={4000}
              onChange={(e) => set({ notes: e.target.value })}
            />
          </Field>
        </div>
      ) : null}

      {step === 'confirm' ? (
        <div className="flex flex-col gap-4">
          <dl className="grid grid-cols-2 gap-2 text-sm">
            <dt className="text-muted-foreground">{t('desk:fields.stay')}</dt>
            <dd>
              {draft.arrival && draft.departure
                ? t('desk:stayRange', {
                    from: formatIsoDate(draft.arrival),
                    to: formatIsoDate(draft.departure),
                    count: nights,
                  })
                : ''}
            </dd>
            <dt className="text-muted-foreground">{t('desk:fields.price')}</dt>
            <dd className="tabular-nums" data-testid="desk-transit-price">
              {draft.solution?.pricing
                ? formatCents(draft.solution.pricing.total_cents, 'EUR', i18n.language)
                : t('desk:transit.priceOnCreate')}
            </dd>
          </dl>
          <Field orientation="horizontal">
            <Checkbox
              id="transit-confirm"
              checked={draft.confirm !== null}
              onCheckedChange={(c) =>
                set({ confirm: c === true ? { reference: '', payAtDeparture: false } : null })
              }
            />
            <FieldLabel htmlFor="transit-confirm">{t('desk:transit.confirmManual')}</FieldLabel>
          </Field>
          {draft.confirm ? (
            <ManualConfirmFields
              idPrefix="transit"
              value={draft.confirm}
              onChange={(confirm) => set({ confirm })}
            />
          ) : null}
          {draft.confirm && !customer ? (
            <p className="text-destructive text-sm">{t('errors:customer_required')}</p>
          ) : null}
          <DeskProblemAlert
            error={create.error}
            onAlternative={(s) => {
              create.reset();
              set({ solution: s, target: null });
            }}
            {...(can('occupancy.override_fit') ? { onOverride: (r: string) => submit(r) } : {})}
          />
        </div>
      ) : null}

      <DialogFooter>
        {idx > 0 ? (
          <Button variant="outline" onClick={() => setStep(TRANSIT_STEPS[idx - 1]!)}>
            {t('common:actions.back')}
          </Button>
        ) : null}
        {step !== 'confirm' ? (
          <Button
            onClick={() => setStep(TRANSIT_STEPS[idx + 1]!)}
            disabled={!stepReady(step, full)}
          >
            {t('common:actions.continue')}
          </Button>
        ) : (
          <Button
            onClick={() => submit()}
            disabled={!stepReady('confirm', full) || create.isPending}
          >
            {draft.confirm ? t('desk:transit.submitConfirm') : t('desk:transit.submitHold')}
          </Button>
        )}
      </DialogFooter>
    </>
  );
}

/** Reference and "pay at departure" of the manual driver (no money moves in M3). */
export function ManualConfirmFields({
  idPrefix,
  value,
  onChange,
}: {
  idPrefix: string;
  value: { reference: string; payAtDeparture: boolean };
  onChange: (v: { reference: string; payAtDeparture: boolean }) => void;
}) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3">
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-reference`}>{t('desk:transit.reference')}</FieldLabel>
        <Input
          id={`${idPrefix}-reference`}
          value={value.reference}
          maxLength={200}
          onChange={(e) => onChange({ ...value, reference: e.target.value })}
        />
      </Field>
      <Field orientation="horizontal">
        <Checkbox
          id={`${idPrefix}-pay-departure`}
          checked={value.payAtDeparture}
          onCheckedChange={(c) => onChange({ ...value, payAtDeparture: c === true })}
        />
        <FieldLabel htmlFor={`${idPrefix}-pay-departure`}>
          {t('desk:transit.payAtDeparture')}
        </FieldLabel>
      </Field>
    </div>
  );
}
