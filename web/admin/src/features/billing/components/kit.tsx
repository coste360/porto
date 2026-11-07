// Building blocks of the M5 billing pages: the M4 kit (money and date cells, sections, reason dialog, load more,
// enum select) plus badges over the billingEnums namespace, file downloads and small labelled values.
import type { ReactNode } from 'react';

import { useT } from '@porto/shared/i18n';
import { formatBp } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { Money } from '../../contracts-m4/components/kit';
import type { BadgeVariant } from '../lib/charges';

export {
  Detail,
  DetailGrid,
  EnumSelect,
  IsoDate,
  LoadMore,
  M4ProblemAlert as BillingProblemAlert,
  Money,
  PageFrame,
  ReasonDialog,
  Section,
  StepBar,
} from '../../contracts-m4/components/kit';

/** Badge of an M5 enum value: `billingEnums:<group>.<value>`. */
export function BillingBadge({
  group,
  value,
  variant = 'secondary',
}: {
  group: string;
  value: string;
  variant?: BadgeVariant;
}) {
  const { t } = useT();
  return <Badge variant={variant}>{t(`billingEnums:${group}.${value}`)}</Badge>;
}

/** A VAT rate in percent ("22 %"), or the nature code at rate 0. */
export function VatRate({ bp, nature }: { bp: number; nature?: string | null | undefined }) {
  const { i18n } = useT();
  if (bp === 0 && nature) return <span>{nature}</span>;
  return <span className="tabular-nums">{`${formatBp(bp, i18n.language)} %`}</span>;
}

/** A summary tile (label, value, optional hint). */
export function Stat({
  label,
  children,
  testId,
}: {
  label: string;
  children: ReactNode;
  testId?: string;
}) {
  return (
    <div className="rounded-md border p-3" {...(testId ? { 'data-testid': testId } : {})}>
      <div className="text-muted-foreground text-xs">{label}</div>
      <div className="text-lg font-semibold">{children}</div>
    </div>
  );
}

/** A plain table with a header row and an empty message. */
export function SimpleTable({
  head,
  empty,
  children,
  rows,
}: {
  head: ReactNode[];
  empty: string;
  rows: number;
  children: ReactNode;
}) {
  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            {head.map((h, i) => (
              <TableHead key={i}>{h}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows === 0 ? (
            <TableRow>
              <TableCell colSpan={head.length} className="text-muted-foreground h-20 text-center">
                {empty}
              </TableCell>
            </TableRow>
          ) : (
            children
          )}
        </TableBody>
      </Table>
    </div>
  );
}

/** VAT summary rows (rate, net, VAT, gross). */
export function VatTable({
  buckets,
}: {
  buckets: readonly {
    vat_rate_bp: number;
    vat_nature?: string | null;
    net_cents: number;
    vat_cents: number;
    gross_cents: number;
  }[];
}) {
  const { t } = useT();
  return (
    <SimpleTable
      head={[
        t('charges:vat.rate'),
        t('charges:vat.net'),
        t('charges:vat.vat'),
        t('charges:vat.gross'),
      ]}
      empty={t('charges:vat.empty')}
      rows={buckets.length}
    >
      {buckets.map((b) => (
        <TableRow key={`${b.vat_rate_bp}-${b.vat_nature ?? ''}`} data-testid="vat-row">
          <TableCell>
            <VatRate bp={b.vat_rate_bp} nature={b.vat_nature} />
          </TableCell>
          <TableCell>
            <Money cents={b.net_cents} />
          </TableCell>
          <TableCell>
            <Money cents={b.vat_cents} />
          </TableCell>
          <TableCell>
            <Money cents={b.gross_cents} />
          </TableCell>
        </TableRow>
      ))}
    </SimpleTable>
  );
}
