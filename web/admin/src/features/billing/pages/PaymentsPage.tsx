import { Link } from '@tanstack/react-router';
import { DownloadIcon, PlusIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  usePayments,
  usePaymentsFile,
  type ExportFormat,
  type PaymentFilters,
  type PaymentKind,
  type PaymentMethod,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

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
} from '../components/kit';
import { RecordPaymentDialog } from '../components/RecordPaymentDialog';
import { PAYMENT_METHODS } from '../lib/catalog';
import { saveBlob } from '../lib/files';

const KINDS: readonly PaymentKind[] = ['payment', 'deposit', 'credit'];

/** Payments list (m5-plan §9.1): filters, record payment with matching, CSV/XLSX of payments and refunds. */
export function PaymentsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [method, setMethod] = useState<PaymentMethod | ''>('');
  const [kind, setKind] = useState<PaymentKind | ''>('');
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [from, setFrom] = useState<string | null>(null);
  const [to, setTo] = useState<string | null>(null);
  const [unallocated, setUnallocated] = useState(false);
  const [recording, setRecording] = useState(false);
  const filters: PaymentFilters = {
    ...(method ? { method } : {}),
    ...(kind ? { kind } : {}),
    ...(customer ? { customer_id: customer.id } : {}),
    ...(from ? { from } : {}),
    ...(to ? { to } : {}),
    ...(unallocated ? { unallocated: true } : {}),
  };
  const list = usePayments(marinaId, filters);
  const file = usePaymentsFile(marinaId);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const download = (format: ExportFormat) =>
    file.mutate(
      {
        format,
        ...(from ? { from } : {}),
        ...(to ? { to } : {}),
        ...(method ? { method } : {}),
      },
      { onSuccess: (b) => saveBlob(`pagamenti-${today}.${format}`, b) },
    );
  return (
    <RequirePermission allowed={can('payments.collect') || can('charges.manage')}>
      <PageFrame>
        <PageHeader
          title={t('payments:list.title')}
          description={t('payments:list.description')}
          actions={
            <>
              {can('charges.export') ? (
                <>
                  <Button
                    variant="outline"
                    disabled={file.isPending}
                    onClick={() => download('csv')}
                  >
                    <DownloadIcon />
                    {t('payments:list.csv')}
                  </Button>
                  <Button
                    variant="outline"
                    disabled={file.isPending}
                    onClick={() => download('xlsx')}
                  >
                    <DownloadIcon />
                    {t('payments:list.xlsx')}
                  </Button>
                </>
              ) : null}
              {can('payments.collect') ? (
                <Button onClick={() => setRecording(true)}>
                  <PlusIcon />
                  {t('payments:record.open')}
                </Button>
              ) : null}
            </>
          }
        />
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="payments-method"
            label={t('payments:fields.method')}
            values={PAYMENT_METHODS}
            labelOf={(m) => t(`billingEnums:paymentMethod.${m}`)}
            value={method}
            onChange={setMethod}
            allLabel={t('payments:list.all')}
          />
          <EnumSelect
            id="payments-kind"
            label={t('payments:fields.kind')}
            values={KINDS}
            labelOf={(k) => t(`billingEnums:paymentKind.${k}`)}
            value={kind}
            onChange={setKind}
            allLabel={t('payments:list.all')}
          />
          <Field className="w-64">
            <FieldLabel htmlFor="payments-customer">{t('payments:fields.customer')}</FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="payments-customer"
              value={customer}
              onChange={setCustomer}
            />
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="payments-from">{t('payments:list.from')}</FieldLabel>
            <DatePicker id="payments-from" value={from} onChange={setFrom} />
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="payments-to">{t('payments:list.to')}</FieldLabel>
            <DatePicker id="payments-to" value={to} onChange={setTo} />
          </Field>
          <label className="flex items-center gap-2 pb-2 text-sm">
            <Checkbox checked={unallocated} onCheckedChange={(v) => setUnallocated(v === true)} />
            {t('payments:list.unallocated')}
          </label>
        </div>
        <BillingProblemAlert error={file.error} />
        {list.isPending ? (
          <LoadingState rows={6} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('payments:fields.number'),
              t('payments:fields.receivedOn'),
              t('payments:fields.customer'),
              t('payments:fields.method'),
              t('payments:fields.amount'),
              t('payments:fields.unallocated'),
              t('payments:fields.kind'),
            ]}
            empty={t('payments:list.empty')}
            rows={items.length}
          >
            {items.map((p) => (
              <TableRow key={p.id} data-testid="payment-row">
                <TableCell>
                  <Link
                    to="/m/$marinaId/payments/$paymentId"
                    params={{ marinaId, paymentId: p.id }}
                    className="font-medium underline-offset-4 hover:underline"
                  >
                    {p.number}
                  </Link>
                </TableCell>
                <TableCell>
                  <IsoDate date={p.received_on} />
                </TableCell>
                <TableCell>{p.customer?.display_name ?? ''}</TableCell>
                <TableCell>{t(`billingEnums:paymentMethod.${p.method}`)}</TableCell>
                <TableCell>
                  <Money cents={p.amount_cents} />
                </TableCell>
                <TableCell>
                  <Money cents={p.unallocated_cents} />
                </TableCell>
                <TableCell>
                  <BillingBadge group="paymentKind" value={p.kind} />
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        <LoadMore query={list} />
        {recording ? (
          <RecordPaymentDialog
            onClose={() => setRecording(false)}
            onDone={(p) => toast.success(t('payments:record.doneNumber', { number: p.number }))}
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}
