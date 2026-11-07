import { Link } from '@tanstack/react-router';
import { DownloadIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  useBulkPaymentRequests,
  useReceivables,
  useReceivablesAgeing,
  useReceivablesFile,
  useSendReminders,
  type AgeingAmounts,
  type AgeingBucket,
  type ExportFormat,
  type ReceivableFilters,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { Progress } from '@porto/shared/ui/progress';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { CustomerField, type CustomerChoice } from '../../desk/components/CustomerField';
import { useMarinaToday } from '../../registry/lib/today';
import {
  BillingBadge,
  BillingProblemAlert,
  EnumSelect,
  IsoDate,
  LoadMore,
  Money,
  PageFrame,
  SimpleTable,
  Stat,
} from '../components/kit';
import { BUCKETS, bucketCents } from '../lib/catalog';
import { saveBlob } from '../lib/files';

const STATUSES = ['open', 'overdue', 'scheduled'] as const;

/** Ageing tiles with a proportional bar per bucket (plain progress bars, no chart). */
export function AgeingBar({ amounts }: { amounts: AgeingAmounts }) {
  const { t } = useT();
  const total = amounts.total_cents;
  return (
    <section aria-label={t('receivables:ageing.title')} className="grid gap-2 sm:grid-cols-5">
      {BUCKETS.map((b) => {
        const cents = bucketCents(amounts, b);
        const pct = total > 0 ? Math.round((cents * 100) / total) : 0;
        return (
          <div key={b} className="flex flex-col gap-1" data-testid={`ageing-${b}`}>
            <Stat label={t(`billingEnums:ageingBucket.${b}`)}>
              <Money cents={cents} />
            </Stat>
            <Progress value={pct} aria-label={t(`billingEnums:ageingBucket.${b}`)} />
          </div>
        );
      })}
    </section>
  );
}

/** Scadenzario (m5-plan §4.5, §9.1): charges and future instalments, bulk request and remind, ageing per customer. */
export function ReceivablesPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <RequirePermission allowed={can('charges.manage')}>
      <PageFrame>
        <PageHeader title={t('receivables:title')} description={t('receivables:description')} />
        <Tabs defaultValue="list">
          <TabsList>
            <TabsTrigger value="list">{t('receivables:tabs.list')}</TabsTrigger>
            <TabsTrigger value="ageing">{t('receivables:tabs.ageing')}</TabsTrigger>
          </TabsList>
          <TabsContent value="list">
            <ReceivablesList />
          </TabsContent>
          <TabsContent value="ageing">
            <AgeingTab />
          </TabsContent>
        </Tabs>
      </PageFrame>
    </RequirePermission>
  );
}

function ReceivablesList() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const [status, setStatus] = useState<(typeof STATUSES)[number] | ''>('open');
  const [bucket, setBucket] = useState<AgeingBucket | ''>('');
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [from, setFrom] = useState<string | null>(null);
  const [to, setTo] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const filters: ReceivableFilters = {
    ...(status ? { status } : {}),
    ...(bucket ? { bucket } : {}),
    ...(customer ? { customer_id: customer.id } : {}),
    ...(from ? { due_from: from } : {}),
    ...(to ? { due_to: to } : {}),
  };
  const list = useReceivables(marinaId, filters);
  const file = useReceivablesFile(marinaId);
  const bulk = useBulkPaymentRequests(marinaId);
  const remind = useSendReminders(marinaId);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const totals = list.data?.pages[0]?.totals;
  const chargeIds = items.filter((i) => i.kind === 'charge' && selected.has(i.id)).map((i) => i.id);
  const selectable = items.filter((i) => i.kind === 'charge');
  const all = selectable.length > 0 && selectable.every((i) => selected.has(i.id));
  const toggle = (id: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const download = (format: ExportFormat) =>
    file.mutate(
      { filters, format },
      { onSuccess: (b) => saveBlob(`scadenzario-${today}.${format}`, b) },
    );
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-3">
        <EnumSelect
          id="rec-status"
          label={t('receivables:fields.status')}
          values={STATUSES}
          labelOf={(s) => t(`receivables:statuses.${s}`)}
          value={status}
          onChange={setStatus}
          allLabel={t('receivables:all')}
        />
        <EnumSelect
          id="rec-bucket"
          label={t('receivables:fields.bucket')}
          values={BUCKETS}
          labelOf={(b) => t(`billingEnums:ageingBucket.${b}`)}
          value={bucket}
          onChange={setBucket}
          allLabel={t('receivables:all')}
        />
        <Field className="w-64">
          <FieldLabel htmlFor="rec-customer">{t('receivables:fields.customer')}</FieldLabel>
          <CustomerField
            marinaId={marinaId}
            id="rec-customer"
            value={customer}
            onChange={setCustomer}
          />
        </Field>
        <Field className="w-auto">
          <FieldLabel htmlFor="rec-from">{t('receivables:fields.dueFrom')}</FieldLabel>
          <DatePicker id="rec-from" value={from} onChange={setFrom} />
        </Field>
        <Field className="w-auto">
          <FieldLabel htmlFor="rec-to">{t('receivables:fields.dueTo')}</FieldLabel>
          <DatePicker id="rec-to" value={to} onChange={setTo} />
        </Field>
        <Button variant="outline" disabled={file.isPending} onClick={() => download('csv')}>
          <DownloadIcon />
          {t('receivables:csv')}
        </Button>
        <Button variant="outline" disabled={file.isPending} onClick={() => download('xlsx')}>
          <DownloadIcon />
          {t('receivables:xlsx')}
        </Button>
      </div>
      {totals ? <AgeingBar amounts={totals} /> : null}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground text-sm">
          {t('receivables:selected', { count: chargeIds.length })}
        </span>
        <Button
          size="sm"
          variant="outline"
          disabled={chargeIds.length === 0 || bulk.isPending}
          onClick={() =>
            bulk.mutate(
              { charge_ids: chargeIds, send: true },
              {
                onSuccess: (r) => {
                  setSelected(new Set());
                  toast.success(
                    t('receivables:bulk.requested', { created: r.created, skipped: r.skipped }),
                  );
                },
              },
            )
          }
        >
          {t('receivables:bulk.request')}
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={chargeIds.length === 0 || remind.isPending}
          onClick={() =>
            remind.mutate(
              { charge_ids: chargeIds },
              {
                onSuccess: (r) => {
                  setSelected(new Set());
                  toast.success(
                    t('receivables:bulk.reminded', { queued: r.queued, skipped: r.skipped }),
                  );
                },
              },
            )
          }
        >
          {t('receivables:bulk.remind')}
        </Button>
      </div>
      <BillingProblemAlert error={file.error ?? bulk.error ?? remind.error} />
      {list.isPending ? (
        <LoadingState rows={6} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <SimpleTable
          head={[
            <Checkbox
              key="all"
              aria-label={t('receivables:selectAll')}
              checked={all}
              onCheckedChange={(c) =>
                setSelected(c === true ? new Set(selectable.map((i) => i.id)) : new Set())
              }
            />,
            t('receivables:fields.number'),
            t('receivables:fields.customer'),
            t('receivables:fields.origin'),
            t('receivables:fields.dueOn'),
            t('receivables:fields.amount'),
            t('receivables:fields.residual'),
            t('receivables:fields.bucket'),
            t('receivables:fields.lastReminder'),
          ]}
          empty={t('receivables:empty')}
          rows={items.length}
        >
          {items.map((r) => (
            <TableRow key={`${r.kind}-${r.id}`} data-testid="receivable-row">
              <TableCell>
                {r.kind === 'charge' ? (
                  <Checkbox
                    aria-label={t('receivables:select', { number: r.number ?? r.id })}
                    checked={selected.has(r.id)}
                    onCheckedChange={() => toggle(r.id)}
                  />
                ) : null}
              </TableCell>
              <TableCell>
                {r.kind === 'charge' ? (
                  <Link
                    to="/m/$marinaId/charges/$chargeId"
                    params={{ marinaId, chargeId: r.id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {r.number}
                  </Link>
                ) : (
                  <Badge variant="outline">
                    {t('billingEnums:receivableKind.scheduled_installment')}
                  </Badge>
                )}
                {r.excluded ? (
                  <Badge variant="outline" className="ml-2">
                    {t('receivables:excluded')}
                  </Badge>
                ) : null}
              </TableCell>
              <TableCell>{r.customer?.display_name ?? ''}</TableCell>
              <TableCell>
                {r.origin_type ? t(`billingEnums:originType.${r.origin_type}`) : ''}
                {r.contract_code ? ` · ${r.contract_code}` : ''}
                {r.description ? (
                  <span className="text-muted-foreground block text-xs">{r.description}</span>
                ) : null}
              </TableCell>
              <TableCell>
                <IsoDate date={r.due_on} />
              </TableCell>
              <TableCell>
                <Money cents={r.amount_cents} />
              </TableCell>
              <TableCell>
                <Money cents={r.residual_cents} />
              </TableCell>
              <TableCell>
                <BillingBadge
                  group="ageingBucket"
                  value={r.bucket}
                  variant={r.bucket === 'not_due' ? 'secondary' : 'destructive'}
                />
              </TableCell>
              <TableCell>
                {r.last_reminder_level
                  ? t(`billingEnums:reminderLevel.${r.last_reminder_level}`)
                  : ''}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      <LoadMore query={list} />
    </div>
  );
}

function AgeingTab() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const ageing = useReceivablesAgeing(marinaId);
  const file = useReceivablesFile(marinaId);
  if (ageing.isPending) return <LoadingState />;
  if (ageing.isError) return <QueryErrorState error={ageing.error} />;
  const r = ageing.data;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-2">
        {(['csv', 'xlsx'] as const).map((format) => (
          <Button
            key={format}
            variant="outline"
            disabled={file.isPending}
            onClick={() =>
              file.mutate(
                { filters: {}, format, ageing: true },
                { onSuccess: (b) => saveBlob(`sospesi-${today}.${format}`, b) },
              )
            }
          >
            <DownloadIcon />
            {t(`receivables:${format}`)}
          </Button>
        ))}
      </div>
      <AgeingBar amounts={r.totals} />
      <SimpleTable
        head={[
          t('receivables:fields.customer'),
          ...BUCKETS.map((b) => t(`billingEnums:ageingBucket.${b}`)),
          t('receivables:ageing.total'),
          t('receivables:ageing.credit'),
        ]}
        empty={t('receivables:ageing.empty')}
        rows={r.rows.length}
      >
        {r.rows.map((row) => (
          <TableRow key={row.customer.id} data-testid="ageing-row">
            <TableCell>
              <Link
                to="/m/$marinaId/customers/$customerId"
                params={{ marinaId, customerId: row.customer.id }}
                className="underline-offset-4 hover:underline"
              >
                {row.customer.display_name ?? row.customer.id}
              </Link>
            </TableCell>
            {BUCKETS.map((b) => (
              <TableCell key={b}>
                <Money cents={bucketCents(row.amounts, b)} />
              </TableCell>
            ))}
            <TableCell className="font-medium">
              <Money cents={row.amounts.total_cents} />
            </TableCell>
            <TableCell>
              <Money cents={row.credit_cents} />
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      <BillingProblemAlert error={file.error} />
    </div>
  );
}
