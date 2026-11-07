import { getRouteApi, Link } from '@tanstack/react-router';
import { PrinterIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  useCharges,
  useMatchPayment,
  usePayment,
  useRefundPayment,
  useRetainDeposit,
  useReverseMatch,
  type Payment,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { MoneyInput } from '@porto/shared/money';
import { Badge } from '@porto/shared/ui/badge';
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
import { PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import {
  BillingBadge,
  BillingProblemAlert,
  Detail,
  DetailGrid,
  EnumSelect,
  IsoDate,
  Money,
  PageFrame,
  Section,
  SimpleTable,
  VatTable,
} from '../components/kit';
import { MatchingTable } from '../components/RecordPaymentDialog';
import { COUNTED_METHODS, type CountedMethod } from '../lib/cash';
import { autoFill, isValidAllocation, toMatches } from '../lib/matching';
import { toMatchRows } from '../lib/drafts';
import { useReceiptOpener } from '../lib/receipt';

const route = getRouteApi('/auth/m/$marinaId/payments/$paymentId');

export function PaymentPage() {
  const { marinaId, can } = useCurrentMarina();
  const { paymentId } = route.useParams();
  const payment = usePayment(marinaId, paymentId);
  return (
    <RequirePermission allowed={can('payments.collect') || can('charges.manage')}>
      {payment.isPending ? (
        <LoadingState rows={6} />
      ) : payment.isError ? (
        <QueryErrorState error={payment.error} onRetry={() => void payment.refetch()} />
      ) : (
        <PaymentDetail payment={payment.data} />
      )}
    </RequirePermission>
  );
}

function PaymentDetail({ payment }: { payment: Payment }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [dialog, setDialog] = useState<'refund' | 'retain' | 'match' | null>(null);
  const reverse = useReverseMatch(marinaId);
  const receipt = useReceiptOpener(marinaId);
  const close = () => setDialog(null);
  const liveMatches = (payment.matches ?? []).filter((m) => !m.reversed && !m.reverses_match_id);
  return (
    <PageFrame>
      <PageHeader
        title={t('payments:detail.title', { number: payment.number })}
        actions={
          <>
            {(['80', '58', 'a4'] as const).map((f) => (
              <Button
                key={f}
                variant="outline"
                disabled={receipt.pending}
                onClick={() => receipt.open(payment.id, f)}
              >
                <PrinterIcon />
                {t(`payments:receipt.${f === 'a4' ? 'a4' : `mm${f}`}`)}
              </Button>
            ))}
            {can('payments.collect') && payment.unallocated_cents > 0 && payment.customer ? (
              <Button variant="outline" onClick={() => setDialog('match')}>
                {t('payments:detail.match')}
              </Button>
            ) : null}
            {can('charges.manage') &&
            payment.kind === 'deposit' &&
            payment.unallocated_cents > 0 ? (
              <Button variant="outline" onClick={() => setDialog('retain')}>
                {t('payments:retain.open')}
              </Button>
            ) : null}
            {can('refunds.issue') && payment.kind !== 'credit' ? (
              <Button variant="outline" onClick={() => setDialog('refund')}>
                {t('payments:refund.open')}
              </Button>
            ) : null}
          </>
        }
      />
      <BillingProblemAlert error={reverse.error ?? receipt.error} />
      <Section title={t('payments:detail.summary')}>
        <DetailGrid>
          <Detail label={t('payments:fields.customer')}>
            {payment.customer ? (
              <Link
                to="/m/$marinaId/customers/$customerId"
                params={{ marinaId, customerId: payment.customer.id }}
                className="underline-offset-4 hover:underline"
              >
                {payment.customer.display_name ?? payment.customer.id}
              </Link>
            ) : null}
          </Detail>
          <Detail label={t('payments:fields.kind')}>
            <BillingBadge group="paymentKind" value={payment.kind} />
          </Detail>
          <Detail label={t('payments:fields.method')}>
            {t(`billingEnums:paymentMethod.${payment.method}`)}
          </Detail>
          <Detail label={t('payments:fields.source')}>
            {t(`billingEnums:paymentSource.${payment.source}`)}
          </Detail>
          <Detail label={t('payments:fields.receivedOn')}>
            <IsoDate date={payment.received_on} />
          </Detail>
          <Detail label={t('payments:fields.amount')}>
            <Money cents={payment.amount_cents} />
          </Detail>
          <Detail label={t('payments:fields.allocated')}>
            <Money cents={payment.allocated_cents} />
          </Detail>
          <Detail label={t('payments:fields.refunded')}>
            <Money cents={payment.refunded_cents} />
          </Detail>
          <Detail label={t('payments:fields.unallocated')}>
            <span data-testid="payment-unallocated">
              <Money cents={payment.unallocated_cents} />
            </span>
          </Detail>
          {payment.fee_cents ? (
            <Detail label={t('payments:fields.fee')}>
              <Money cents={payment.fee_cents} />
            </Detail>
          ) : null}
          {payment.has_reference ? (
            <Detail label={t('payments:fields.reference')}>
              {t('payments:detail.hasReference')}
            </Detail>
          ) : null}
          {payment.notes ? (
            <Detail label={t('payments:fields.notes')}>{payment.notes}</Detail>
          ) : null}
        </DetailGrid>
      </Section>
      <Section title={t('payments:detail.matches')}>
        <SimpleTable
          head={[
            t('payments:match.charge'),
            t('payments:match.amount'),
            t('payments:detail.at'),
            '',
          ]}
          empty={t('payments:detail.noMatches')}
          rows={payment.matches?.length ?? 0}
        >
          {(payment.matches ?? []).map((m) => (
            <TableRow key={m.id} data-testid="payment-match">
              <TableCell>
                <Link
                  to="/m/$marinaId/charges/$chargeId"
                  params={{ marinaId, chargeId: m.charge_id }}
                  className="underline-offset-4 hover:underline"
                >
                  {m.charge_number}
                </Link>
                {m.reversed ? (
                  <Badge variant="outline" className="ml-2">
                    {t('payments:detail.reversed')}
                  </Badge>
                ) : null}
                {m.reverses_match_id ? (
                  <Badge variant="outline" className="ml-2">
                    {t('payments:detail.reversal')}
                  </Badge>
                ) : null}
              </TableCell>
              <TableCell>
                <Money cents={m.amount_cents} />
              </TableCell>
              <TableCell>
                <IsoDate date={m.created_at.slice(0, 10)} />
              </TableCell>
              <TableCell className="text-right">
                {can('payments.collect') && !m.reversed && !m.reverses_match_id ? (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={reverse.isPending}
                    onClick={() => reverse.mutate({ paymentId: payment.id, matchId: m.id })}
                  >
                    {t('payments:detail.reverse')}
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      </Section>
      {payment.vat_split?.length ? (
        <Section title={t('payments:detail.vatSplit')}>
          <VatTable buckets={payment.vat_split} />
        </Section>
      ) : null}
      <Section title={t('payments:detail.refunds')}>
        <SimpleTable
          head={[
            t('payments:fields.number'),
            t('payments:fields.method'),
            t('payments:fields.amount'),
            t('payments:fields.status'),
            t('payments:refund.reason'),
          ]}
          empty={t('payments:detail.noRefunds')}
          rows={payment.refunds?.length ?? 0}
        >
          {(payment.refunds ?? []).map((r) => (
            <TableRow key={r.id} data-testid="payment-refund">
              <TableCell>{r.number}</TableCell>
              <TableCell>{t(`billingEnums:paymentMethod.${r.method}`)}</TableCell>
              <TableCell>
                <Money cents={r.amount_cents} />
              </TableCell>
              <TableCell>
                <BillingBadge group="refundStatus" value={r.status} />
              </TableCell>
              <TableCell>{r.reason}</TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      </Section>
      {payment.fiscal_documents?.length ? (
        <Section title={t('payments:detail.fiscalDocuments')}>
          <SimpleTable
            head={[t('fiscal:fields.number'), t('fiscal:fields.status'), t('fiscal:fields.total')]}
            empty=""
            rows={payment.fiscal_documents.length}
          >
            {payment.fiscal_documents.map((d) => (
              <TableRow key={d.id}>
                <TableCell>{d.number}</TableCell>
                <TableCell>
                  <BillingBadge group="fiscalStatus" value={d.status} />
                </TableCell>
                <TableCell>
                  <Money cents={d.total_cents} />
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
      ) : null}
      {dialog === 'refund' ? (
        <RefundDialog payment={payment} matches={liveMatches} onClose={close} />
      ) : null}
      {dialog === 'retain' ? <RetainDialog payment={payment} onClose={close} /> : null}
      {dialog === 'match' ? <MatchDialog payment={payment} onClose={close} /> : null}
    </PageFrame>
  );
}

/** Matches the unallocated money of a payment (credit) to open charges of its customer. */
function MatchDialog({ payment, onClose }: { payment: Payment; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const match = useMatchPayment(marinaId);
  const charges = useCharges(marinaId, { customer_id: payment.customer?.id });
  const rows = useMemo(
    () => toMatchRows(charges.data?.pages.flatMap((p) => p.items) ?? []),
    [charges.data],
  );
  const [alloc, setAlloc] = useState<Record<string, number> | null>(null);
  const shown = alloc ?? autoFill(rows, payment.unallocated_cents);
  const matches = toMatches(rows, shown);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('payments:detail.match')}</DialogTitle>
          <DialogDescription>
            {t('payments:detail.matchDescription')} <Money cents={payment.unallocated_cents} />
          </DialogDescription>
        </DialogHeader>
        <MatchingTable
          rows={rows}
          amountCents={payment.unallocated_cents}
          alloc={shown}
          onChange={setAlloc}
        />
        <BillingProblemAlert error={match.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={
              match.isPending ||
              matches.length === 0 ||
              !isValidAllocation(rows, shown, payment.unallocated_cents)
            }
            onClick={() => match.mutate({ id: payment.id, matches }, { onSuccess: onClose })}
          >
            {t('payments:detail.matchSubmit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Refund (m5-plan §4.3, D6): refunds come only from unallocated money; refunding a paid charge unmatches it in the
 * same transaction. A provider payment can be refunded through the provider.
 */
function RefundDialog({
  payment,
  matches,
  onClose,
}: {
  payment: Payment;
  matches: NonNullable<Payment['matches']>;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const refund = useRefundPayment(marinaId);
  const [unmatch, setUnmatch] = useState<Record<string, number>>({});
  const [rev, setRev] = useState<Record<string, number>>({});
  const [amount, setAmount] = useState<number | null>(payment.unallocated_cents || null);
  const [method, setMethod] = useState<CountedMethod>(
    payment.method === 'cash' ? 'cash' : 'bank_transfer',
  );
  const viaProvider = payment.source === 'provider';
  const [useProvider, setUseProvider] = useState(viaProvider);
  const [reason, setReason] = useState('');
  const unmatched = Object.values(unmatch).reduce((s, v) => s + v, 0);
  const max = payment.unallocated_cents + unmatched;
  const cents = amount ?? 0;
  const valid = cents > 0 && cents <= max && reason.trim().length > 0;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl" data-testid="refund-dialog">
        <DialogHeader>
          <DialogTitle>{t('payments:refund.title', { number: payment.number })}</DialogTitle>
          <DialogDescription>{t('payments:refund.description')}</DialogDescription>
        </DialogHeader>
        {matches.length ? (
          <SimpleTable
            head={[
              t('payments:match.charge'),
              t('payments:match.amount'),
              t('payments:refund.unmatch'),
            ]}
            empty=""
            rows={matches.length}
          >
            {matches.map((m) => (
              <TableRow key={m.id}>
                <TableCell>{m.charge_number}</TableCell>
                <TableCell>
                  <Money cents={m.amount_cents} />
                </TableCell>
                <TableCell className="w-36">
                  <MoneyInput
                    key={`${m.id}-${rev[m.id] ?? 0}`}
                    autoFocus={(rev[m.id] ?? 0) > 0}
                    aria-label={t('payments:refund.unmatchOf', { number: m.charge_number })}
                    locale={i18n.language}
                    value={unmatch[m.id] ?? 0}
                    onValueChange={(v) => {
                      const clamped = Math.max(0, Math.min(v ?? 0, m.amount_cents));
                      // A clamped amount remounts the field so it shows the limit at once.
                      if (v !== null && clamped !== v)
                        setRev((x) => ({ ...x, [m.id]: (x[m.id] ?? 0) + 1 }));
                      setUnmatch({ ...unmatch, [m.id]: clamped });
                    }}
                  />
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        ) : null}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="refund-amount">{t('payments:refund.amount')}</FieldLabel>
            <MoneyInput
              id="refund-amount"
              locale={i18n.language}
              value={amount}
              onValueChange={setAmount}
              aria-invalid={cents > max ? true : undefined}
            />
            <p className="text-muted-foreground text-xs">
              {t('payments:refund.max')} <Money cents={max} />
            </p>
          </Field>
          {!useProvider ? (
            <EnumSelect
              id="refund-method"
              label={t('payments:fields.method')}
              values={COUNTED_METHODS}
              labelOf={(m) => t(`billingEnums:paymentMethod.${m}`)}
              value={method}
              onChange={(m) => m && setMethod(m)}
            />
          ) : null}
        </div>
        {viaProvider ? (
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={useProvider} onCheckedChange={(v) => setUseProvider(v === true)} />
            {t('payments:refund.viaProvider')}
          </label>
        ) : null}
        <Field>
          <FieldLabel htmlFor="refund-reason">{t('payments:refund.reason')}</FieldLabel>
          <Textarea
            id="refund-reason"
            maxLength={500}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <BillingProblemAlert error={refund.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            variant="destructive"
            disabled={!valid || refund.isPending}
            onClick={() =>
              refund.mutate(
                {
                  id: payment.id,
                  body: {
                    amount_cents: cents,
                    reason: reason.trim(),
                    ...(useProvider ? { via_provider: true } : { method }),
                    ...(unmatched > 0
                      ? {
                          unmatch: Object.entries(unmatch)
                            .filter(([, v]) => v > 0)
                            .map(([match_id, amount_cents]) => ({ match_id, amount_cents })),
                        }
                      : {}),
                  },
                },
                {
                  onSuccess: () => {
                    toast.success(t('payments:refund.done'));
                    onClose();
                  },
                },
              )
            }
          >
            {t('payments:refund.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Deposit retention: part of a deposit becomes a charge (deposit_retention) matched by the deposit. */
function RetainDialog({ payment, onClose }: { payment: Payment; onClose: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const retain = useRetainDeposit(marinaId);
  const [amount, setAmount] = useState<number | null>(null);
  const [reason, setReason] = useState('');
  const cents = amount ?? 0;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('payments:retain.title')}</DialogTitle>
          <DialogDescription>{t('payments:retain.description')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="retain-amount">{t('payments:fields.amount')}</FieldLabel>
          <MoneyInput
            id="retain-amount"
            locale={i18n.language}
            value={amount}
            onValueChange={setAmount}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="retain-reason">{t('payments:refund.reason')}</FieldLabel>
          <Textarea
            id="retain-reason"
            maxLength={500}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <BillingProblemAlert error={retain.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={
              retain.isPending || cents <= 0 || cents > payment.unallocated_cents || !reason.trim()
            }
            onClick={() =>
              retain.mutate(
                { id: payment.id, body: { amount_cents: cents, reason: reason.trim() } },
                { onSuccess: onClose },
              )
            }
          >
            {t('payments:retain.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
