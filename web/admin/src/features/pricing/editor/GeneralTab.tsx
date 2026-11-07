import type { ContractType, Item, PriceListSettings, Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, MoneyInput, PercentInput } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { IntegerInput } from '../components/IntegerInput';
import {
  CONTRACT_TYPES,
  DIMENSION_SOURCES,
  LENGTH_MODES,
  LENGTH_STEPS,
  SPLIT_RULES,
  TOTAL_ROUNDINGS,
  localText,
} from '../lib/enums';
import type { EditorDoc } from './model';

const INHERIT = '__inherit__';
const NONE = '__none__';

type DepositKind = 'none' | 'percent' | 'amount';

function depositKind(p: Schemas['DepositPolicy'] | undefined): DepositKind {
  if (p?.percent_bp !== undefined) return 'percent';
  if (p?.amount_cents !== undefined) return 'amount';
  return 'none';
}

function setKey<V>(map: Record<string, V> | undefined, key: string, value: V | undefined) {
  const out = { ...(map ?? {}) };
  if (value === undefined) delete out[key];
  else out[key] = value;
  return out;
}

export type GeneralTabProps = {
  doc: EditorDoc;
  onChange: (doc: EditorDoc) => void;
  readOnly: boolean;
  items: Item[];
};

/** Tab Generale: identity, validity, contract types and the engine settings of §2.3. */
export function GeneralTab({ doc, onChange, readOnly, items }: GeneralTabProps) {
  const { t, i18n } = useT();
  const s = doc.settings;
  const setSettings = (patch: Partial<PriceListSettings>) =>
    onChange({ ...doc, settings: { ...s, ...patch } });
  const berthFees = items.filter((i) => i.item_type === 'berth_fee');
  const needsWindow = doc.contract_types.some((c) => c === 'seasonal' || c === 'charter');

  return (
    <fieldset disabled={readOnly} className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle>{t('pricing:editor.general.identity')}</CardTitle>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <div className="grid gap-4 sm:grid-cols-3">
              <Field>
                <FieldLabel htmlFor="pl-field-code">{t('pricing:lists.fields.code')}</FieldLabel>
                <Input
                  id="pl-field-code"
                  value={doc.code}
                  maxLength={32}
                  autoComplete="off"
                  onChange={(e) => onChange({ ...doc, code: e.target.value })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="pl-field-name-it">
                  {t('pricing:lists.fields.nameIt')}
                </FieldLabel>
                <Input
                  id="pl-field-name-it"
                  value={doc.name.it}
                  maxLength={200}
                  onChange={(e) => onChange({ ...doc, name: { ...doc.name, it: e.target.value } })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="pl-field-name-en">
                  {t('pricing:lists.fields.nameEn')}
                </FieldLabel>
                <Input
                  id="pl-field-name-en"
                  value={doc.name.en ?? ''}
                  maxLength={200}
                  onChange={(e) =>
                    onChange({
                      ...doc,
                      name: e.target.value
                        ? { ...doc.name, en: e.target.value }
                        : { it: doc.name.it },
                    })
                  }
                />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="pl-field-valid_from">
                  {t('pricing:lists.fields.validFrom')}
                </FieldLabel>
                <DatePicker
                  id="pl-field-valid_from"
                  value={doc.valid_from}
                  disabled={readOnly}
                  onChange={(d) => d && onChange({ ...doc, valid_from: d })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="pl-field-valid_to">
                  {t('pricing:lists.fields.validTo')}
                </FieldLabel>
                <DatePicker
                  id="pl-field-valid_to"
                  value={doc.valid_to}
                  disabled={readOnly}
                  onChange={(d) => d && onChange({ ...doc, valid_to: d })}
                />
              </Field>
            </div>
            <Field>
              <FieldLabel>{t('pricing:lists.fields.contractTypes')}</FieldLabel>
              <div id="pl-field-contract_types" className="flex flex-wrap gap-4">
                {CONTRACT_TYPES.map((c) => (
                  <Field key={c} orientation="horizontal" className="w-auto">
                    <Checkbox
                      id={`pl-ct-${c}`}
                      checked={doc.contract_types.includes(c)}
                      disabled={readOnly}
                      onCheckedChange={(on) =>
                        onChange({
                          ...doc,
                          contract_types: CONTRACT_TYPES.filter((x) =>
                            x === c ? on === true : doc.contract_types.includes(x),
                          ),
                        })
                      }
                    />
                    <FieldLabel htmlFor={`pl-ct-${c}`} className="font-normal">
                      {t(`pricingEnums:contractTypes.${c}`)}
                    </FieldLabel>
                  </Field>
                ))}
              </div>
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('pricing:editor.general.perContractType')}</CardTitle>
          <CardDescription>{t('pricing:editor.general.perContractTypeHint')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="pl-field-split">
                {t('pricing:editor.general.defaultSplit')}
              </FieldLabel>
              <Select
                value={s.split?.['*'] ?? 'pro_rata_daily'}
                disabled={readOnly}
                onValueChange={(v) => setSettings({ split: setKey(s.split, '*', v as never) })}
              >
                <SelectTrigger id="pl-field-split" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SPLIT_RULES.map((r) => (
                    <SelectItem key={r} value={r}>
                      {t(`pricingEnums:splitRules.${r}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="pl-field-rounding">
                {t('pricing:editor.general.defaultRounding')}
              </FieldLabel>
              <Select
                value={s.rounding?.['*'] ?? 'none'}
                disabled={readOnly}
                onValueChange={(v) =>
                  setSettings({ rounding: setKey(s.rounding, '*', v as never) })
                }
              >
                <SelectTrigger id="pl-field-rounding" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {TOTAL_ROUNDINGS.map((r) => (
                    <SelectItem key={r} value={r}>
                      {t(`pricingEnums:totalRoundings.${r}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>
          {doc.contract_types.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              {t('pricing:editor.general.noContractTypes')}
            </p>
          ) : (
            <div className="overflow-x-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('pricing:editor.general.contractType')}</TableHead>
                    <TableHead>{t('pricing:editor.general.primaryItem')}</TableHead>
                    <TableHead>{t('pricing:editor.general.split')}</TableHead>
                    <TableHead>{t('pricing:editor.general.rounding')}</TableHead>
                    <TableHead>{t('pricing:editor.general.deposit')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {doc.contract_types.map((c) => (
                    <ContractTypeRow
                      key={c}
                      contractType={c}
                      settings={s}
                      berthFees={berthFees}
                      readOnly={readOnly}
                      lang={i18n.language}
                      onChange={setSettings}
                    />
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('pricing:editor.general.dimensions')}</CardTitle>
          <CardDescription>{t('pricing:editor.general.dimensionsHint')}</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <div className="grid gap-4 sm:grid-cols-3">
              <Field>
                <FieldLabel htmlFor="pl-field-dimension_source">
                  {t('pricing:editor.general.dimensionSource')}
                </FieldLabel>
                <Select
                  value={s.dimension_source}
                  disabled={readOnly}
                  onValueChange={(v) => setSettings({ dimension_source: v as never })}
                >
                  <SelectTrigger id="pl-field-dimension_source" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {DIMENSION_SOURCES.map((d) => (
                      <SelectItem key={d} value={d}>
                        {t(`pricingEnums:dimensionSources.${d}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="pl-field-length_step">
                  {t('pricing:editor.general.lengthStep')}
                </FieldLabel>
                <Select
                  value={String(s.length_rounding.step_cm)}
                  disabled={readOnly}
                  onValueChange={(v) =>
                    setSettings({
                      length_rounding: { ...s.length_rounding, step_cm: Number(v) as never },
                    })
                  }
                >
                  <SelectTrigger id="pl-field-length_step" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {LENGTH_STEPS.map((step) => (
                      <SelectItem key={step} value={String(step)}>
                        {t(`pricing:editor.general.steps.s${step}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="pl-field-length_mode">
                  {t('pricing:editor.general.lengthMode')}
                </FieldLabel>
                <Select
                  value={s.length_rounding.mode}
                  disabled={readOnly}
                  onValueChange={(v) =>
                    setSettings({ length_rounding: { ...s.length_rounding, mode: v as never } })
                  }
                >
                  <SelectTrigger id="pl-field-length_mode" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {LENGTH_MODES.map((m) => (
                      <SelectItem key={m} value={m}>
                        {t(`pricingEnums:lengthRoundingModes.${m}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
            <Field orientation="horizontal">
              <Switch
                id="pl-field-min_beam"
                checked={s.min_beam_ratio != null}
                disabled={readOnly}
                onCheckedChange={(on) =>
                  setSettings({ min_beam_ratio: on ? { num: 1, den: 3 } : null })
                }
              />
              <FieldLabel htmlFor="pl-field-min_beam">
                {t('pricing:editor.general.minBeam')}
              </FieldLabel>
            </Field>
            {s.min_beam_ratio ? (
              <div className="grid max-w-sm grid-cols-2 gap-4">
                <Field>
                  <FieldLabel htmlFor="pl-field-min_beam_num">
                    {t('pricing:editor.general.ratioNum')}
                  </FieldLabel>
                  <IntegerInput
                    id="pl-field-min_beam_num"
                    value={s.min_beam_ratio.num}
                    onChange={(n) =>
                      n !== null &&
                      !Number.isNaN(n) &&
                      setSettings({ min_beam_ratio: { ...s.min_beam_ratio!, num: n } })
                    }
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="pl-field-min_beam_den">
                    {t('pricing:editor.general.ratioDen')}
                  </FieldLabel>
                  <IntegerInput
                    id="pl-field-min_beam_den"
                    value={s.min_beam_ratio.den}
                    onChange={(n) =>
                      n !== null &&
                      !Number.isNaN(n) &&
                      n > 0 &&
                      setSettings({ min_beam_ratio: { ...s.min_beam_ratio!, den: n } })
                    }
                  />
                </Field>
                <FieldDescription className="col-span-2">
                  {t('pricing:editor.general.minBeamHint')}
                </FieldDescription>
              </div>
            ) : null}
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('pricing:editor.general.seasonWindow')}</CardTitle>
          <CardDescription>
            {t(
              needsWindow
                ? 'pricing:editor.general.seasonWindowRequired'
                : 'pricing:editor.general.seasonWindowHint',
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="pl-field-season_from">
                {t('pricing:editor.general.windowFrom')}
              </FieldLabel>
              <DatePicker
                id="pl-field-season_from"
                value={s.season_window?.from ?? null}
                disabled={readOnly}
                onChange={(d) =>
                  setSettings({
                    season_window: d ? { from: d, to: s.season_window?.to ?? d } : null,
                  })
                }
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="pl-field-season_to">
                {t('pricing:editor.general.windowTo')}
              </FieldLabel>
              <DatePicker
                id="pl-field-season_to"
                value={s.season_window?.to ?? null}
                disabled={readOnly || !s.season_window}
                onChange={(d) =>
                  d &&
                  s.season_window &&
                  setSettings({ season_window: { ...s.season_window, to: d } })
                }
              />
            </Field>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="pt-6">
          <Field orientation="horizontal">
            <Switch
              id="pl-field-publish_matrix"
              checked={s.publish_matrix === true}
              disabled={readOnly}
              onCheckedChange={(on) => setSettings({ publish_matrix: on })}
            />
            <FieldLabel htmlFor="pl-field-publish_matrix">
              {t('pricing:editor.general.publishMatrix')}
            </FieldLabel>
          </Field>
          <FieldDescription>{t('pricing:editor.general.publishMatrixHint')}</FieldDescription>
        </CardContent>
      </Card>
    </fieldset>
  );
}

function ContractTypeRow({
  contractType: c,
  settings,
  berthFees: fees,
  readOnly: ro,
  lang,
  onChange: set,
}: {
  contractType: ContractType;
  settings: PriceListSettings;
  berthFees: Item[];
  readOnly: boolean;
  lang: string;
  onChange: (patch: Partial<PriceListSettings>) => void;
}) {
  const { t } = useT();
  const dep = settings.deposits?.[c];
  const kind = depositKind(dep);
  const label = t(`pricingEnums:contractTypes.${c}`);
  return (
    <TableRow>
      <TableCell className="font-medium">{label}</TableCell>
      <TableCell className="min-w-56">
        <Select
          value={settings.primary_items?.[c] ?? NONE}
          disabled={ro}
          onValueChange={(v) =>
            set({ primary_items: setKey(settings.primary_items, c, v === NONE ? undefined : v) })
          }
        >
          <SelectTrigger
            id={`pl-field-primary_items-${c}`}
            className="w-full"
            aria-label={t('pricing:editor.general.primaryItemFor', { type: label })}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>{t('pricing:editor.general.noPrimaryItem')}</SelectItem>
            {fees.map((i) => (
              <SelectItem key={i.id} value={i.id}>
                {i.code} · {localText(i.name, lang)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TableCell>
      <TableCell className="min-w-44">
        <Select
          value={settings.split?.[c] ?? INHERIT}
          disabled={ro}
          onValueChange={(v) =>
            set({ split: setKey(settings.split, c, v === INHERIT ? undefined : (v as never)) })
          }
        >
          <SelectTrigger
            className="w-full"
            aria-label={t('pricing:editor.general.splitFor', { type: label })}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={INHERIT}>{t('pricing:editor.general.inherit')}</SelectItem>
            {SPLIT_RULES.map((r) => (
              <SelectItem key={r} value={r}>
                {t(`pricingEnums:splitRules.${r}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TableCell>
      <TableCell className="min-w-40">
        <Select
          value={settings.rounding?.[c] ?? INHERIT}
          disabled={ro}
          onValueChange={(v) =>
            set({
              rounding: setKey(settings.rounding, c, v === INHERIT ? undefined : (v as never)),
            })
          }
        >
          <SelectTrigger
            className="w-full"
            aria-label={t('pricing:editor.general.roundingFor', { type: label })}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={INHERIT}>{t('pricing:editor.general.inherit')}</SelectItem>
            {TOTAL_ROUNDINGS.map((r) => (
              <SelectItem key={r} value={r}>
                {t(`pricingEnums:totalRoundings.${r}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TableCell>
      <TableCell className="min-w-64">
        <div className="flex gap-2">
          <Select
            value={kind}
            disabled={ro}
            onValueChange={(v) =>
              set({
                deposits: setKey(
                  settings.deposits,
                  c,
                  v === 'percent'
                    ? { percent_bp: 3000 }
                    : v === 'amount'
                      ? { amount_cents: 0 }
                      : undefined,
                ),
              })
            }
          >
            <SelectTrigger
              className="w-32"
              aria-label={t('pricing:editor.general.depositFor', { type: label })}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">{t('pricing:editor.general.depositNone')}</SelectItem>
              <SelectItem value="percent">{t('pricing:editor.general.depositPercent')}</SelectItem>
              <SelectItem value="amount">{t('pricing:editor.general.depositAmount')}</SelectItem>
            </SelectContent>
          </Select>
          {kind === 'percent' ? (
            <PercentInput
              value={dep?.percent_bp ?? null}
              locale={lang}
              aria-label={t('pricing:editor.general.depositPercentFor', { type: label })}
              onChange={(bp) =>
                bp !== null &&
                bp >= 0 &&
                bp <= 10_000 &&
                set({ deposits: setKey(settings.deposits, c, { percent_bp: bp }) })
              }
            />
          ) : kind === 'amount' ? (
            <MoneyInput
              value={dep?.amount_cents ?? null}
              locale={lang}
              aria-label={t('pricing:editor.general.depositAmountFor', { type: label })}
              onChange={(cents) =>
                cents !== null &&
                cents >= 0 &&
                set({ deposits: setKey(settings.deposits, c, { amount_cents: cents }) })
              }
            />
          ) : null}
        </div>
      </TableCell>
    </TableRow>
  );
}
