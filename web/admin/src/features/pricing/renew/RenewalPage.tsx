import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import { ArrowLeftIcon, PlusIcon, Trash2Icon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  newIdempotencyKey,
  useAllItems,
  useApplyRenewal,
  useBerthCategories,
  usePreviewRenewal,
  usePriceList,
  type PriceListDocument,
  type RenewalPreview,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  DatePicker,
  MoneyInput,
  PageHeader,
  PercentInput,
  ProblemAlert,
} from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';
import { PriceMatrixTable } from '../../pricing-tools/components/PriceMatrixTable';
import { IntegerInput } from '../components/IntegerInput';
import { ADJUSTMENT_SCOPES, AMOUNT_ROUNDINGS, localText } from '../lib/enums';
import {
  initialState,
  renewalBody,
  shiftIsoDate,
  stepErrors,
  suggestCode,
  type AdjustmentRow,
  type RenewalState,
} from './renewal';

const route = getRouteApi('/auth/m/$marinaId/price-lists/$priceListId/renew');

type Step = 1 | 2 | 3 | 4;

/** Renewal wizard (spec 12.6): target, adjustments, preview of the matrix diff, creation of the draft. */
export function RenewalPage() {
  const { priceListId } = route.useParams();
  const { marinaId, can } = useCurrentMarina();
  const source = usePriceList(marinaId, priceListId);
  return (
    <RequirePermission allowed={can('pricing.configure')}>
      {source.isPending ? (
        <LoadingState rows={6} />
      ) : source.isError ? (
        <QueryErrorState error={source.error} onRetry={() => void source.refetch()} />
      ) : (
        <Wizard key={source.data.id} source={source.data} />
      )}
    </RequirePermission>
  );
}

function Wizard({ source }: { source: PriceListDocument }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const navigate = useNavigate();
  const items = useAllItems(marinaId);
  const categories = useBerthCategories(marinaId);
  const preview = usePreviewRenewal(marinaId, source.id);
  const apply = useApplyRenewal(marinaId, source.id);
  const [state, setState] = useState<RenewalState>(() => initialState(source));
  const [step, setStep] = useState<Step>(1);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [result, setResult] = useState<RenewalPreview | null>(null);
  const [idemKey] = useState(newIdempotencyKey);

  const set = (patch: Partial<RenewalState>) => {
    setState((s) => ({ ...s, ...patch }));
    setResult(null);
  };
  const setRow = (i: number, patch: Partial<AdjustmentRow>) =>
    set({ adjustments: state.adjustments.map((a, j) => (j === i ? { ...a, ...patch } : a)) });

  const shift = state.shift_years ?? 0;
  const from = state.valid_from ?? shiftIsoDate(source.valid_from, shift);
  const to = state.valid_to ?? shiftIsoDate(source.valid_to, shift);

  const next = async () => {
    if (step === 1 || step === 2) {
      const e = stepErrors(state, step);
      setErrors(e);
      if (Object.keys(e).length > 0) return;
    }
    if (step === 2) {
      try {
        setResult(await preview.mutateAsync(renewalBody(state)));
      } catch (err) {
        toastProblem(t, err);
        return;
      }
    }
    setStep((s) => (s < 4 ? ((s + 1) as Step) : s));
  };

  const create = async () => {
    try {
      const doc = await apply.mutateAsync({ body: renewalBody(state), idempotencyKey: idemKey });
      toast.success(t('pricing:renew.created', { code: doc.code }));
      await navigate({
        to: '/m/$marinaId/price-lists/$priceListId',
        params: { marinaId, priceListId: doc.id },
      });
    } catch (err) {
      toastProblem(t, err);
    }
  };

  const err = (path: string) => (errors[path] ? <FieldError>{t(errors[path])}</FieldError> : null);
  const stepLabels = [
    t('pricing:renew.steps.target'),
    t('pricing:renew.steps.adjustments'),
    t('pricing:renew.steps.preview'),
    t('pricing:renew.steps.create'),
  ];
  const allItems = items.data ?? [];
  const categoryCodes = (categories.data?.items ?? []).map((c) => c.code);

  return (
    <div className="flex flex-col gap-4">
      <div>
        <Button variant="ghost" size="sm" asChild>
          <Link
            to="/m/$marinaId/price-lists/$priceListId"
            params={{ marinaId, priceListId: source.id }}
          >
            <ArrowLeftIcon />
            {source.code}
          </Link>
        </Button>
      </div>
      <PageHeader
        title={t('pricing:renew.title', { code: source.code })}
        description={t('pricing:renew.description')}
      />
      <ol className="flex flex-wrap gap-2" aria-label={t('pricing:renew.stepsLabel')}>
        {stepLabels.map((label, i) => (
          <li key={label}>
            <Badge
              variant={step === i + 1 ? 'default' : 'outline'}
              aria-current={step === i + 1 ? 'step' : undefined}
            >
              {i + 1}. {label}
            </Badge>
          </li>
        ))}
      </ol>

      {step === 1 ? (
        <Card>
          <CardHeader>
            <CardTitle>{stepLabels[0]}</CardTitle>
            <CardDescription>{t('pricing:renew.targetHint')}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <div className="grid gap-4 sm:grid-cols-3">
                <Field data-invalid={!!errors.code || undefined}>
                  <FieldLabel htmlFor="renew-code">{t('pricing:lists.fields.code')}</FieldLabel>
                  <Input
                    id="renew-code"
                    value={state.code}
                    maxLength={32}
                    autoComplete="off"
                    onChange={(e) => set({ code: e.target.value })}
                  />
                  {err('code')}
                </Field>
                <Field data-invalid={!!errors.name_it || undefined}>
                  <FieldLabel htmlFor="renew-name-it">
                    {t('pricing:lists.fields.nameIt')}
                  </FieldLabel>
                  <Input
                    id="renew-name-it"
                    value={state.name_it}
                    maxLength={200}
                    onChange={(e) => set({ name_it: e.target.value })}
                  />
                  {err('name_it')}
                </Field>
                <Field>
                  <FieldLabel htmlFor="renew-name-en">
                    {t('pricing:lists.fields.nameEn')}
                  </FieldLabel>
                  <Input
                    id="renew-name-en"
                    value={state.name_en}
                    maxLength={200}
                    onChange={(e) => set({ name_en: e.target.value })}
                  />
                </Field>
              </div>
              <Field data-invalid={!!errors.shift_years || undefined} className="max-w-xs">
                <FieldLabel htmlFor="renew-shift">
                  {t('pricing:renew.fields.shiftYears')}
                </FieldLabel>
                <IntegerInput
                  id="renew-shift"
                  value={state.shift_years}
                  onChange={(n) => {
                    const years = n !== null && Number.isInteger(n) ? n : null;
                    set({
                      shift_years: n,
                      ...(years !== null && state.code === suggestCode(source.code, shift)
                        ? { code: suggestCode(source.code, years) }
                        : {}),
                    });
                  }}
                />
                <FieldDescription>{t('pricing:renew.fields.shiftHint')}</FieldDescription>
                {err('shift_years')}
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="renew-from">
                    {t('pricing:lists.fields.validFrom')}
                  </FieldLabel>
                  <DatePicker
                    id="renew-from"
                    value={from}
                    onChange={(d) => set({ valid_from: d })}
                  />
                </Field>
                <Field data-invalid={!!errors.valid_to || undefined}>
                  <FieldLabel htmlFor="renew-to">{t('pricing:lists.fields.validTo')}</FieldLabel>
                  <DatePicker id="renew-to" value={to} onChange={(d) => set({ valid_to: d })} />
                  {err('valid_to')}
                </Field>
              </div>
              <FieldDescription>
                {t('pricing:renew.fields.sourceValidity', {
                  from: formatIsoDate(source.valid_from),
                  to: formatIsoDate(source.valid_to),
                })}
              </FieldDescription>
            </FieldGroup>
          </CardContent>
        </Card>
      ) : null}

      {step === 2 ? (
        <Card>
          <CardHeader>
            <CardTitle>{stepLabels[1]}</CardTitle>
            <CardDescription>{t('pricing:renew.adjustmentsHint')}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field data-invalid={!!errors.istat_bp || undefined}>
                  <FieldLabel htmlFor="renew-istat">{t('pricing:renew.fields.istat')}</FieldLabel>
                  <PercentInput
                    id="renew-istat"
                    value={state.istat_bp}
                    locale={i18n.language}
                    onChange={(bp) => set({ istat_bp: bp })}
                  />
                  <FieldDescription>{t('pricing:renew.fields.istatHint')}</FieldDescription>
                  {err('istat_bp')}
                </Field>
                <Field>
                  <FieldLabel htmlFor="renew-rounding">
                    {t('pricing:renew.fields.rounding')}
                  </FieldLabel>
                  <Select
                    value={state.amount_rounding}
                    onValueChange={(v) =>
                      set({ amount_rounding: v as RenewalState['amount_rounding'] })
                    }
                  >
                    <SelectTrigger id="renew-rounding" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {AMOUNT_ROUNDINGS.map((r) => (
                        <SelectItem key={r} value={r}>
                          {t(`pricingEnums:amountRoundings.${r}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              </div>
              <div className="flex flex-col gap-3">
                <span className="text-sm font-medium">{t('pricing:renew.fields.adjustments')}</span>
                {state.adjustments.length === 0 ? (
                  <p className="text-muted-foreground text-sm">
                    {t('pricing:renew.noAdjustments')}
                  </p>
                ) : null}
                {state.adjustments.map((a, i) => (
                  <div key={i} className="grid items-end gap-2 sm:grid-cols-12">
                    <Field className="sm:col-span-3">
                      <FieldLabel htmlFor={`adj-${i}-scope`}>
                        {t('pricing:renew.fields.scope')}
                      </FieldLabel>
                      <Select
                        value={a.scope}
                        onValueChange={(v) => setRow(i, { scope: v as AdjustmentRow['scope'] })}
                      >
                        <SelectTrigger id={`adj-${i}-scope`} className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {ADJUSTMENT_SCOPES.map((s) => (
                            <SelectItem key={s} value={s}>
                              {t(`pricingEnums:adjustmentScopes.${s}`)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </Field>
                    <Field
                      className="sm:col-span-3"
                      data-invalid={
                        !!(
                          errors[`adjustments.${i}.item_id`] || errors[`adjustments.${i}.category`]
                        ) || undefined
                      }
                    >
                      <FieldLabel htmlFor={`adj-${i}-target`}>
                        {t('pricing:renew.fields.target')}
                      </FieldLabel>
                      {a.scope === 'item' ? (
                        <Select value={a.item_id} onValueChange={(v) => setRow(i, { item_id: v })}>
                          <SelectTrigger id={`adj-${i}-target`} className="w-full">
                            <SelectValue placeholder={t('pricing:rules.fields.itemPlaceholder')} />
                          </SelectTrigger>
                          <SelectContent>
                            {allItems.map((it) => (
                              <SelectItem key={it.id} value={it.id}>
                                {it.code} · {localText(it.name, i18n.language)}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : a.scope === 'category' ? (
                        <Select
                          value={a.category}
                          onValueChange={(v) => setRow(i, { category: v })}
                        >
                          <SelectTrigger id={`adj-${i}-target`} className="w-full">
                            <SelectValue
                              placeholder={t('pricing:renew.fields.categoryPlaceholder')}
                            />
                          </SelectTrigger>
                          <SelectContent>
                            {categoryCodes.map((c) => (
                              <SelectItem key={c} value={c}>
                                {c}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : (
                        <Input
                          id={`adj-${i}-target`}
                          value={t('pricingEnums:adjustmentScopes.all')}
                          disabled
                        />
                      )}
                      {err(`adjustments.${i}.item_id`) ?? err(`adjustments.${i}.category`)}
                    </Field>
                    <Field className="sm:col-span-2">
                      <FieldLabel htmlFor={`adj-${i}-kind`}>
                        {t('pricing:renew.fields.kind')}
                      </FieldLabel>
                      <Select
                        value={a.kind}
                        onValueChange={(v) => setRow(i, { kind: v as AdjustmentRow['kind'] })}
                      >
                        <SelectTrigger id={`adj-${i}-kind`} className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="percent">
                            {t('pricing:renew.fields.percent')}
                          </SelectItem>
                          <SelectItem value="amount">{t('pricing:renew.fields.amount')}</SelectItem>
                        </SelectContent>
                      </Select>
                    </Field>
                    <Field
                      className="sm:col-span-3"
                      data-invalid={
                        !!(
                          errors[`adjustments.${i}.percent_bp`] ||
                          errors[`adjustments.${i}.amount_cents`]
                        ) || undefined
                      }
                    >
                      <FieldLabel htmlFor={`adj-${i}-value`}>
                        {t('pricing:renew.fields.value')}
                      </FieldLabel>
                      {a.kind === 'percent' ? (
                        <PercentInput
                          id={`adj-${i}-value`}
                          value={a.percent_bp}
                          locale={i18n.language}
                          onChange={(bp) => setRow(i, { percent_bp: bp })}
                        />
                      ) : (
                        <MoneyInput
                          id={`adj-${i}-value`}
                          value={a.amount_cents}
                          locale={i18n.language}
                          onChange={(c) => setRow(i, { amount_cents: c })}
                        />
                      )}
                      {err(`adjustments.${i}.percent_bp`) ?? err(`adjustments.${i}.amount_cents`)}
                    </Field>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="sm:col-span-1"
                      aria-label={t('pricing:renew.removeAdjustment', { n: i + 1 })}
                      onClick={() =>
                        set({ adjustments: state.adjustments.filter((_, j) => j !== i) })
                      }
                    >
                      <Trash2Icon />
                    </Button>
                  </div>
                ))}
                <div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      set({
                        adjustments: [
                          ...state.adjustments,
                          {
                            scope: 'all',
                            item_id: '',
                            category: '',
                            kind: 'percent',
                            percent_bp: null,
                            amount_cents: null,
                          },
                        ],
                      })
                    }
                  >
                    <PlusIcon />
                    {t('pricing:renew.addAdjustment')}
                  </Button>
                </div>
              </div>
            </FieldGroup>
          </CardContent>
        </Card>
      ) : null}

      {step === 3 && result ? (
        <Card>
          <CardHeader>
            <CardTitle>{stepLabels[2]}</CardTitle>
            <CardDescription>
              {t('pricing:renew.summary', {
                rules: result.summary.rules,
                changed: result.summary.amounts_changed,
              })}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {result.diffs.length === 0 ? (
              <p className="text-muted-foreground text-sm">{t('pricing:renew.noDiff')}</p>
            ) : (
              <Tabs defaultValue={result.diffs[0]!.contract_type}>
                <TabsList className="flex-wrap">
                  {result.diffs.map((d) => (
                    <TabsTrigger key={d.contract_type} value={d.contract_type}>
                      {t(`pricingEnums:contractTypes.${d.contract_type}`)}
                    </TabsTrigger>
                  ))}
                </TabsList>
                {result.diffs.map((d) => (
                  <TabsContent key={d.contract_type} value={d.contract_type} className="pt-4">
                    <PriceMatrixTable
                      mode="diff"
                      before={d.before}
                      after={d.after}
                      cells={d.cells}
                    />
                  </TabsContent>
                ))}
              </Tabs>
            )}
          </CardContent>
        </Card>
      ) : null}

      {step === 4 ? (
        <Card>
          <CardHeader>
            <CardTitle>{stepLabels[3]}</CardTitle>
            <CardDescription>{t('pricing:renew.createHint')}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2 text-sm">
            <p>
              {t('pricing:renew.createSummary', {
                code: state.code,
                from: formatIsoDate(from),
                to: formatIsoDate(to),
              })}
            </p>
            <ProblemAlert error={apply.error} />
          </CardContent>
        </Card>
      ) : null}

      <div className="flex flex-wrap gap-2">
        {step > 1 ? (
          <Button variant="outline" onClick={() => setStep((s) => (s - 1) as Step)}>
            {t('common:actions.back')}
          </Button>
        ) : null}
        {step < 4 ? (
          <Button onClick={() => void next()} disabled={preview.isPending}>
            {step === 2 ? t('pricing:renew.preview') : t('common:actions.continue')}
          </Button>
        ) : (
          <Button onClick={() => void create()} disabled={apply.isPending}>
            {t('pricing:renew.create')}
          </Button>
        )}
      </div>
    </div>
  );
}
