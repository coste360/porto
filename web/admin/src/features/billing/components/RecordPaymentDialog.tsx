import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import { useCharges, useRecordPayment, type Payment, type RecordPayment } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { isQueuedOffline, useOnlineStatus } from '@porto/shared/offline';
import { MoneyInput } from '@porto/shared/money';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker } from '@porto/shared/ui/patterns';
import { OfflineReason } from '@porto/shared/ui/patterns/OfflineReason';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { CustomerField, type CustomerChoice } from '../../desk/components/CustomerField';
import { useMarinaToday } from '../../registry/lib/today';
import { COUNTED_METHODS, type CountedMethod } from '../lib/cash';
import {
  allocatedTotal,
  autoFill,
  creditPreview,
  isValidAllocation,
  setRow,
  toMatches,
  type MatchRow,
} from '../lib/matching';
import { BillingProblemAlert, EnumSelect, IsoDate, Money, SimpleTable } from './kit';
import { toMatchRows } from '../lib/drafts';
import { offlineReasonOf } from '../lib/offline';

/**
 * Matching table (m5-plan §4.2): one row per open charge, amounts typed per row and clamped so a row never
 * exceeds its residual and the rows never exceed the payment; the unallocated rest is shown as credit.
 */
export function MatchingTable({
  rows,
  amountCents,
  alloc,
  onChange,
}: {
  rows: readonly MatchRow[];
  amountCents: number;
  alloc: Readonly<Record<string, number>>;
  onChange: (alloc: Record<string, number>) => void;
}) {
  const { t, i18n } = useT();
  const [rev, setRev] = useState<Record<string, number>>({});
  const credit = creditPreview(alloc, amountCents);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">{t('payments:match.title')}</span>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={() => onChange(autoFill(rows, amountCents))}
        >
          {t('payments:match.autoFill')}
        </Button>
      </div>
      <SimpleTable
        head={[
          t('payments:match.charge'),
          t('payments:match.dueOn'),
          t('payments:match.residual'),
          t('payments:match.amount'),
        ]}
        empty={t('payments:match.empty')}
        rows={rows.length}
      >
        {rows.map((r) => (
          <TableRow key={r.chargeId} data-testid="match-row">
            <TableCell>{r.number}</TableCell>
            <TableCell>
              <IsoDate date={r.dueOn} />
            </TableCell>
            <TableCell>
              <Money cents={r.residualCents} />
            </TableCell>
            <TableCell className="w-36">
              <MoneyInput
                key={`${r.chargeId}-${rev[r.chargeId] ?? 0}`}
                autoFocus={(rev[r.chargeId] ?? 0) > 0}
                aria-label={t('payments:match.amountOf', { number: r.number })}
                locale={i18n.language}
                value={alloc[r.chargeId] ?? 0}
                onValueChange={(v) => {
                  const next = setRow(rows, alloc, amountCents, r.chargeId, v ?? 0);
                  // A clamped amount remounts the field so it shows the limit at once (focus kept).
                  if (v !== null && next[r.chargeId] !== v)
                    setRev((x) => ({ ...x, [r.chargeId]: (x[r.chargeId] ?? 0) + 1 }));
                  onChange(next);
                }}
              />
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      <div className="flex flex-wrap justify-end gap-4 text-sm" data-testid="match-summary">
        <span>
          {t('payments:match.allocated')} <Money cents={allocatedTotal(alloc)} />
        </span>
        <span data-testid="match-credit">
          {t('payments:match.credit')} <Money cents={credit} />
        </span>
      </div>
    </div>
  );
}

const KINDS = ['payment', 'deposit'] as const;

/** Record payment dialog (m5-plan §9.1): customer, method, amount, matching table, credit preview, receipt. */
export function RecordPaymentDialog({
  onClose,
  onDone,
  customer: initialCustomer = null,
  chargeIds,
  amountCents: initialAmount,
  lockCustomer = false,
}: {
  onClose: () => void;
  onDone?: (p: Payment) => void;
  customer?: CustomerChoice;
  chargeIds?: string[];
  amountCents?: number;
  lockCustomer?: boolean;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const record = useRecordPayment(marinaId);
  const [customer, setCustomer] = useState<CustomerChoice>(initialCustomer);
  const [kind, setKind] = useState<(typeof KINDS)[number]>('payment');
  const [method, setMethod] = useState<CountedMethod>('bank_transfer');
  const [amount, setAmount] = useState<number | null>(initialAmount ?? null);
  const [receivedOn, setReceivedOn] = useState<string | null>(today);
  const [reference, setReference] = useState('');
  const [sendReceipt, setSendReceipt] = useState(false);
  const charges = useCharges(marinaId, { customer_id: customer?.id }, !!customer);
  const rows = useMemo(() => {
    const all = toMatchRows(charges.data?.pages.flatMap((p) => p.items) ?? []);
    return chargeIds?.length ? all.filter((r) => chargeIds.includes(r.chargeId)) : all;
  }, [charges.data, chargeIds]);
  const [alloc, setAlloc] = useState<Record<string, number> | null>(null);
  const cents = amount ?? 0;
  // Until the operator edits, the table follows the amount (auto-fill oldest first).
  const shown = alloc ?? autoFill(rows, cents);
  const valid = cents > 0 && isValidAllocation(rows, shown, cents);
  const { online } = useOnlineStatus();
  // Receipt mode of the matched charges (unknown until they are loaded: then online only).
  const matched = rows.filter((r) => (shown[r.chargeId] ?? 0) > 0);
  const receiptMode = charges.data ? matched.some((r) => r.receiptMode === true) : undefined;
  const offlineReason = online
    ? null
    : offlineReasonOf({ kind, method, cents, allocated: allocatedTotal(shown), receiptMode });
  const submit = () => {
    const body: RecordPayment = {
      kind,
      method,
      amount_cents: cents,
      ...(customer ? { customer_id: customer.id } : {}),
      ...(receivedOn ? { received_on: receivedOn } : {}),
      ...(reference.trim() ? { reference: reference.trim() } : {}),
      ...(kind === 'payment' ? { matches: toMatches(rows, shown) } : {}),
      ...(sendReceipt ? { send_receipt: true } : {}),
    };
    record.mutate(
      { ...body, amountLabel: formatCents(cents, 'EUR', i18n.language), receiptMode },
      {
        onSuccess: (p) => {
          onDone?.(p);
          onClose();
        },
        // A queued cash collection (offline, or no answer yet) is sent later by the queue (m6b-plan §4.3).
        onError: (err) => {
          if (!isQueuedOffline(err)) return;
          toast(t('offline:queuedBadge'));
          onClose();
        },
      },
    );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl" data-testid="record-payment-dialog">
        <DialogHeader>
          <DialogTitle>{t('payments:record.title')}</DialogTitle>
          <DialogDescription>{t('payments:record.description')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="pay-customer">{t('payments:fields.customer')}</FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="pay-customer"
              value={customer}
              disabled={lockCustomer}
              onChange={(c) => {
                setCustomer(c);
                setAlloc(null);
              }}
            />
          </Field>
          <EnumSelect
            id="pay-kind"
            label={t('payments:fields.kind')}
            values={KINDS}
            labelOf={(k) => t(`billingEnums:paymentKind.${k}`)}
            value={kind}
            onChange={(k) => k && setKind(k)}
          />
          <EnumSelect
            id="pay-method"
            label={t('payments:fields.method')}
            values={COUNTED_METHODS}
            labelOf={(m) => t(`billingEnums:paymentMethod.${m}`)}
            value={method}
            onChange={(m) => m && setMethod(m)}
          />
          <Field>
            <FieldLabel htmlFor="pay-amount">{t('payments:fields.amount')}</FieldLabel>
            <MoneyInput
              id="pay-amount"
              locale={i18n.language}
              value={amount}
              onValueChange={(v) => {
                setAmount(v);
                setAlloc(null);
              }}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="pay-date">{t('payments:fields.receivedOn')}</FieldLabel>
            <DatePicker id="pay-date" value={receivedOn} onChange={setReceivedOn} />
          </Field>
          <Field>
            <FieldLabel htmlFor="pay-ref">{t('payments:fields.reference')}</FieldLabel>
            <Input
              id="pay-ref"
              maxLength={200}
              autoComplete="off"
              value={reference}
              onChange={(e) => setReference(e.target.value)}
            />
          </Field>
        </div>
        {kind === 'payment' && customer ? (
          <MatchingTable rows={rows} amountCents={cents} alloc={shown} onChange={setAlloc} />
        ) : null}
        {kind === 'payment' && !customer ? (
          <p className="text-muted-foreground text-sm">{t('payments:record.noCustomer')}</p>
        ) : null}
        <label className="flex items-center gap-2 text-sm">
          <Checkbox checked={sendReceipt} onCheckedChange={(v) => setSendReceipt(v === true)} />
          {t('payments:record.sendReceipt')}
        </label>
        {offlineReason ? <OfflineReason reason={offlineReason} /> : null}
        <BillingProblemAlert
          error={record.error && !isQueuedOffline(record.error) ? record.error : null}
        />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button disabled={!valid || record.isPending || offlineReason !== null} onClick={submit}>
            {t('payments:record.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
