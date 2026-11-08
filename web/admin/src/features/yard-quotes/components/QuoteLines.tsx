import type {
  YardLineOverride,
  YardQualification,
  YardQuoteLine,
  YardQuotePreview,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatQtyMilli, parseQtyToMilli } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Input } from '@porto/shared/ui/input';
import { MoneyInput } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { SimpleTable, Stat, VatRate, VatTable } from '../../billing/components/kit';
import { Money } from '../../contracts-m4/components/kit';
import { IssueList } from '../../yard-catalog/public';
import { isManual, type OverrideField } from '../lib';

/**
 * Lines of a quote (m10-plan §14.1): computed quantity and price, manual badge, issues. With `onOverride` the
 * quantity, the unit price and the discount are editable (step 4 of the wizard).
 */
export function QuoteLines({
  lines,
  overrides,
  onOverride,
}: {
  lines: readonly YardQuoteLine[];
  overrides?: readonly YardLineOverride[];
  onOverride?: (seq: number, field: OverrideField, value: number | null) => void;
}) {
  const { t, i18n } = useT();
  const loc = i18n.language;
  const ov = (seq: number) => overrides?.find((o) => o.seq === seq);
  return (
    <SimpleTable
      head={[
        '#',
        t('yardQuotes:lines.description'),
        t('yardQuotes:lines.qty'),
        t('yardQuotes:lines.unitPrice'),
        t('yardQuotes:lines.discount'),
        t('yardQuotes:lines.net'),
        t('yardQuotes:lines.vat'),
      ]}
      empty={t('yardQuotes:lines.empty')}
      rows={lines.length}
    >
      {lines.map((l) => (
        <TableRow key={l.seq} data-testid="yard-quote-line">
          <TableCell className="tabular-nums">{l.seq}</TableCell>
          <TableCell>
            <div className="flex flex-wrap items-center gap-2">
              <span>{l.description}</span>
              {isManual(l) ? (
                <Badge variant="outline" data-testid="manual-badge">
                  {t('yardQuotes:lines.manual')}
                </Badge>
              ) : null}
            </div>
            {l.qty_formula ? (
              <div className="text-muted-foreground font-mono text-xs">{l.qty_formula}</div>
            ) : null}
            {l.issues.length > 0 ? (
              <IssueList issues={l.issues} testId={`line-${l.seq}-issues`} />
            ) : null}
          </TableCell>
          <TableCell className="tabular-nums">
            {onOverride ? (
              <Input
                aria-label={t('yardQuotes:lines.qtyOf', { n: l.seq })}
                inputMode="decimal"
                className="w-24"
                defaultValue={formatQtyMilli(ov(l.seq)?.qty_milli ?? l.qty_milli, loc)}
                onBlur={(e) => {
                  const v = parseQtyToMilli(e.target.value, loc);
                  if (v !== null && v !== l.qty_milli) onOverride(l.seq, 'qty_milli', v);
                }}
              />
            ) : (
              `${formatQtyMilli(l.qty_milli, loc)}${l.unit ? ` ${t(`yardQuotes:units.${l.unit}`)}` : ''}`
            )}
          </TableCell>
          <TableCell className="tabular-nums">
            {onOverride ? (
              <MoneyInput
                aria-label={t('yardQuotes:lines.unitOf', { n: l.seq })}
                className="w-28"
                value={ov(l.seq)?.unit_cents ?? l.unit_cents}
                locale={loc}
                onChange={(v) => {
                  if (v !== null && v !== l.unit_cents) onOverride(l.seq, 'unit_cents', v);
                }}
              />
            ) : (
              <Money cents={l.unit_cents} />
            )}
          </TableCell>
          <TableCell className="tabular-nums">
            {onOverride ? (
              <MoneyInput
                aria-label={t('yardQuotes:lines.discountOf', { n: l.seq })}
                className="w-28"
                value={ov(l.seq)?.discount_cents ?? l.discount_cents}
                locale={loc}
                onChange={(v) => onOverride(l.seq, 'discount_cents', v && v > 0 ? v : null)}
              />
            ) : l.discount_cents ? (
              <Money cents={l.discount_cents} />
            ) : null}
          </TableCell>
          <TableCell className="tabular-nums">
            <Money cents={l.net_cents} />
          </TableCell>
          <TableCell>
            <VatRate bp={l.vat_rate_bp} nature={l.vat_nature} />
          </TableCell>
        </TableRow>
      ))}
    </SimpleTable>
  );
}

/** Totals, VAT summary and estimated hours of a quote or preview. */
export function QuoteTotals({
  preview,
  qualifications,
}: {
  preview: Pick<YardQuotePreview, 'totals' | 'estimated_hours'>;
  qualifications: readonly YardQualification[];
}) {
  const { t, i18n } = useT();
  const tot = preview.totals;
  const qualName = (id: string) => qualifications.find((q) => q.id === id)?.name.it ?? id;
  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-4">
        <Stat label={t('yardQuotes:totals.net')} testId="total-net">
          <Money cents={tot.net_cents} />
        </Stat>
        <Stat label={t('yardQuotes:totals.vat')} testId="total-vat">
          <Money cents={tot.vat_cents} />
        </Stat>
        <Stat label={t('yardQuotes:totals.gross')} testId="total-gross">
          <Money cents={tot.gross_cents} />
        </Stat>
        <Stat label={t('yardQuotes:totals.deposit')} testId="total-deposit">
          <Money cents={tot.deposit_amount_cents} />
        </Stat>
      </div>
      <VatTable buckets={tot.vat} />
      {preview.estimated_hours.length > 0 ? (
        <ul className="text-sm" data-testid="estimated-hours">
          {preview.estimated_hours.map((h) => (
            <li key={h.qualification_id} className="tabular-nums">
              {t('yardQuotes:totals.hours', {
                qualification: qualName(h.qualification_id),
                hours: formatQtyMilli(h.hours_milli, i18n.language),
              })}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
