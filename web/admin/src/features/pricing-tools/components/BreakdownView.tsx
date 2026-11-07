import { Link } from '@tanstack/react-router';
import { TriangleAlertIcon } from 'lucide-react';
import { Fragment } from 'react';

import type { Item, PriceBreakdown, PriceRule, Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp, formatCm, formatIsoDate, formatQtyMilli } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { localName } from '../../maps/lib';
import { formatTraceParam, money, numberLocale } from '../lib/format';

type Line = Schemas['PriceLine'];
type Step = Schemas['PriceTraceStep'];

export type BreakdownViewProps = {
  breakdown: PriceBreakdown;
  marinaId: string;
  /** Items of the marina, for the line names (codes are shown otherwise). */
  items?: readonly Item[];
  /** Rules of the priced list, for the rule names (ids are shown otherwise). */
  rules?: readonly PriceRule[];
};

/** Full price breakdown from the server: lines, blocks, discounts, rounding, VAT, deposit and trace. */
export function BreakdownView({ breakdown, marinaId, items = [], rules = [] }: BreakdownViewProps) {
  const { t, i18n } = useT();
  const lang = i18n.language;
  const priceListId = breakdown.price_list_id;
  const itemName = (id: string, code: string) => {
    const item = items.find((i) => i.id === id);
    return item ? `${code} · ${localName(item.name, lang)}` : code;
  };
  const ruleName = (id: string) => rules.find((r) => r.id === id)?.name ?? id;
  const ruleLink = (id: string) =>
    id ? (
      <Link
        to="/m/$marinaId/price-lists/$priceListId"
        params={{ marinaId, priceListId }}
        hash={`rule-${id}`}
        className="underline underline-offset-4"
      >
        {ruleName(id)}
      </Link>
    ) : null;

  return (
    <div className="flex flex-col gap-4" data-testid="breakdown">
      <TotalsCard breakdown={breakdown} />
      {breakdown.warnings.length > 0 ? (
        <Alert>
          <TriangleAlertIcon />
          <AlertTitle>{t('pricingTools:breakdown.warnings')}</AlertTitle>
          <AlertDescription>
            <ul className="list-disc pl-4">
              {breakdown.warnings.map((w) => (
                <li key={w}>{t(`pricingEnums:warnings.${w}`, { defaultValue: w })}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      ) : null}
      {breakdown.resolved ? <ResolvedCard resolved={breakdown.resolved} /> : null}

      <Card>
        <CardHeader>
          <CardTitle>{t('pricingTools:breakdown.lines')}</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('pricingTools:breakdown.item')}</TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.qty')}</TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.base')}</TableHead>
                <TableHead className="text-right">
                  {t('pricingTools:breakdown.modifiers')}
                </TableHead>
                <TableHead className="text-right">
                  {t('pricingTools:breakdown.discounts')}
                </TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.gross')}</TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.net')}</TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.vatRate')}</TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.vat')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {breakdown.lines.map((line) => (
                <TableRow key={line.seq} data-testid={`line-${line.seq}`}>
                  <TableCell>
                    <div className="flex flex-col">
                      <span className="flex items-center gap-2">
                        {itemName(line.item_id, line.item_code)}
                        <Badge variant={line.kind === 'included' ? 'secondary' : 'outline'}>
                          {t(`pricingEnums:lineKinds.${line.kind}`)}
                        </Badge>
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {formatIsoDate(line.service_from)} – {formatIsoDate(line.service_to)}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatQtyMilli(line.qty_milli, numberLocale(lang))}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(line.base_cents, lang)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(line.modifier_total_cents, lang)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(discountTotal(line), lang)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(line.gross_cents, lang)}
                    {line.rounding_share_cents !== 0 ? (
                      <span className="block text-xs text-muted-foreground">
                        {t('pricingTools:breakdown.rounding')}{' '}
                        {money(line.rounding_share_cents, lang)}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(line.net_cents, lang)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {line.vat_nature
                      ? line.vat_nature
                      : formatBp(line.vat_rate_bp, numberLocale(lang))}
                    {line.price_includes_vat ? (
                      <span className="block text-xs text-muted-foreground">
                        {t('pricingTools:breakdown.vatIncluded')}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(line.vat_cents, lang)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {breakdown.lines.some((l) => l.blocks.length > 0) ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('pricingTools:breakdown.blocks')}</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('pricingTools:breakdown.from')}</TableHead>
                  <TableHead>{t('pricingTools:breakdown.to')}</TableHead>
                  <TableHead>{t('pricingTools:breakdown.unit')}</TableHead>
                  <TableHead className="text-right">{t('pricingTools:breakdown.nights')}</TableHead>
                  <TableHead>{t('pricingTools:breakdown.period')}</TableHead>
                  <TableHead>{t('pricingTools:breakdown.rule')}</TableHead>
                  <TableHead className="text-right">{t('pricingTools:breakdown.base')}</TableHead>
                  <TableHead>{t('pricingTools:breakdown.modifiers')}</TableHead>
                  <TableHead className="text-right">{t('pricingTools:breakdown.amount')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {breakdown.lines.map((line) =>
                  line.blocks.map((b, i) => (
                    <TableRow key={`${line.seq}-${i}`} data-testid={`block-${line.seq}-${i}`}>
                      <TableCell>{formatIsoDate(b.from)}</TableCell>
                      <TableCell>{formatIsoDate(b.to)}</TableCell>
                      <TableCell>{t(`pricingEnums:timeUnits.${b.unit}`)}</TableCell>
                      <TableCell className="text-right tabular-nums">{b.nights}</TableCell>
                      <TableCell>{b.period ?? ''}</TableCell>
                      <TableCell>{ruleLink(b.rule_id)}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {money(b.base_cents, lang)}
                      </TableCell>
                      <TableCell>
                        <ul className="flex flex-col gap-1">
                          {b.modifiers.map((m) => (
                            <li key={m.rule_id}>
                              {ruleLink(m.rule_id)} {money(m.delta_cents, lang)}
                            </li>
                          ))}
                        </ul>
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {money(b.amount_cents, lang)}
                      </TableCell>
                    </TableRow>
                  )),
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      ) : null}

      {breakdown.lines.some((l) => l.discounts.length > 0) ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('pricingTools:breakdown.discountsTitle')}</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="flex flex-col gap-1" data-testid="breakdown-discounts">
              {breakdown.lines.flatMap((line) =>
                line.discounts.map((d) => (
                  <li key={`${line.seq}-${d.rule_id}`} className="flex justify-between gap-4">
                    <span>
                      {line.item_code}: {ruleLink(d.rule_id)}
                    </span>
                    <span className="tabular-nums">{money(d.delta_cents, lang)}</span>
                  </li>
                )),
              )}
            </ul>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>{t('pricingTools:breakdown.vatSummary')}</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('pricingTools:breakdown.vatRate')}</TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.net')}</TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.vat')}</TableHead>
                <TableHead className="text-right">{t('pricingTools:breakdown.gross')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {breakdown.vat_summary.map((v) => (
                <TableRow key={`${v.vat_rate_bp}-${v.vat_nature ?? ''}`}>
                  <TableCell>
                    {formatBp(v.vat_rate_bp, numberLocale(lang))}
                    {v.vat_nature ? ` (${v.vat_nature})` : ''}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(v.net_cents, lang)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(v.vat_cents, lang)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {money(v.gross_cents, lang)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
            <TableFooter>
              <TableRow>
                <TableCell>{t('pricingTools:breakdown.total')}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {money(breakdown.net_cents, lang)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {money(breakdown.vat_cents, lang)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {money(breakdown.total_cents, lang)}
                </TableCell>
              </TableRow>
            </TableFooter>
          </Table>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('pricingTools:breakdown.trace')}</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="flex flex-col gap-2" data-testid="breakdown-trace">
            {breakdown.trace.map((step, i) => (
              <TraceItem
                key={i}
                step={step}
                ruleName={ruleName}
                itemCode={(id) => items.find((it) => it.id === id)?.code ?? id}
              />
            ))}
          </ol>
        </CardContent>
      </Card>
    </div>
  );
}

function discountTotal(line: Line): number {
  let sum = 0;
  for (const d of line.discounts) sum += d.delta_cents;
  return sum;
}

function TotalsCard({ breakdown }: { breakdown: PriceBreakdown }) {
  const { t, i18n } = useT();
  const lang = i18n.language;
  const rows: { key: string; label: string; value: string; testId?: string }[] = [
    {
      key: 'subtotal',
      label: t('pricingTools:breakdown.subtotal'),
      value: money(breakdown.subtotal_cents, lang),
    },
    {
      key: 'discounts',
      label: t('pricingTools:breakdown.discountTotal'),
      value: money(breakdown.discount_total_cents, lang),
    },
    {
      key: 'rounding',
      label: t('pricingTools:breakdown.roundingTotal'),
      value: money(breakdown.rounding_total_cents, lang),
    },
    { key: 'net', label: t('pricingTools:breakdown.net'), value: money(breakdown.net_cents, lang) },
    { key: 'vat', label: t('pricingTools:breakdown.vat'), value: money(breakdown.vat_cents, lang) },
    {
      key: 'deposit',
      label: t('pricingTools:breakdown.deposit'),
      value: money(breakdown.deposit_cents, lang),
      testId: 'breakdown-deposit',
    },
  ];
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-baseline justify-between gap-2">
          <span>{t('pricingTools:breakdown.total')}</span>
          <span className="text-2xl tabular-nums" data-testid="breakdown-total">
            {money(breakdown.total_cents, lang)}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-4">
          <dt className="text-muted-foreground">{t('pricingTools:breakdown.nights')}</dt>
          <dd className="tabular-nums" data-testid="breakdown-nights">
            {breakdown.nights}
          </dd>
          <dt className="text-muted-foreground">{t('pricingTools:breakdown.contractType')}</dt>
          <dd>{t(`pricingEnums:contractTypes.${breakdown.contract_type}`)}</dd>
          {rows.map((r) => (
            <Fragment key={r.key}>
              <dt className="text-muted-foreground">{r.label}</dt>
              <dd className="tabular-nums" data-testid={r.testId}>
                {r.value}
              </dd>
            </Fragment>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

function ResolvedCard({ resolved }: { resolved: Schemas['PriceResolved'] }) {
  const { t, i18n } = useT();
  const locale = numberLocale(i18n.language);
  const cm = (v: number | undefined) => (v === undefined ? '' : formatCm(v, locale));
  const entries: [string, string][] = [
    [t('pricingTools:resolved.priceList'), resolved.price_list_code],
    [t('pricingTools:resolved.loa'), cm(resolved.loa_cm)],
    [t('pricingTools:resolved.beam'), cm(resolved.beam_cm)],
    [
      t('pricingTools:resolved.hullType'),
      resolved.hull_type ? t(`pricingEnums:hullTypes.${resolved.hull_type}`) : '',
    ],
    [
      t('pricingTools:resolved.dimensionOrigin'),
      t(`pricingTools:dimensionOrigins.${resolved.dimension_origin}`),
    ],
    [t('pricingTools:resolved.category'), resolved.category ?? ''],
    [t('pricingTools:resolved.berthLength'), cm(resolved.berth_length_cm)],
    [t('pricingTools:resolved.berthWidth'), cm(resolved.berth_width_cm)],
    [
      t('pricingTools:resolved.customerCategory'),
      resolved.customer_category
        ? t(`pricingEnums:customerCategories.${resolved.customer_category}`)
        : '',
    ],
    [t('pricingTools:resolved.channel'), t(`pricingEnums:channels.${resolved.channel}`)],
    [t('pricingTools:resolved.quotedOn'), formatIsoDate(resolved.quoted_on)],
  ];
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('pricingTools:resolved.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <dl
          className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-4"
          data-testid="breakdown-resolved"
        >
          {entries
            .filter(([, v]) => v !== '')
            .map(([k, v]) => (
              <Fragment key={k}>
                <dt className="text-muted-foreground">{k}</dt>
                <dd>{v}</dd>
              </Fragment>
            ))}
        </dl>
      </CardContent>
    </Card>
  );
}

function TraceItem({
  step,
  ruleName,
  itemCode,
}: {
  step: Step;
  ruleName: (id: string) => string;
  itemCode: (id: string) => string;
}) {
  const { t, i18n } = useT();
  const params = Object.entries(step.params ?? {});
  const value = (k: string, v: string) => {
    if (k === 'item_id') return itemCode(v);
    if (k === 'rule_id') return ruleName(v);
    const ns = enumNamespace(step.code, k);
    if (ns) return t(`${ns}.${v}`, { defaultValue: v });
    return formatTraceParam(k, v, i18n.language);
  };
  return (
    <li className="flex flex-col gap-0.5 text-sm" data-code={step.code}>
      <span className="font-medium">
        {t(`pricingEnums:trace.${step.code}`, { defaultValue: step.code })}
        {step.rule_id ? <span className="font-normal"> · {ruleName(step.rule_id)}</span> : null}
      </span>
      {params.length > 0 ? (
        <span className="text-muted-foreground">
          {params
            .map(
              ([k, v]) =>
                `${t(`pricingTools:traceParams.${k}`, { defaultValue: k })}: ${value(k, v)}`,
            )
            .join(' · ')}
        </span>
      ) : null}
    </li>
  );
}

/** Enum-valued trace params are shown through their label (the namespace depends on the step). */
function enumNamespace(code: string, key: string): string | null {
  const prefix = (ns: string, group: string) => `${ns}:${group}`;
  if (key === 'unit') return prefix('pricingEnums', 'timeUnits');
  if (key === 'kind') return prefix('pricingEnums', 'discountKinds');
  if (key === 'hull_type') return prefix('pricingEnums', 'hullTypes');
  if (key === 'mode')
    return code === 'dimensions.rounded'
      ? prefix('pricingEnums', 'lengthRoundingModes')
      : prefix('pricingEnums', 'totalRoundings');
  if (key === 'source')
    return code === 'item.resolved'
      ? prefix('pricingTools', 'itemSources')
      : prefix('pricingEnums', 'dimensionSources');
  if (key === 'stackable') return prefix('pricingTools', 'booleans');
  return null;
}
