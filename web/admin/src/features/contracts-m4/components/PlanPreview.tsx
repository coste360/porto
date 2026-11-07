import { CheckIcon, TriangleAlertIcon } from 'lucide-react';

import type { Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { installmentStatusKey, planBalanced } from '../lib/installments';
import { IsoDate, Money } from './kit';

type Row = Schemas['PlannedInstallment'] &
  Partial<Pick<Schemas['Installment'], 'status' | 'overdue'>>;

/**
 * An instalment plan as computed by the server, with the Σ = total check of INV-31 under the table. The sum shown
 * is the server's `sum_cents`; a mismatch (never expected) is shown as an alert.
 */
export function PlanPreview({
  items,
  totalCents,
  sumCents,
  caption,
  actions,
}: {
  items: readonly Row[];
  totalCents: number;
  sumCents?: number;
  caption?: string;
  actions?: (row: Row) => React.ReactNode;
}) {
  const { t } = useT();
  const sum =
    sumCents ?? items.reduce((s, i) => s + (i.status === 'cancelled' ? 0 : i.amount_cents), 0);
  const balanced = planBalanced({ total_cents: totalCents, sum_cents: sum });
  return (
    <div className="flex flex-col gap-2">
      <div className="rounded-md border">
        <Table>
          {caption ? <caption className="sr-only">{caption}</caption> : null}
          <TableHeader>
            <TableRow>
              <TableHead>{t('contractsM4:plan.seq')}</TableHead>
              <TableHead>{t('contractsM4:plan.kind')}</TableHead>
              <TableHead>{t('contractsM4:plan.issueOn')}</TableHead>
              <TableHead>{t('contractsM4:plan.dueOn')}</TableHead>
              <TableHead className="text-right">{t('contractsM4:plan.amount')}</TableHead>
              {items.some((i) => i.status) ? (
                <TableHead>{t('contractsM4:plan.status')}</TableHead>
              ) : null}
              {actions ? <TableHead /> : null}
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="text-muted-foreground text-center">
                  {t('contractsM4:plan.empty')}
                </TableCell>
              </TableRow>
            ) : (
              items.map((i) => (
                <TableRow key={`${i.seq}-${i.due_on}`} data-status={i.status}>
                  <TableCell>{i.seq}</TableCell>
                  <TableCell>{t(`docsEnums:installmentKind.${i.kind}`)}</TableCell>
                  <TableCell>
                    <IsoDate date={i.issue_on} />
                  </TableCell>
                  <TableCell>
                    <IsoDate date={i.due_on} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money cents={i.amount_cents} />
                  </TableCell>
                  {i.status ? (
                    <TableCell>{t(installmentStatusKey(i.status, i.overdue ?? false))}</TableCell>
                  ) : null}
                  {actions ? <TableCell className="text-right">{actions(i)}</TableCell> : null}
                </TableRow>
              ))
            )}
          </TableBody>
          <TableFooter>
            <TableRow>
              <TableCell colSpan={4}>{t('contractsM4:plan.sum')}</TableCell>
              <TableCell className="text-right" data-testid="plan-sum">
                <Money cents={sum} />
              </TableCell>
              <TableCell colSpan={2} />
            </TableRow>
          </TableFooter>
        </Table>
      </div>
      <p
        className={
          balanced
            ? 'text-muted-foreground flex items-center gap-1 text-sm'
            : 'text-destructive flex items-center gap-1 text-sm'
        }
        role={balanced ? undefined : 'alert'}
      >
        {balanced ? <CheckIcon className="size-4" /> : <TriangleAlertIcon className="size-4" />}
        {balanced ? t('contractsM4:plan.balanced') : t('contractsM4:plan.unbalanced')}{' '}
        <Money cents={totalCents} />
      </p>
    </div>
  );
}
