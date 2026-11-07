// M5 extensions of other features' pages (m5-plan §9.1), exported through ../public.ts: the transit desk payment
// fields, payment link and charge panel; the instalment charge columns of the contract Rate tab; the mandate
// "Liquida" action.
import { Link } from '@tanstack/react-router';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  useCharges,
  useOnlinePaymentsEnabled,
  useSettleMandate,
  useTransitPaymentLink,
  type Charge,
  type SettleMandateInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
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
import { DatePicker } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { useMarinaToday } from '../../registry/lib/today';
import { COUNTED_METHODS } from '../lib/cash';
import { invoicingVariant, paymentVariant } from '../lib/charges';
import {
  BillingBadge,
  BillingProblemAlert,
  EnumSelect,
  IsoDate,
  Money,
  Section,
  SimpleTable,
} from './kit';
import { PayLinkField, PaymentRequestDialog } from './PaymentRequestDialog';
import { RecordPaymentDialog } from './RecordPaymentDialog';
import { type DeskPaymentValue } from '../lib/catalog';

/** "Incassa" fields of the desk confirmation (D8): method, amount (default the transit total), receipt. */
export function DeskPaymentFields({
  idPrefix,
  value,
  onChange,
}: {
  idPrefix: string;
  value: DeskPaymentValue;
  onChange: (v: DeskPaymentValue) => void;
}) {
  const { t, i18n } = useT();
  const { can } = useCurrentMarina();
  if (!can('payments.collect') && !can('payments.collect_cash')) return null;
  return (
    <div className="flex flex-col gap-3 rounded-md border p-3" data-testid="desk-payment">
      <label className="flex items-center gap-2 text-sm">
        <Checkbox
          checked={value.collect}
          onCheckedChange={(c) => onChange({ ...value, collect: c === true })}
        />
        {t('payments:desk.collect')}
      </label>
      {value.collect ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <EnumSelect
            id={`${idPrefix}-pay-method`}
            label={t('payments:fields.method')}
            values={COUNTED_METHODS}
            labelOf={(m) => t(`billingEnums:paymentMethod.${m}`)}
            value={value.method}
            onChange={(m) => m && onChange({ ...value, method: m })}
          />
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-pay-amount`}>
              {t('payments:fields.amount')}
            </FieldLabel>
            <MoneyInput
              id={`${idPrefix}-pay-amount`}
              locale={i18n.language}
              value={value.amountCents}
              onValueChange={(amountCents) => onChange({ ...value, amountCents })}
            />
          </Field>
          <label className="flex items-center gap-2 text-sm sm:col-span-2">
            <Checkbox
              checked={value.fiscalDocument}
              onCheckedChange={(c) => onChange({ ...value, fiscalDocument: c === true })}
            />
            {t('payments:desk.fiscalDocument')}
          </label>
        </div>
      ) : null}
    </div>
  );
}

/** "Invia link di pagamento" of a transit: shown only when an online provider is enabled. */
export function TransitPaymentLinkButton({
  transit,
}: {
  transit: { id: string; version: number; status: string };
}) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const online = useOnlinePaymentsEnabled(marinaId, can('transits.desk'));
  const link = useTransitPaymentLink(marinaId);
  const [url, setUrl] = useState<string | null>(null);
  if (!online || !can('transits.desk') || !['held', 'pending_payment'].includes(transit.status))
    return null;
  return (
    <>
      <Button
        variant="outline"
        disabled={link.isPending}
        onClick={() =>
          link.mutate(
            {
              id: transit.id,
              version: transit.version,
              body: { send: true, expires_in_hours: 24 },
            },
            { onSuccess: (r) => setUrl(r.url) },
          )
        }
      >
        {t('payments:desk.paymentLink')}
      </Button>
      {url || link.error ? (
        <Dialog
          open
          onOpenChange={(o) => {
            if (!o) {
              setUrl(null);
              link.reset();
            }
          }}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('payments:desk.paymentLink')}</DialogTitle>
              <DialogDescription>{t('payments:desk.paymentLinkSent')}</DialogDescription>
            </DialogHeader>
            {url ? <PayLinkField url={url} /> : null}
            <BillingProblemAlert error={link.error} />
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  );
}

/** Charges of a contract or transit with status and residual, and the collect / request actions. */
export function ContractChargesCard({
  contractId,
  customer,
  title,
}: {
  contractId: string;
  customer?: { id: string; display_name?: string } | null;
  title?: string;
}) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const allowed = can('charges.manage') || can('payments.collect');
  const charges = useCharges(marinaId, { contract_id: contractId }, allowed);
  const items = useMemo(() => charges.data?.pages.flatMap((p) => p.items) ?? [], [charges.data]);
  const [dialog, setDialog] = useState<{ kind: 'collect' | 'request'; charge: Charge } | null>(
    null,
  );
  if (!allowed) return null;
  return (
    <Section title={title ?? t('charges:panel.title')}>
      <SimpleTable
        head={[
          t('charges:fields.number'),
          t('charges:fields.dueOn'),
          t('charges:fields.total'),
          t('charges:fields.residual'),
          t('charges:fields.status'),
          '',
        ]}
        empty={t('charges:panel.empty')}
        rows={items.length}
      >
        {items.map((c) => (
          <TableRow key={c.id} data-testid="contract-charge">
            <TableCell>
              <Link
                to="/m/$marinaId/charges/$chargeId"
                params={{ marinaId, chargeId: c.id }}
                className="underline-offset-4 hover:underline"
              >
                {c.number}
              </Link>
            </TableCell>
            <TableCell>
              <IsoDate date={c.due_on} />
            </TableCell>
            <TableCell>
              <Money cents={c.total_cents} />
            </TableCell>
            <TableCell>
              <Money cents={c.residual_cents} />
            </TableCell>
            <TableCell className="flex flex-wrap gap-1">
              <BillingBadge
                group="paymentStatus"
                value={c.payment_status}
                variant={paymentVariant(c)}
              />
              <BillingBadge
                group="invoicingStatus"
                value={c.invoicing_status}
                variant={invoicingVariant(c.invoicing_status)}
              />
            </TableCell>
            <TableCell className="text-right">
              {c.residual_cents > 0 && c.payment_status !== 'cancelled' ? (
                <div className="flex justify-end gap-2">
                  {can('payments.collect') ? (
                    <Button size="sm" onClick={() => setDialog({ kind: 'collect', charge: c })}>
                      {t('charges:actions.collect')}
                    </Button>
                  ) : null}
                  {can('charges.manage') && c.customer ? (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setDialog({ kind: 'request', charge: c })}
                    >
                      {t('charges:actions.requestPayment')}
                    </Button>
                  ) : null}
                </div>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      {dialog?.kind === 'collect' ? (
        <RecordPaymentDialog
          customer={
            dialog.charge.customer
              ? {
                  id: dialog.charge.customer.id,
                  label: dialog.charge.customer.display_name ?? customer?.display_name ?? '',
                }
              : null
          }
          lockCustomer
          chargeIds={[dialog.charge.id]}
          amountCents={dialog.charge.residual_cents}
          onClose={() => setDialog(null)}
          onDone={() => toast.success(t('payments:record.done'))}
        />
      ) : null}
      {dialog?.kind === 'request' && dialog.charge.customer ? (
        <PaymentRequestDialog
          customerId={dialog.charge.customer.id}
          chargeIds={[dialog.charge.id]}
          totalCents={dialog.charge.residual_cents}
          onClose={() => setDialog(null)}
        />
      ) : null}
    </Section>
  );
}

/** "Liquida" of a management mandate (D21): owner shares through a date become credit or a recorded payout. */
export function MandateSettleButton({ mandateId }: { mandateId: string }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const settle = useSettleMandate(marinaId);
  const [open, setOpen] = useState(false);
  const [through, setThrough] = useState<string | null>(today);
  const [mode, setMode] = useState<SettleMandateInput['mode']>('credit');
  if (!can('refunds.issue')) return null;
  return (
    <>
      <Button variant="outline" onClick={() => setOpen(true)}>
        {t('payments:mandate.open')}
      </Button>
      {open ? (
        <Dialog open onOpenChange={(o) => !o && setOpen(false)}>
          <DialogContent data-testid="mandate-settle-dialog">
            <DialogHeader>
              <DialogTitle>{t('payments:mandate.title')}</DialogTitle>
              <DialogDescription>{t('payments:mandate.description')}</DialogDescription>
            </DialogHeader>
            <Field>
              <FieldLabel htmlFor="mandate-through">{t('payments:mandate.through')}</FieldLabel>
              <DatePicker id="mandate-through" value={through} onChange={setThrough} />
            </Field>
            <EnumSelect
              id="mandate-mode"
              label={t('payments:mandate.mode')}
              values={['credit', 'payout'] as const}
              labelOf={(m) => t(`payments:mandate.modes.${m}`)}
              value={mode}
              onChange={(m) => m && setMode(m)}
            />
            <BillingProblemAlert error={settle.error} />
            <DialogFooter>
              <Button variant="outline" onClick={() => setOpen(false)}>
                {t('common:actions.cancel')}
              </Button>
              <Button
                disabled={!through || settle.isPending}
                onClick={() =>
                  settle.mutate(
                    { mandateId, body: { through: through ?? today, mode } },
                    {
                      onSuccess: (r) => {
                        toast.success(
                          t('payments:mandate.done', {
                            credited: formatCents(r.credited_cents, 'EUR', i18n.language),
                            paid: formatCents(r.paid_out_cents, 'EUR', i18n.language),
                          }),
                        );
                        setOpen(false);
                      },
                    },
                  )
                }
              >
                {t('payments:mandate.submit')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  );
}
