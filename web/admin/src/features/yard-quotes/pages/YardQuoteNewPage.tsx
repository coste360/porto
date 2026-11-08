import { useNavigate } from '@tanstack/react-router';
import { useEffect, useMemo, useReducer, useState, type Dispatch } from 'react';

import {
  formulaIssuesOf,
  useCreateYardQuote,
  useCustomers,
  usePreviewYardQuote,
  useVessels,
  useWorkPackages,
  useYardItems,
  useYardQualifications,
  type FormulaValue,
  type YardBillingMode,
  type YardVariableInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatCm, parseQtyToMilli } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader, PercentInput } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import {
  EnumSelect,
  M4ProblemAlert,
  PageFrame,
  Section,
  StepBar,
} from '../../contracts-m4/components/kit';
import { IssueList } from '../../yard-catalog/public';
import { QuoteLines, QuoteTotals } from '../components/QuoteLines';
import {
  WIZARD_STEPS,
  canAdvance,
  initialWizard,
  toQuoteInput,
  valueText,
  wizardReducer,
  type WizardAction,
  type WizardState,
} from '../lib';

const BILLING_MODES: readonly YardBillingMode[] = ['actual', 'fixed', 'progress'];

/** Guided yard quote (m10-plan §4.2): vessel and customer, catalogue, job variables, summary, conditions. Every step
 * asks the stateless preview; quantities and prices come from the formulas and the price lists. */
export function YardQuoteNewPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const navigate = useNavigate();
  const [state, dispatch] = useReducer(wizardReducer, undefined, initialWizard);
  const preview = usePreviewYardQuote(marinaId);
  const create = useCreateYardQuote(marinaId);
  const quals = useYardQualifications(marinaId);
  const input = useMemo(() => toQuoteInput(state), [state]);
  const ready = state.customerId !== '' && state.vesselId !== '' && state.selections.length > 0;
  const key = JSON.stringify(input);
  const { mutate } = preview;
  useEffect(() => {
    if (!ready) return;
    const timer = setTimeout(() => mutate(JSON.parse(key)), 250);
    return () => clearTimeout(timer);
  }, [key, ready, mutate]);
  const data = ready ? preview.data : undefined;
  const submit = () =>
    create.mutate(input, {
      onSuccess: (q) =>
        void navigate({
          to: '/m/$marinaId/yard/quotes/$quoteId',
          params: { marinaId, quoteId: q.id },
        }),
    });
  return (
    <RequirePermission allowed={can('yard.manage')}>
      <PageFrame>
        <div className="flex flex-col gap-4" data-testid="yard-quote-wizard">
          <PageHeader
            title={t('yardQuotes:newTitle')}
            description={t('yardQuotes:wizard.description')}
          />
          <StepBar
            steps={WIZARD_STEPS}
            current={state.step}
            label={t('yardQuotes:wizard.steps')}
            labelOf={(s) => t(`yardQuotes:wizard.step.${s}`)}
            onSelect={(s) => dispatch({ type: 'goto', step: s })}
          />
          {state.step === 'vessel' ? (
            <VesselStep marinaId={marinaId} state={state} dispatch={dispatch} />
          ) : null}
          {state.step === 'catalog' ? (
            <CatalogStep marinaId={marinaId} state={state} dispatch={dispatch} />
          ) : null}
          {state.step === 'variables' ? (
            <VariablesStep variables={data?.variables ?? []} state={state} dispatch={dispatch} />
          ) : null}
          {state.step === 'summary' && data ? (
            <Section title={t('yardQuotes:wizard.step.summary')}>
              <QuoteLines
                lines={data.lines}
                overrides={state.overrides}
                onOverride={(seq, field, value) =>
                  dispatch({ type: 'override', seq, field, value })
                }
              />
              <QuoteTotals preview={data} qualifications={quals.data?.items ?? []} />
              {data.blocking ? (
                <p className="text-destructive text-sm" data-testid="blocking">
                  {t('yardQuotes:wizard.blocking')}
                </p>
              ) : null}
            </Section>
          ) : null}
          {state.step === 'conditions' ? (
            <ConditionsStep state={state} dispatch={dispatch} />
          ) : null}
          {data && state.step !== 'summary' ? (
            <p className="text-muted-foreground text-sm" data-testid="wizard-running-total">
              {t('yardQuotes:wizard.runningTotal', { lines: data.lines.length })}{' '}
              <QuoteGross cents={data.totals.gross_cents} />
            </p>
          ) : null}
          <IssueList issues={formulaIssuesOf(preview.error ?? create.error)} />
          <M4ProblemAlert error={preview.error ?? create.error} />
          <div className="flex justify-between gap-2">
            <Button
              variant="outline"
              disabled={state.step === 'vessel'}
              onClick={() => dispatch({ type: 'back' })}
            >
              {t('common:actions.back')}
            </Button>
            {state.step === 'conditions' ? (
              <Button onClick={submit} disabled={!ready || create.isPending}>
                {t('yardQuotes:wizard.create')}
              </Button>
            ) : (
              <Button onClick={() => dispatch({ type: 'next' })} disabled={!canAdvance(state)}>
                {t('common:actions.continue')}
              </Button>
            )}
          </div>
        </div>
      </PageFrame>
    </RequirePermission>
  );
}

function QuoteGross({ cents }: { cents: number }) {
  const { i18n } = useT();
  return <strong className="tabular-nums">{formatCents(cents, 'EUR', i18n.language)}</strong>;
}

type StepProps = { state: WizardState; dispatch: Dispatch<WizardAction> };
type TermsPatch = Extract<WizardAction, { type: 'terms' }>['patch'];

function VesselStep({ marinaId, state, dispatch }: StepProps & { marinaId: string }) {
  const { t, i18n } = useT();
  const [cq, setCq] = useState('');
  const [vq, setVq] = useState('');
  const customers = useCustomers(marinaId, cq.trim() ? { q: cq.trim() } : {});
  const vessels = useVessels(
    marinaId,
    vq.trim() ? { q: vq.trim() } : state.customerId ? { customer_id: state.customerId } : {},
  );
  const customerList = customers.data?.pages.flatMap((p) => p.items) ?? [];
  const vesselList = vessels.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Section title={t('yardQuotes:wizard.customer')}>
        <Field>
          <FieldLabel htmlFor="wz-cq">{t('yardQuotes:wizard.searchCustomer')}</FieldLabel>
          <Input id="wz-cq" value={cq} onChange={(e) => setCq(e.target.value)} />
        </Field>
        <ul className="flex flex-col gap-1">
          {customerList.slice(0, 20).map((c) => (
            <li key={c.id}>
              <Button
                variant={state.customerId === c.id ? 'default' : 'ghost'}
                size="sm"
                className="w-full justify-start"
                aria-pressed={state.customerId === c.id}
                onClick={() => dispatch({ type: 'customer', id: c.id })}
              >
                {c.display_name}
              </Button>
            </li>
          ))}
        </ul>
      </Section>
      <Section title={t('yardQuotes:wizard.vessel')}>
        <Field>
          <FieldLabel htmlFor="wz-vq">{t('yardQuotes:wizard.searchVessel')}</FieldLabel>
          <Input id="wz-vq" value={vq} onChange={(e) => setVq(e.target.value)} />
        </Field>
        <ul className="flex flex-col gap-1">
          {vesselList.slice(0, 20).map((v) => (
            <li key={v.id}>
              <Button
                variant={state.vesselId === v.id ? 'default' : 'ghost'}
                size="sm"
                className="w-full justify-start"
                aria-pressed={state.vesselId === v.id}
                onClick={() => dispatch({ type: 'vessel', id: v.id })}
              >
                {`${v.name} · ${formatCm(v.loa_cm, i18n.language)} × ${formatCm(v.beam_cm, i18n.language)}`}
              </Button>
            </li>
          ))}
        </ul>
      </Section>
    </div>
  );
}

function CatalogStep({ marinaId, state, dispatch }: StepProps & { marinaId: string }) {
  const { t } = useT();
  const [q, setQ] = useState('');
  const packages = useWorkPackages(marinaId, { active: true });
  const items = useYardItems(marinaId, {
    yard_enabled: true,
    ...(q.trim() ? { q: q.trim() } : {}),
  });
  const has = (sel: { package_id?: string; item_id?: string }) =>
    state.selections.some(
      (s) =>
        (s.package_id ?? '') === (sel.package_id ?? '') &&
        (s.item_id ?? '') === (sel.item_id ?? ''),
    );
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Section title={t('yardQuotes:wizard.packages')}>
        {(packages.data?.items ?? []).map((p) => (
          <Field key={p.id} orientation="horizontal">
            <Checkbox
              id={`wz-p-${p.id}`}
              checked={has({ package_id: p.id })}
              onCheckedChange={() => dispatch({ type: 'toggle', selection: { package_id: p.id } })}
            />
            <FieldLabel htmlFor={`wz-p-${p.id}`}>{`${p.code} · ${p.name.it}`}</FieldLabel>
          </Field>
        ))}
      </Section>
      <Section title={t('yardQuotes:wizard.items')}>
        <Field>
          <FieldLabel htmlFor="wz-iq">{t('yardQuotes:wizard.searchItem')}</FieldLabel>
          <Input id="wz-iq" value={q} onChange={(e) => setQ(e.target.value)} />
        </Field>
        {(items.data?.items ?? [])
          .filter((i) => i.configured)
          .map((i) => (
            <Field key={i.item_id} orientation="horizontal">
              <Checkbox
                id={`wz-i-${i.item_id}`}
                checked={has({ item_id: i.item_id })}
                onCheckedChange={() =>
                  dispatch({ type: 'toggle', selection: { item_id: i.item_id } })
                }
              />
              <FieldLabel htmlFor={`wz-i-${i.item_id}`}>
                {`${i.code} · ${i.name.it}`}
                {i.category ? (
                  <span className="text-muted-foreground ml-2 text-xs">{i.category}</span>
                ) : null}
              </FieldLabel>
            </Field>
          ))}
      </Section>
    </div>
  );
}

function VariablesStep({
  variables,
  state,
  dispatch,
}: StepProps & { variables: readonly YardVariableInput[] }) {
  const { t } = useT();
  if (variables.length === 0)
    return <p className="text-muted-foreground text-sm">{t('yardQuotes:wizard.noVariables')}</p>;
  return (
    <Section title={t('yardQuotes:wizard.step.variables')}>
      <div className="grid gap-3 md:grid-cols-2">
        {variables.map((v) => (
          <VariableField key={v.name} variable={v} state={state} dispatch={dispatch} />
        ))}
      </div>
    </Section>
  );
}

function VariableField({
  variable: v,
  state,
  dispatch,
}: StepProps & { variable: YardVariableInput }) {
  const { t, i18n } = useT();
  const loc = i18n.language;
  const isJob = v.source === 'job';
  const current = (isJob ? state.jobInputs : state.vesselOverrides)[v.name] ?? v.value ?? v.default;
  const set = (value: FormulaValue | null) =>
    dispatch(
      isJob
        ? { type: 'jobInput', name: v.name, value }
        : { type: 'vesselOverride', name: v.name, value },
    );
  const id = `wz-var-${v.name}`;
  const label = `${v.label.it}${v.unit !== 'none' ? ` (${t(`yardCatalog:units.${v.unit}`)})` : ''}`;
  const range =
    v.min_milli !== undefined || v.max_milli !== undefined
      ? t('yardQuotes:wizard.range', {
          min: valueText(v.min_milli, loc),
          max: valueText(v.max_milli, loc),
        })
      : undefined;
  if (v.kind === 'bool')
    return (
      <Field orientation="horizontal">
        <Switch id={id} checked={current === true} onCheckedChange={(c) => set(c)} />
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
      </Field>
    );
  if (v.kind === 'enum')
    return (
      <EnumSelect
        id={id}
        label={label}
        values={v.enum_values ?? []}
        value={typeof current === 'string' ? current : ''}
        onChange={(e) => set(e || null)}
        labelOf={(e) => e}
      />
    );
  return (
    <Field>
      <FieldLabel htmlFor={id}>
        {label}
        {v.required ? (
          <Badge variant="destructive" className="ml-2">
            {t('yardQuotes:wizard.required')}
          </Badge>
        ) : null}
      </FieldLabel>
      <Input
        id={id}
        inputMode="decimal"
        defaultValue={valueText(current, loc)}
        onBlur={(e) => {
          const m = e.target.value.trim() ? parseQtyToMilli(e.target.value, loc) : null;
          set(m);
        }}
      />
      {range ? <FieldDescription>{range}</FieldDescription> : null}
    </Field>
  );
}

function ConditionsStep({ state, dispatch }: StepProps) {
  const { t } = useT();
  const terms = (patch: TermsPatch) => dispatch({ type: 'terms', patch });
  return (
    <Section title={t('yardQuotes:wizard.step.conditions')}>
      <div className="grid gap-3 md:grid-cols-3">
        <Field>
          <FieldLabel htmlFor="wz-valid">{t('yardQuotes:fields.validUntil')}</FieldLabel>
          <Input
            id="wz-valid"
            type="date"
            value={state.validUntil}
            onChange={(e) => terms({ validUntil: e.target.value })}
          />
          <FieldDescription>{t('yardQuotes:wizard.validityHint')}</FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="wz-deposit">{t('yardQuotes:fields.deposit')}</FieldLabel>
          <PercentInput
            id="wz-deposit"
            value={state.depositBp}
            onChange={(v) => terms({ depositBp: v })}
          />
          <FieldDescription>{t('yardQuotes:wizard.depositHint')}</FieldDescription>
        </Field>
        <EnumSelect
          id="wz-billing"
          label={t('yardQuotes:fields.billingMode')}
          values={BILLING_MODES}
          value={state.billingMode}
          onChange={(v) => terms({ billingMode: v || 'actual' })}
          labelOf={(v) => t(`yardQuotes:billingModes.${v}`)}
        />
        <Field>
          <FieldLabel htmlFor="wz-start">{t('yardQuotes:fields.plannedStart')}</FieldLabel>
          <Input
            id="wz-start"
            type="date"
            value={state.plannedStartOn}
            onChange={(e) => terms({ plannedStartOn: e.target.value })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="wz-delivery">{t('yardQuotes:fields.plannedDelivery')}</FieldLabel>
          <Input
            id="wz-delivery"
            type="date"
            value={state.plannedDeliveryOn}
            onChange={(e) => terms({ plannedDeliveryOn: e.target.value })}
          />
        </Field>
      </div>
      <Field>
        <FieldLabel htmlFor="wz-conditions">{t('yardQuotes:fields.conditions')}</FieldLabel>
        <Textarea
          id="wz-conditions"
          maxLength={4000}
          value={state.conditions}
          onChange={(e) => terms({ conditions: e.target.value })}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="wz-notes">{t('yardQuotes:fields.notes')}</FieldLabel>
        <Textarea
          id="wz-notes"
          maxLength={2000}
          value={state.notes}
          onChange={(e) => terms({ notes: e.target.value })}
        />
      </Field>
    </Section>
  );
}
