import { DownloadIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  useHolderInstallments,
  useHolderInstallmentsCsv,
  useSettleHolderInstallments,
  type HolderInstallmentFilters,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { PageHeader } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { EnumSelect, IsoDate, LoadMore, Money, PageFrame } from '../contracts-m4/components/kit';
import { saveBlob } from '../contracts-m4/lib/files';
import { SettleDialog } from '../contracts-m4/public';
import {
  INSTALLMENT_FILTER_STATUSES,
  installmentStatusKey,
  installmentVariant,
} from '../contracts-m4/lib/installments';
import { useMarinaToday } from '../registry/lib/today';

const SOURCES: readonly Schemas['HolderInstallmentSource'][] = [
  'management_fee',
  'common_advance',
  'common_balance',
  'transfer_fee',
];

/** Holder instalments (spec 14.6): the scadenzario of fees, common charges and transfer fees, with settlement. */
export function HolderInstallmentsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [filters, setFilters] = useState<HolderInstallmentFilters>({ status: 'issued' });
  const list = useHolderInstallments(marinaId, filters);
  const csv = useHolderInstallmentsCsv(marinaId);
  const settle = useSettleHolderInstallments(marinaId);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [settling, setSettling] = useState(false);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const chosen = items.filter((i) => selected.has(i.id));
  const settleable =
    chosen.length > 0 && chosen.every((i) => i.status === 'issued' || i.status === 'scheduled');
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        <PageHeader
          title={t('commonCharges:holderInstallments.title')}
          description={t('commonCharges:holderInstallments.description')}
          actions={
            <Button
              variant="outline"
              disabled={csv.isPending}
              onClick={() =>
                csv.mutate(filters, { onSuccess: (b) => saveBlob(`rate-titolari-${today}.csv`, b) })
              }
            >
              <DownloadIcon />
              {t('contractsM4:installments.csv')}
            </Button>
          }
        />
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="hi-status"
            label={t('contractsM4:installments.status')}
            values={INSTALLMENT_FILTER_STATUSES}
            labelOf={(s) => t(`docsEnums:installmentStatus.${s}`)}
            value={filters.status ?? ''}
            onChange={(status) => setFilters({ ...filters, status: status || undefined })}
            allLabel={t('contractsM4:common.all')}
          />
          <EnumSelect
            id="hi-source"
            label={t('commonCharges:holderInstallments.source')}
            group="holderInstallmentSource"
            values={SOURCES}
            value={filters.source_type ?? ''}
            onChange={(source_type) =>
              setFilters({ ...filters, source_type: source_type || undefined })
            }
            allLabel={t('contractsM4:common.all')}
          />
          {can('payments.collect') ? (
            <Button disabled={!settleable} onClick={() => setSettling(true)}>
              {t('contractsM4:settle.open')}
            </Button>
          ) : null}
        </div>
        {list.isPending ? (
          <LoadingState rows={5} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>
                    <Checkbox
                      aria-label={t('contractsM4:installments.selectAll')}
                      checked={items.length > 0 && items.every((i) => selected.has(i.id))}
                      onCheckedChange={(c) =>
                        setSelected(c === true ? new Set(items.map((i) => i.id)) : new Set())
                      }
                    />
                  </TableHead>
                  <TableHead>{t('commonCharges:holderInstallments.holder')}</TableHead>
                  <TableHead>{t('commonCharges:holderInstallments.source')}</TableHead>
                  <TableHead>{t('contractsM4:plan.dueOn')}</TableHead>
                  <TableHead className="text-right">{t('contractsM4:plan.amount')}</TableHead>
                  <TableHead>{t('contractsM4:plan.status')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className="text-muted-foreground h-24 text-center">
                      {t('contractsM4:installments.empty')}
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((i) => (
                    <TableRow key={i.id}>
                      <TableCell>
                        <Checkbox
                          aria-label={t('commonCharges:holderInstallments.select', {
                            name: i.customer.display_name ?? '',
                            seq: i.seq,
                          })}
                          checked={selected.has(i.id)}
                          onCheckedChange={() =>
                            setSelected((s) => {
                              const n = new Set(s);
                              if (n.has(i.id)) n.delete(i.id);
                              else n.add(i.id);
                              return n;
                            })
                          }
                        />
                      </TableCell>
                      <TableCell>{i.customer.display_name ?? ''}</TableCell>
                      <TableCell>
                        {t(`docsEnums:holderInstallmentSource.${i.source_type}`)}
                      </TableCell>
                      <TableCell>
                        <IsoDate date={i.due_on} />
                      </TableCell>
                      <TableCell className="text-right">
                        <Money cents={i.amount_cents} />
                      </TableCell>
                      <TableCell>
                        <Badge variant={installmentVariant(i.status, i.overdue)}>
                          {t(installmentStatusKey(i.status, i.overdue))}
                        </Badge>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        )}
        <LoadMore query={list} />
        {settling ? (
          <SettleDialog
            open
            items={chosen}
            totalCents={chosen.reduce((s, i) => s + i.amount_cents, 0)}
            pending={settle.isPending}
            error={settle.error}
            onClose={() => setSettling(false)}
            onSettle={(body) =>
              settle.mutate(body, {
                onSuccess: (r) => {
                  setSettling(false);
                  setSelected(new Set());
                  toast.success(t('contractsM4:installments.settled', { count: r.settled }));
                },
              })
            }
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}
