import { getRouteApi, Link } from '@tanstack/react-router';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useAdjustCharge,
  useCancelCharge,
  useCharge,
  useSetChargeInvoice,
  useUpdateCharge,
  type Charge,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatQtyMilli } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
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
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import {
  BillingBadge,
  BillingProblemAlert,
  Detail,
  DetailGrid,
  IsoDate,
  Money,
  PageFrame,
  ReasonDialog,
  Section,
  SimpleTable,
  VatRate,
  VatTable,
} from '../components/kit';
import { LinesEditor } from '../components/LinesEditor';
import { PaymentRequestDialog } from '../components/PaymentRequestDialog';
import { RecordPaymentDialog } from '../components/RecordPaymentDialog';
import { chargeActions, invoicingVariant, paymentVariant } from '../lib/charges';
import { type LineDraft, draftsOf, emptyLine, linesValid, toLineInputs } from '../lib/drafts';

const route = getRouteApi('/auth/m/$marinaId/charges/$chargeId');

export function ChargePage() {
  const { marinaId, can } = useCurrentMarina();
  const { chargeId } = route.useParams();
  const charge = useCharge(marinaId, chargeId);
  return (
    <RequirePermission allowed={can('charges.manage') || can('payments.collect')}>
      {charge.isPending ? (
        <LoadingState rows={6} />
      ) : charge.isError ? (
        <QueryErrorState error={charge.error} onRetry={() => void charge.refetch()} />
      ) : (
        <ChargeDetail charge={charge.data} />
      )}
    </RequirePermission>
  );
}

type DialogKind = 'edit' | 'cancel' | 'adjust' | 'invoice' | 'request' | 'collect' | null;

function ChargeDetail({ charge }: { charge: Charge }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [dialog, setDialog] = useState<DialogKind>(null);
  const cancel = useCancelCharge(marinaId);
  const acts = chargeActions(charge);
  const manage = can('charges.manage');
  const close = () => setDialog(null);
  return (
    <PageFrame>
      <PageHeader
        title={t('charges:detail.title', { number: charge.number })}
        description={charge.description}
        actions={
          <>
            {manage && acts.edit ? (
              <Button variant="outline" onClick={() => setDialog('edit')}>
                {t('charges:actions.edit')}
              </Button>
            ) : null}
            {manage && acts.cancel ? (
              <Button variant="outline" onClick={() => setDialog('cancel')}>
                {t('charges:actions.cancel')}
              </Button>
            ) : null}
            {manage && acts.adjust ? (
              <Button variant="outline" onClick={() => setDialog('adjust')}>
                {t('charges:actions.adjust')}
              </Button>
            ) : null}
            {can('charges.export') && acts.invoiceRef ? (
              <Button variant="outline" onClick={() => setDialog('invoice')}>
                {t('charges:actions.invoiceRef')}
              </Button>
            ) : null}
            {manage && acts.requestPayment ? (
              <Button variant="outline" onClick={() => setDialog('request')}>
                {t('charges:actions.requestPayment')}
              </Button>
            ) : null}
            {can('payments.collect') && acts.collect ? (
              <Button onClick={() => setDialog('collect')}>{t('charges:actions.collect')}</Button>
            ) : null}
          </>
        }
      />
      <div className="flex flex-wrap gap-2">
        <BillingBadge
          group="paymentStatus"
          value={charge.payment_status}
          variant={paymentVariant(charge)}
        />
        <BillingBadge
          group="invoicingStatus"
          value={charge.invoicing_status}
          variant={invoicingVariant(charge.invoicing_status)}
        />
        <BillingBadge group="fiscalMode" value={charge.fiscal_mode} variant="outline" />
        {charge.overdue ? <Badge variant="destructive">{t('charges:list.overdue')}</Badge> : null}
      </div>
      <Section title={t('charges:detail.summary')}>
        <DetailGrid>
          <Detail label={t('charges:fields.customer')}>
            {charge.customer ? (
              <Link
                to="/m/$marinaId/customers/$customerId"
                params={{ marinaId, customerId: charge.customer.id }}
                className="underline-offset-4 hover:underline"
              >
                {charge.customer.display_name ?? charge.customer.id}
              </Link>
            ) : (
              t('charges:list.walkIn')
            )}
          </Detail>
          <Detail label={t('charges:fields.origin')}>
            {t(`billingEnums:originType.${charge.origin_type}`)}
            {charge.contract_code ? ` · ${charge.contract_code}` : ''}
          </Detail>
          <Detail label={t('charges:fields.issuedOn')}>
            <IsoDate date={charge.issued_on} />
          </Detail>
          <Detail label={t('charges:fields.dueOn')}>
            <IsoDate date={charge.due_on} />
          </Detail>
          {charge.service_from ? (
            <Detail label={t('charges:fields.service')}>
              <IsoDate date={charge.service_from} /> – <IsoDate date={charge.service_to} />
            </Detail>
          ) : null}
          <Detail label={t('charges:fields.total')}>
            <Money cents={charge.total_cents} />
          </Detail>
          <Detail label={t('charges:fields.paid')}>
            <Money cents={charge.paid_amount_cents} />
          </Detail>
          <Detail label={t('charges:fields.credited')}>
            <Money cents={charge.credited_cents} />
          </Detail>
          <Detail label={t('charges:fields.residual')}>
            <span data-testid="charge-residual">
              <Money cents={charge.residual_cents} />
            </span>
          </Detail>
          {charge.external_invoice_number ? (
            <Detail label={t('charges:fields.invoice')}>
              {charge.external_invoice_number} <IsoDate date={charge.external_invoice_date} />
            </Detail>
          ) : null}
          {charge.corrects_charge ? (
            <Detail label={t('charges:fields.corrects')}>
              <Link
                to="/m/$marinaId/charges/$chargeId"
                params={{ marinaId, chargeId: charge.corrects_charge.id }}
                className="underline-offset-4 hover:underline"
              >
                {charge.corrects_charge.number}
              </Link>
              {charge.correction_kind
                ? ` · ${t(`billingEnums:correctionKind.${charge.correction_kind}`)}`
                : ''}
            </Detail>
          ) : null}
          {charge.cancel_reason ? (
            <Detail label={t('charges:fields.cancelReason')}>{charge.cancel_reason}</Detail>
          ) : null}
        </DetailGrid>
      </Section>
      <Section title={t('charges:detail.lines')}>
        <SimpleTable
          head={[
            '#',
            t('charges:lines.item'),
            t('charges:lines.qty'),
            t('charges:lines.unit'),
            t('charges:vat.net'),
            t('charges:lines.vat'),
            t('charges:vat.gross'),
          ]}
          empty={t('charges:lines.empty')}
          rows={charge.lines?.length ?? 0}
        >
          {(charge.lines ?? []).map((l) => (
            <TableRow key={l.seq} data-testid="charge-line">
              <TableCell>{l.seq}</TableCell>
              <TableCell>
                {l.item_code} · {l.description}
              </TableCell>
              <TableCell>{formatQtyMilli(l.qty_milli, i18n.language)}</TableCell>
              <TableCell>
                <Money cents={l.unit_cents} />
              </TableCell>
              <TableCell>
                <Money cents={l.net_cents} />
              </TableCell>
              <TableCell>
                <VatRate bp={l.vat_rate_bp} nature={l.vat_nature} />
              </TableCell>
              <TableCell>
                <Money cents={l.gross_cents} />
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      </Section>
      <Section title={t('charges:detail.vatSummary')}>
        <VatTable buckets={charge.vat_summary ?? []} />
      </Section>
      <div className="grid gap-4 lg:grid-cols-2">
        <Section title={t('charges:detail.matches')}>
          <SimpleTable
            head={[t('charges:detail.payment'), t('charges:fields.total'), t('charges:detail.at')]}
            empty={t('charges:detail.noMatches')}
            rows={charge.matches?.length ?? 0}
          >
            {(charge.matches ?? []).map((m) => (
              <TableRow key={m.id}>
                <TableCell>
                  <Link
                    to="/m/$marinaId/payments/$paymentId"
                    params={{ marinaId, paymentId: m.payment_id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {m.payment_number}
                  </Link>
                  {m.reverses_match_id ? (
                    <Badge variant="outline" className="ml-2">
                      {t('charges:detail.reversal')}
                    </Badge>
                  ) : null}
                </TableCell>
                <TableCell>
                  <Money cents={m.amount_cents} />
                </TableCell>
                <TableCell>
                  <IsoDate date={m.created_at.slice(0, 10)} />
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
        <Section title={t('charges:detail.corrections')}>
          <SimpleTable
            head={[
              t('charges:fields.number'),
              t('charges:fields.total'),
              t('charges:fields.status'),
            ]}
            empty={t('charges:detail.noCorrections')}
            rows={(charge.corrections?.length ?? 0) + (charge.offsets?.length ?? 0)}
          >
            {(charge.corrections ?? []).map((c) => (
              <TableRow key={c.id}>
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
                  <Money cents={c.total_cents} />
                </TableCell>
                <TableCell>
                  {c.payment_status ? t(`billingEnums:paymentStatus.${c.payment_status}`) : ''}
                </TableCell>
              </TableRow>
            ))}
            {(charge.offsets ?? []).map((o) => (
              <TableRow key={`${o.negative_charge_id}-${o.target_charge_id}`}>
                <TableCell>{t('charges:detail.offset')}</TableCell>
                <TableCell>
                  <Money cents={o.amount_cents} />
                </TableCell>
                <TableCell />
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
        <Section title={t('charges:detail.exports')}>
          <SimpleTable
            head={[t('exports:fields.number'), t('exports:fields.kind'), t('charges:detail.at')]}
            empty={t('charges:detail.noExports')}
            rows={charge.exports?.length ?? 0}
          >
            {(charge.exports ?? []).map((e) => (
              <TableRow key={e.export_id}>
                <TableCell>
                  <Link
                    to="/m/$marinaId/exports/$exportId"
                    params={{ marinaId, exportId: e.export_id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {e.number}
                  </Link>
                </TableCell>
                <TableCell>
                  {e.first ? t('exports:fields.first') : t('billingEnums:exportKind.reexport')}
                </TableCell>
                <TableCell>
                  {e.created_at ? <IsoDate date={e.created_at.slice(0, 10)} /> : null}
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
        <Section title={t('charges:detail.reminders')}>
          <SimpleTable
            head={[
              t('reminders:fields.level'),
              t('reminders:fields.status'),
              t('charges:detail.at'),
            ]}
            empty={t('charges:detail.noReminders')}
            rows={charge.reminders?.length ?? 0}
          >
            {(charge.reminders ?? []).map((r, i) => (
              <TableRow key={i}>
                <TableCell>{t(`billingEnums:reminderLevel.${r.level}`)}</TableCell>
                <TableCell>{t(`billingEnums:reminderItemStatus.${r.status}`)}</TableCell>
                <TableCell>
                  <IsoDate date={r.created_at.slice(0, 10)} />
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
      </div>
      {dialog === 'edit' ? <EditChargeDialog charge={charge} onClose={close} /> : null}
      {dialog === 'adjust' ? <AdjustDialog charge={charge} onClose={close} /> : null}
      {dialog === 'invoice' ? <InvoiceRefDialog charge={charge} onClose={close} /> : null}
      {dialog === 'cancel' ? (
        <ReasonDialog
          open
          required
          destructive
          title={t('charges:cancel.title', { number: charge.number })}
          description={t('charges:cancel.description')}
          confirmLabel={t('charges:actions.cancel')}
          pending={cancel.isPending}
          error={cancel.error}
          onClose={close}
          onConfirm={(reason) =>
            cancel.mutate({ id: charge.id, version: charge.version, reason }, { onSuccess: close })
          }
        />
      ) : null}
      {dialog === 'request' && charge.customer ? (
        <PaymentRequestDialog
          customerId={charge.customer.id}
          chargeIds={[charge.id]}
          totalCents={charge.residual_cents}
          onClose={close}
        />
      ) : null}
      {dialog === 'collect' ? (
        <RecordPaymentDialog
          customer={
            charge.customer
              ? { id: charge.customer.id, label: charge.customer.display_name ?? '' }
              : null
          }
          lockCustomer
          chargeIds={[charge.id]}
          amountCents={charge.residual_cents}
          onClose={close}
          onDone={() => toast.success(t('payments:record.done'))}
        />
      ) : null}
    </PageFrame>
  );
}

function EditChargeDialog({ charge, onClose }: { charge: Charge; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateCharge(marinaId);
  const [lines, setLines] = useState<LineDraft[]>(() => draftsOf(charge));
  const [due, setDue] = useState<string | null>(charge.due_on);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{t('charges:edit.title', { number: charge.number })}</DialogTitle>
          <DialogDescription>{t('charges:edit.description')}</DialogDescription>
        </DialogHeader>
        <Field className="w-auto">
          <FieldLabel htmlFor="edit-due">{t('charges:fields.dueOn')}</FieldLabel>
          <DatePicker id="edit-due" value={due} onChange={setDue} />
        </Field>
        <LinesEditor marinaId={marinaId} lines={lines} onChange={setLines} idPrefix="edit" />
        <BillingProblemAlert error={update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={update.isPending || !linesValid(lines)}
            onClick={() =>
              update.mutate(
                {
                  id: charge.id,
                  version: charge.version,
                  patch: { lines: toLineInputs(lines), ...(due ? { due_on: due } : {}) },
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Adjustment dialog (m5-plan §3.5): delta lines (signed) or replace with the corrected lines, live totals. */
function AdjustDialog({ charge, onClose }: { charge: Charge; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const adjust = useAdjustCharge(marinaId);
  const [mode, setMode] = useState<'delta' | 'replace'>('delta');
  const [delta, setDelta] = useState<LineDraft[]>(() => [emptyLine()]);
  const [replace, setReplace] = useState<LineDraft[]>(() => draftsOf(charge));
  const [reason, setReason] = useState('');
  const lines = mode === 'delta' ? delta : replace;
  const valid = linesValid(lines, mode === 'delta') && reason.trim().length > 0;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-4xl" data-testid="adjust-dialog">
        <DialogHeader>
          <DialogTitle>{t('charges:adjust.title', { number: charge.number })}</DialogTitle>
          <DialogDescription>{t('charges:adjust.description')}</DialogDescription>
        </DialogHeader>
        <RadioGroup
          value={mode}
          onValueChange={(v) => setMode(v as 'delta' | 'replace')}
          className="flex flex-wrap gap-4"
          aria-label={t('charges:adjust.mode')}
        >
          {(['delta', 'replace'] as const).map((m) => (
            <label key={m} className="flex items-center gap-2 text-sm">
              <RadioGroupItem value={m} />
              {t(`charges:adjust.modes.${m}`)}
            </label>
          ))}
        </RadioGroup>
        <p className="text-muted-foreground text-sm">{t(`charges:adjust.hints.${mode}`)}</p>
        {mode === 'delta' ? (
          <LinesEditor
            marinaId={marinaId}
            lines={delta}
            onChange={setDelta}
            allowNegative
            idPrefix="delta"
          />
        ) : (
          <LinesEditor marinaId={marinaId} lines={replace} onChange={setReplace} idPrefix="repl" />
        )}
        <Field>
          <FieldLabel htmlFor="adjust-reason">{t('charges:adjust.reason')}</FieldLabel>
          <Textarea
            id="adjust-reason"
            maxLength={500}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <BillingProblemAlert error={adjust.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={!valid || adjust.isPending}
            onClick={() =>
              adjust.mutate(
                {
                  id: charge.id,
                  version: charge.version,
                  body: {
                    mode,
                    lines: toLineInputs(lines, mode === 'delta'),
                    reason: reason.trim(),
                  },
                },
                {
                  onSuccess: (r) => {
                    toast.success(t('charges:adjust.done', { count: r.adjustments.length }));
                    onClose();
                  },
                },
              )
            }
          >
            {t('charges:adjust.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function InvoiceRefDialog({ charge, onClose }: { charge: Charge; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const set = useSetChargeInvoice(marinaId);
  const [number, setNumber] = useState(charge.external_invoice_number ?? '');
  const [date, setDate] = useState<string | null>(charge.external_invoice_date ?? today);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('charges:invoice.title')}</DialogTitle>
          <DialogDescription>{t('charges:invoice.description')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="inv-number">{t('charges:invoice.number')}</FieldLabel>
          <Input
            id="inv-number"
            maxLength={64}
            value={number}
            onChange={(e) => setNumber(e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="inv-date">{t('charges:invoice.date')}</FieldLabel>
          <DatePicker id="inv-date" value={date} onChange={setDate} />
        </Field>
        <BillingProblemAlert error={set.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={set.isPending || !number.trim() || !date}
            onClick={() =>
              set.mutate(
                { id: charge.id, number: number.trim(), date: date ?? '' },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
