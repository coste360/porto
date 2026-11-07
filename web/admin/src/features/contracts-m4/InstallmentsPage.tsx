import { Link } from '@tanstack/react-router';
import { DownloadIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  useInstallments,
  useInstallmentsCsv,
  useIssueInstallments,
  useRemindInstallments,
  useSettleInstallments,
  type Installment,
  type InstallmentFilters,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
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
import { CustomerField, type CustomerChoice } from '../desk/components/CustomerField';
import { useMarinaToday } from '../registry/lib/today';
import { EnumSelect, IsoDate, LoadMore, M4ProblemAlert, Money, PageFrame } from './components/kit';
import { saveBlob } from './lib/files';
import { SettleDialog } from './components/SettleDialog';
import {
  INSTALLMENT_FILTER_STATUSES,
  ageing,
  bulkActions,
  installmentStatusKey,
  installmentVariant,
  type AgeingBucket,
} from './lib/installments';

const BUCKETS: readonly AgeingBucket[] = ['d0_30', 'd31_60', 'd61_90', 'd90plus'];

/** Scadenzario rate (spec 13.7): filters, overdue ageing, bulk issue/remind/settle and the CSV export. */
export function InstallmentsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [filters, setFilters] = useState<InstallmentFilters>({ status: 'issued' });
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const effective: InstallmentFilters = {
    ...filters,
    ...(customer ? { customer_id: customer.id } : {}),
  };
  const list = useInstallments(marinaId, effective);
  const csv = useInstallmentsCsv(marinaId);
  const issue = useIssueInstallments(marinaId);
  const remind = useRemindInstallments(marinaId);
  const settle = useSettleInstallments(marinaId);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [settling, setSettling] = useState(false);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const chosen = items.filter((i) => selected.has(i.id));
  const acts = bulkActions(chosen);
  const ages = ageing(items, today);
  const totals = list.data?.pages[0]?.totals;
  const toggle = (id: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const done = (msg: string) => {
    setSelected(new Set());
    toast.success(msg);
  };
  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageFrame>
        <PageHeader
          title={t('contractsM4:installments.title')}
          description={t('contractsM4:installments.description')}
          actions={
            <Button
              variant="outline"
              disabled={csv.isPending}
              onClick={() =>
                csv.mutate(effective, {
                  onSuccess: (b) => saveBlob(`scadenzario-${today}.csv`, b),
                })
              }
            >
              <DownloadIcon />
              {t('contractsM4:installments.csv')}
            </Button>
          }
        />
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="inst-status"
            label={t('contractsM4:installments.status')}
            values={INSTALLMENT_FILTER_STATUSES}
            labelOf={(s) => t(`docsEnums:installmentStatus.${s}`)}
            value={filters.status ?? ''}
            onChange={(status) => setFilters({ ...filters, status: status || undefined })}
            allLabel={t('contractsM4:common.all')}
          />
          <Field className="w-auto">
            <FieldLabel htmlFor="inst-from">{t('contractsM4:installments.from')}</FieldLabel>
            <DatePicker
              id="inst-from"
              value={filters.from ?? null}
              onChange={(from) => setFilters({ ...filters, from: from ?? undefined })}
            />
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="inst-to">{t('contractsM4:installments.to')}</FieldLabel>
            <DatePicker
              id="inst-to"
              value={filters.to ?? null}
              onChange={(to) => setFilters({ ...filters, to: to ?? undefined })}
            />
          </Field>
          <Field className="w-64">
            <FieldLabel htmlFor="inst-customer">
              {t('contractsM4:installments.customer')}
            </FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="inst-customer"
              value={customer}
              onChange={setCustomer}
            />
          </Field>
        </div>
        <section
          aria-label={t('contractsM4:installments.ageing')}
          className="grid gap-2 sm:grid-cols-4"
        >
          {BUCKETS.map((b) => (
            <div key={b} className="rounded-md border p-3" data-testid={`ageing-${b}`}>
              <div className="text-muted-foreground text-xs">
                {t(`contractsM4:installments.buckets.${b}`)}
              </div>
              <div className="text-lg font-semibold">
                <Money cents={ages[b].cents} />
              </div>
              <div className="text-muted-foreground text-xs">
                {t('contractsM4:installments.count', { count: ages[b].count })}
              </div>
            </div>
          ))}
        </section>
        {totals ? (
          <p className="text-sm" data-testid="installments-totals">
            {t('contractsM4:installments.totals', { count: totals.count })}{' '}
            <Money cents={totals.amount_cents} />
          </p>
        ) : null}
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-muted-foreground text-sm">
            {t('contractsM4:installments.selected', { count: chosen.length })}
          </span>
          {can('contracts.write') ? (
            <>
              <Button
                size="sm"
                variant="outline"
                disabled={!acts.issue || issue.isPending}
                onClick={() =>
                  issue.mutate(
                    chosen.map((i) => i.id),
                    {
                      onSuccess: (r) =>
                        done(t('contractsM4:installments.issued', { count: r.issued })),
                    },
                  )
                }
              >
                {t('contractsM4:installments.issue')}
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={!acts.remind || remind.isPending}
                onClick={() =>
                  remind.mutate(
                    chosen.map((i) => i.id),
                    {
                      onSuccess: (r) =>
                        done(t('contractsM4:installments.reminded', { count: r.queued })),
                    },
                  )
                }
              >
                {t('contractsM4:installments.remind')}
              </Button>
            </>
          ) : null}
          {can('payments.collect') ? (
            <Button size="sm" disabled={!acts.settle} onClick={() => setSettling(true)}>
              {t('contractsM4:settle.open')}
            </Button>
          ) : null}
        </div>
        <M4ProblemAlert error={issue.error ?? remind.error} />
        {list.isPending ? (
          <LoadingState rows={6} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <InstallmentTable
            marinaId={marinaId}
            items={items}
            selected={selected}
            onToggle={toggle}
            onToggleAll={(all) => setSelected(all ? new Set(items.map((i) => i.id)) : new Set())}
          />
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
                  done(t('contractsM4:installments.settled', { count: r.settled }));
                },
              })
            }
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

function InstallmentTable({
  marinaId,
  items,
  selected,
  onToggle,
  onToggleAll,
}: {
  marinaId: string;
  items: Installment[];
  selected: ReadonlySet<string>;
  onToggle: (id: string) => void;
  onToggleAll: (all: boolean) => void;
}) {
  const { t } = useT();
  const all = items.length > 0 && items.every((i) => selected.has(i.id));
  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>
              <Checkbox
                aria-label={t('contractsM4:installments.selectAll')}
                checked={all}
                onCheckedChange={(c) => onToggleAll(c === true)}
              />
            </TableHead>
            <TableHead>{t('contractsM4:installments.contract')}</TableHead>
            <TableHead>{t('contractsM4:installments.customer')}</TableHead>
            <TableHead>{t('contractsM4:plan.kind')}</TableHead>
            <TableHead>{t('contractsM4:plan.dueOn')}</TableHead>
            <TableHead className="text-right">{t('contractsM4:plan.amount')}</TableHead>
            <TableHead>{t('contractsM4:plan.status')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.length === 0 ? (
            <TableRow>
              <TableCell colSpan={7} className="text-muted-foreground h-24 text-center">
                {t('contractsM4:installments.empty')}
              </TableCell>
            </TableRow>
          ) : (
            items.map((i) => (
              <TableRow key={i.id} data-testid="installment-row">
                <TableCell>
                  <Checkbox
                    aria-label={t('contractsM4:installments.select', {
                      seq: i.seq,
                      code: i.contract_code ?? '',
                    })}
                    checked={selected.has(i.id)}
                    onCheckedChange={() => onToggle(i.id)}
                  />
                </TableCell>
                <TableCell>
                  <Link
                    to="/m/$marinaId/contracts/$contractId"
                    params={{ marinaId, contractId: i.contract_id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {i.contract_code ?? i.contract_id}
                  </Link>
                </TableCell>
                <TableCell>{i.customer?.display_name ?? ''}</TableCell>
                <TableCell>{`${i.seq} · ${t(`docsEnums:installmentKind.${i.kind}`)}`}</TableCell>
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
  );
}
