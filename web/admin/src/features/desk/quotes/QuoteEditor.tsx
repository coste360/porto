import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useState } from 'react';

import { useCreateQuote, useUpdateQuote, type Quote, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';
import { Textarea } from '@porto/shared/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useMarinaToday } from '../../registry/lib/today';
import { CustomerField, type CustomerChoice } from '../components/CustomerField';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { ResourceField } from '../components/ResourceField';
import { VesselField } from '../components/VesselField';
import { vesselDims, type VesselValue } from '../lib/vessel';
import { defaultEndsOn } from '../lib/contract';
import { addDays, isIsoDate } from '../lib/dates';
import {
  MAX_OPTIONS,
  emptyOption,
  optionFromSolution,
  quoteInput,
  type OptionDraft,
  type QuoteDraft,
} from '../lib/quote';
import { SolutionPicker } from '../search/SolutionPicker';

const TERMS = ['annual', 'seasonal', 'monthly', 'period'] as const;

export type QuoteEditorProps = {
  marinaId: string;
  initial: QuoteDraft;
  initialCustomer?: CustomerChoice;
  initialVessel?: VesselValue;
  /** Editing a draft quote (PATCH with If-Match); creating otherwise. */
  quote?: Quote;
  onSaved: (q: Quote) => void;
  onCancel?: () => void;
};

/** Quote editor: customer, vessel, period, term, 1–3 options with placements (search or map) and holds. */
export function QuoteEditor({
  marinaId,
  initial,
  initialCustomer = null,
  initialVessel,
  quote,
  onSaved,
  onCancel,
}: QuoteEditorProps) {
  const { t } = useT();
  const today = useMarinaToday();
  const create = useCreateQuote(marinaId);
  const update = useUpdateQuote(marinaId);
  const [draft, setDraft] = useState<QuoteDraft>(initial);
  const [customer, setCustomer] = useState<CustomerChoice>(initialCustomer);
  const [vessel, setVessel] = useState<VesselValue>(
    initialVessel ?? { kind: 'existing', id: initial.vesselId, label: '', dims: null },
  );
  const [error, setError] = useState<string | null>(null);
  const set = (patch: Partial<QuoteDraft>) => setDraft((d) => ({ ...d, ...patch }));
  const setOption = (i: number, o: OptionDraft) =>
    set({ options: draft.options.map((x, j) => (j === i ? o : x)) });
  const full: QuoteDraft = {
    ...draft,
    customerId: customer?.id ?? null,
    vesselId: vessel.kind === 'existing' ? vessel.id : null,
  };
  const mutation = quote ? update : create;
  const dims = vesselDims(vessel);
  const searchRequest: Schemas['OccupancySearchRequest'] | null =
    full.vesselId &&
    isIsoDate(draft.startsOn) &&
    isIsoDate(draft.endsOn) &&
    draft.startsOn < draft.endsOn
      ? {
          purpose: draft.kind === 'transit' ? 'transit' : 'contract',
          vessel_id: full.vesselId,
          arrival: draft.startsOn,
          departure: draft.endsOn,
          channel: 'desk',
          ...(draft.kind === 'mooring' ? { term: draft.term } : {}),
          ...(full.customerId ? { customer_id: full.customerId } : {}),
        }
      : null;

  const save = () => {
    const built = quoteInput(full);
    if (!built.ok) {
      setError(built.error);
      return;
    }
    setError(null);
    if (quote)
      update.mutate(
        { quoteId: quote.id, version: quote.version, body: built.body },
        { onSuccess: onSaved },
      );
    else create.mutate(built.body, { onSuccess: onSaved });
  };

  return (
    <form
      className="flex flex-col gap-4"
      data-testid="desk-quote-editor"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <ToggleGroup
        type="single"
        variant="outline"
        value={draft.kind}
        onValueChange={(v) => v && set({ kind: v as QuoteDraft['kind'] })}
        aria-label={t('desk:quote.kind')}
      >
        <ToggleGroupItem value="transit">{t('occupancyEnums:quoteKind.transit')}</ToggleGroupItem>
        <ToggleGroupItem value="mooring">{t('occupancyEnums:quoteKind.mooring')}</ToggleGroupItem>
      </ToggleGroup>
      <div className="grid gap-3 md:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="quote-customer">{t('desk:fields.customer')}</FieldLabel>
          <CustomerField
            marinaId={marinaId}
            id="quote-customer"
            value={customer}
            onChange={setCustomer}
          />
        </Field>
        <VesselField marinaId={marinaId} idPrefix="quote" value={vessel} onChange={setVessel} />
      </div>
      <div className="grid gap-3 md:grid-cols-4">
        {draft.kind === 'mooring' ? (
          <Field>
            <FieldLabel htmlFor="quote-term">{t('desk:fields.term')}</FieldLabel>
            <Select
              value={draft.term}
              onValueChange={(v) => {
                const term = v as QuoteDraft['term'];
                const end = draft.startsOn ? defaultEndsOn(term, draft.startsOn) : null;
                set({ term, ...(end ? { endsOn: end } : {}) });
              }}
            >
              <SelectTrigger id="quote-term" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TERMS.map((term) => (
                  <SelectItem key={term} value={term}>
                    {t(`occupancyEnums:contractTerm.${term}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ) : null}
        <Field>
          <FieldLabel htmlFor="quote-starts">{t('desk:fields.startsOn')}</FieldLabel>
          <DatePicker
            id="quote-starts"
            value={draft.startsOn}
            onChange={(startsOn) => {
              const end =
                startsOn && draft.kind === 'mooring' ? defaultEndsOn(draft.term, startsOn) : null;
              set({ startsOn, ...(end ? { endsOn: end } : {}) });
            }}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="quote-ends">{t('desk:fields.endsOn')}</FieldLabel>
          <DatePicker id="quote-ends" value={draft.endsOn} onChange={(endsOn) => set({ endsOn })} />
        </Field>
        <Field>
          <FieldLabel htmlFor="quote-valid">{t('desk:quote.validUntil')}</FieldLabel>
          <DatePicker
            id="quote-valid"
            value={draft.validUntil}
            onChange={(validUntil) => set({ validUntil })}
          />
        </Field>
      </div>

      {draft.options.map((o, i) => (
        <Card key={i} data-testid="desk-quote-option">
          <CardHeader className="flex flex-row items-center justify-between gap-2">
            <CardTitle>{t('desk:quote.option', { n: i + 1 })}</CardTitle>
            {draft.options.length > 1 ? (
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('desk:quote.removeOption', { n: i + 1 })}
                onClick={() => set({ options: draft.options.filter((_, j) => j !== i) })}
              >
                <Trash2Icon />
              </Button>
            ) : null}
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="grid gap-3 md:grid-cols-2">
              <Field>
                <FieldLabel htmlFor={`quote-opt-${i}-label`}>
                  {t('desk:quote.optionLabel')}
                </FieldLabel>
                <Input
                  id={`quote-opt-${i}-label`}
                  value={o.label}
                  maxLength={120}
                  onChange={(e) => setOption(i, { ...o, label: e.target.value })}
                />
              </Field>
              <Field orientation="horizontal" className="md:pt-6">
                <Switch
                  id={`quote-opt-${i}-hold`}
                  checked={o.hold}
                  onCheckedChange={(hold) => setOption(i, { ...o, hold })}
                />
                <FieldLabel htmlFor={`quote-opt-${i}-hold`}>{t('desk:quote.hold')}</FieldLabel>
              </Field>
            </div>
            {o.targets.map((target, k) => (
              <div key={k} className="flex flex-col gap-2">
                {o.targets.length > 1 ? (
                  <span className="text-sm font-medium">
                    {t('desk:quote.segment', { n: k + 1 })}
                  </span>
                ) : null}
                <ResourceField
                  marinaId={marinaId}
                  idPrefix={`quote-opt-${i}-${k}`}
                  value={target}
                  onChange={(p) => {
                    const targets = [...o.targets] as OptionDraft['targets'];
                    targets[k] = p;
                    setOption(i, { ...o, targets });
                  }}
                  vessel={dims}
                  date={draft.startsOn ?? today}
                  from={draft.startsOn ?? today}
                  to={draft.endsOn ? addDays(draft.endsOn, -1) : today}
                />
              </div>
            ))}
            {o.targets.length > 1 ? (
              <Field>
                <FieldLabel htmlFor={`quote-opt-${i}-split`}>{t('desk:quote.splitOn')}</FieldLabel>
                <DatePicker
                  id={`quote-opt-${i}-split`}
                  value={o.splitOn}
                  onChange={(splitOn) => setOption(i, { ...o, splitOn })}
                />
              </Field>
            ) : null}
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() =>
                  setOption(
                    i,
                    o.targets.length === 1
                      ? { ...o, targets: [o.targets[0], null] }
                      : { ...o, targets: [o.targets[0]], splitOn: null },
                  )
                }
              >
                {o.targets.length === 1
                  ? t('desk:quote.addSegment')
                  : t('desk:quote.removeSegment')}
              </Button>
            </div>
            <SolutionPicker
              marinaId={marinaId}
              request={searchRequest}
              onPick={(s) =>
                setOption(i, { ...optionFromSolution(s), label: o.label, hold: o.hold })
              }
            />
          </CardContent>
        </Card>
      ))}
      {draft.options.length < MAX_OPTIONS ? (
        <div>
          <Button
            type="button"
            variant="outline"
            onClick={() => set({ options: [...draft.options, emptyOption()] })}
          >
            <PlusIcon />
            {t('desk:quote.addOption')}
          </Button>
        </div>
      ) : null}
      <Field>
        <FieldLabel htmlFor="quote-notes">{t('desk:fields.notes')}</FieldLabel>
        <Textarea
          id="quote-notes"
          value={draft.notes}
          maxLength={4000}
          onChange={(e) => set({ notes: e.target.value })}
        />
      </Field>
      {error ? <FieldError>{t(error)}</FieldError> : null}
      <DeskProblemAlert error={mutation.error} />
      <div className="flex flex-wrap justify-end gap-2">
        {onCancel ? (
          <Button type="button" variant="outline" onClick={onCancel}>
            {t('common:actions.cancel')}
          </Button>
        ) : null}
        <Button type="submit" disabled={mutation.isPending}>
          {quote ? t('common:actions.save') : t('desk:quote.create')}
        </Button>
      </div>
    </form>
  );
}
