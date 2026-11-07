import { Link } from '@tanstack/react-router';
import { DownloadIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useApplyCredit,
  useCreateSepaMandate,
  useCustomerDashboard,
  useCustomerStatement,
  useCustomerStatementFile,
  useOnlinePaymentsEnabled,
  useRevokeSepaMandate,
  useSepaMandates,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatDateTime } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { AgeingBar } from '../pages/ReceivablesPage';
import {
  BillingBadge,
  BillingProblemAlert,
  IsoDate,
  Money,
  Section,
  SimpleTable,
  Stat,
} from './kit';
import { PayLinkField } from './PaymentRequestDialog';
import { RecordPaymentDialog } from './RecordPaymentDialog';
import { openBlob, saveBlob } from '../lib/files';

/** Tab "Cruscotto" of the customer page (spec 11.4, m5-plan §4.5): everything about the customer at a glance. */
export function CustomerDashboardTab({
  customerId,
  customerName,
}: {
  customerId: string;
  customerName: string;
}) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const dash = useCustomerDashboard(marinaId, customerId);
  const apply = useApplyCredit(marinaId);
  const [collecting, setCollecting] = useState(false);
  if (dash.isPending) return <LoadingState rows={4} />;
  if (dash.isError)
    return <QueryErrorState error={dash.error} onRetry={() => void dash.refetch()} />;
  const d = dash.data;
  const billing = d.balance_cents !== undefined;
  return (
    <div className="flex flex-col gap-4" data-testid="customer-dashboard">
      {billing ? (
        <>
          <section className="grid gap-2 sm:grid-cols-3">
            <Stat label={t('customerBilling:dashboard.balance')} testId="dashboard-balance">
              <Money cents={d.balance_cents} />
            </Stat>
            <Stat label={t('customerBilling:dashboard.credit')}>
              <Money cents={d.credit_cents ?? 0} />
            </Stat>
            <Stat label={t('customerBilling:dashboard.overdue')}>
              <Money cents={d.arrears ? d.arrears.total_cents - d.arrears.not_due_cents : 0} />
            </Stat>
          </section>
          {d.arrears ? <AgeingBar amounts={d.arrears} /> : null}
          <div className="flex flex-wrap gap-2">
            {can('payments.collect') ? (
              <Button onClick={() => setCollecting(true)}>
                {t('customerBilling:dashboard.collect')}
              </Button>
            ) : null}
            {can('payments.collect') && (d.credit_cents ?? 0) > 0 ? (
              <Button
                variant="outline"
                disabled={apply.isPending}
                onClick={() =>
                  apply.mutate(
                    { customerId },
                    {
                      onSuccess: (r) =>
                        toast.success(
                          t('customerBilling:dashboard.applied', {
                            amount: formatCents(r.matched_cents, 'EUR', i18n.language),
                          }),
                        ),
                    },
                  )
                }
              >
                {t('customerBilling:dashboard.applyCredit')}
              </Button>
            ) : null}
            <Button asChild variant="outline">
              <Link to="/m/$marinaId/charges" params={{ marinaId }}>
                {t('customerBilling:dashboard.charges')}
              </Link>
            </Button>
          </div>
          <BillingProblemAlert error={apply.error} />
        </>
      ) : null}
      <div className="grid gap-4 lg:grid-cols-2">
        <Section title={t('customerBilling:dashboard.contracts')}>
          <SimpleTable
            head={[
              t('customerBilling:dashboard.code'),
              t('customerBilling:dashboard.period'),
              t('customerBilling:dashboard.status'),
            ]}
            empty={t('customerBilling:dashboard.none')}
            rows={d.contracts.length}
          >
            {d.contracts.map((c) => (
              <TableRow key={c.id}>
                <TableCell>
                  <Link
                    to="/m/$marinaId/contracts/$contractId"
                    params={{ marinaId, contractId: c.id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {c.code}
                  </Link>
                </TableCell>
                <TableCell>
                  <IsoDate date={c.starts_on} /> <IsoDate date={c.ends_on} />
                </TableCell>
                <TableCell>{c.status}</TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
        <Section title={t('customerBilling:dashboard.vessels')}>
          <ul className="flex flex-col gap-1 text-sm">
            {d.vessels.length === 0 ? (
              <li className="text-muted-foreground">{t('customerBilling:dashboard.none')}</li>
            ) : (
              d.vessels.map((v) => <li key={v.id}>{v.name}</li>)
            )}
          </ul>
          <p className="text-muted-foreground text-xs">
            {t('customerBilling:dashboard.berthsToday', { count: d.berths_today.length })}
          </p>
        </Section>
        {billing ? (
          <Section title={t('customerBilling:dashboard.futureInstallments')}>
            <SimpleTable
              head={[t('receivables:fields.dueOn'), t('receivables:fields.amount')]}
              empty={t('customerBilling:dashboard.none')}
              rows={d.future_installments?.length ?? 0}
            >
              {(d.future_installments ?? []).map((r) => (
                <TableRow key={r.id}>
                  <TableCell>
                    <IsoDate date={r.due_on} />
                  </TableCell>
                  <TableCell>
                    <Money cents={r.amount_cents} />
                  </TableCell>
                </TableRow>
              ))}
            </SimpleTable>
          </Section>
        ) : null}
        {billing ? (
          <Section title={t('customerBilling:dashboard.recentPayments')}>
            <SimpleTable
              head={[
                t('payments:fields.number'),
                t('payments:fields.receivedOn'),
                t('payments:fields.amount'),
              ]}
              empty={t('customerBilling:dashboard.none')}
              rows={d.recent_payments?.length ?? 0}
            >
              {(d.recent_payments ?? []).map((p) => (
                <TableRow key={p.id}>
                  <TableCell>
                    <Link
                      to="/m/$marinaId/payments/$paymentId"
                      params={{ marinaId, paymentId: p.id }}
                      className="underline-offset-4 hover:underline"
                    >
                      {p.number}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <IsoDate date={p.received_on} />
                  </TableCell>
                  <TableCell>
                    <Money cents={p.amount_cents} />
                  </TableCell>
                </TableRow>
              ))}
            </SimpleTable>
          </Section>
        ) : null}
        <Section title={t('customerBilling:dashboard.documents')}>
          <SimpleTable
            head={[t('customerBilling:dashboard.document'), t('customerBilling:dashboard.expires')]}
            empty={t('customerBilling:dashboard.none')}
            rows={d.documents_expiring.length}
          >
            {d.documents_expiring.map((doc) => (
              <TableRow key={doc.id}>
                <TableCell>{doc.type}</TableCell>
                <TableCell>
                  <IsoDate date={doc.expires_on} />
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
        <Section title={t('customerBilling:dashboard.communications')}>
          <SimpleTable
            head={[t('customerBilling:dashboard.message'), t('customerBilling:dashboard.status')]}
            empty={t('customerBilling:dashboard.none')}
            rows={d.communications.length}
          >
            {d.communications.map((m) => (
              <TableRow key={m.id}>
                <TableCell>
                  {m.template} · {formatDateTime(m.created_at, i18n.language)}
                </TableCell>
                <TableCell>{m.status}</TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
      </div>
      {d.notes ? (
        <Section title={t('customerBilling:dashboard.notes')}>
          <p className="text-sm whitespace-pre-line">{d.notes}</p>
        </Section>
      ) : null}
      {can('payments.collect') ? <SepaMandatesPanel customerId={customerId} /> : null}
      {collecting ? (
        <RecordPaymentDialog
          customer={{ id: customerId, label: customerName }}
          lockCustomer
          onClose={() => setCollecting(false)}
          onDone={() => toast.success(t('payments:record.done'))}
        />
      ) : null}
    </div>
  );
}

/** SEPA mandates of a customer: state, request a new one (when a provider supports it), revoke. */
export function SepaMandatesPanel({ customerId }: { customerId: string }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const online = useOnlinePaymentsEnabled(marinaId);
  const list = useSepaMandates(marinaId, customerId);
  const create = useCreateSepaMandate(marinaId);
  const revoke = useRevokeSepaMandate(marinaId);
  const [url, setUrl] = useState<string | null>(null);
  const items = list.data?.items ?? [];
  return (
    <Section
      title={t('customerBilling:sepa.title')}
      description={t('customerBilling:sepa.description')}
      action={
        online ? (
          <Button
            size="sm"
            variant="outline"
            disabled={create.isPending}
            onClick={() =>
              create.mutate(
                { customerId, body: { send: true } },
                { onSuccess: (r) => setUrl(r.setup_url) },
              )
            }
          >
            {t('customerBilling:sepa.request')}
          </Button>
        ) : undefined
      }
    >
      <SimpleTable
        head={[
          t('customerBilling:sepa.reference'),
          t('customerBilling:sepa.iban'),
          t('customerBilling:dashboard.status'),
          '',
        ]}
        empty={t('customerBilling:sepa.empty')}
        rows={items.length}
      >
        {items.map((m) => (
          <TableRow key={m.id} data-testid="sepa-mandate">
            <TableCell>{m.mandate_ref ?? ''}</TableCell>
            <TableCell>{m.iban_last4 ? `•••• ${m.iban_last4}` : ''}</TableCell>
            <TableCell>
              <BillingBadge group="mandateStatus" value={m.status} />
            </TableCell>
            <TableCell className="text-right">
              {m.status === 'active' || m.status === 'pending' ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={revoke.isPending}
                  onClick={() => revoke.mutate({ customerId, mandateId: m.id })}
                >
                  {t('customerBilling:sepa.revoke')}
                </Button>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      {url ? <PayLinkField url={url} /> : null}
      <BillingProblemAlert error={create.error ?? revoke.error} />
    </Section>
  );
}

/** Tab "Estratto conto" (m5-plan §4.5): period, rows with running balance, PDF and CSV. */
export function CustomerStatementTab({ customerId }: { customerId: string }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const [from, setFrom] = useState<string | null>(`${today.slice(0, 4)}-01-01`);
  const [to, setTo] = useState<string | null>(today);
  const range = { ...(from ? { from } : {}), ...(to ? { to } : {}) };
  const statement = useCustomerStatement(marinaId, customerId, range);
  const file = useCustomerStatementFile(marinaId);
  const s = statement.data;
  return (
    <div className="flex flex-col gap-4" data-testid="customer-statement">
      <div className="flex flex-wrap items-end gap-3">
        <Field className="w-auto">
          <FieldLabel htmlFor="st-from">{t('customerBilling:statement.from')}</FieldLabel>
          <DatePicker id="st-from" value={from} onChange={setFrom} />
        </Field>
        <Field className="w-auto">
          <FieldLabel htmlFor="st-to">{t('customerBilling:statement.to')}</FieldLabel>
          <DatePicker id="st-to" value={to} onChange={setTo} />
        </Field>
        <Button
          variant="outline"
          disabled={file.isPending}
          onClick={() => file.mutate({ customerId, range, format: 'pdf' }, { onSuccess: openBlob })}
        >
          <DownloadIcon />
          {t('customerBilling:statement.pdf')}
        </Button>
        <Button
          variant="outline"
          disabled={file.isPending}
          onClick={() =>
            file.mutate(
              { customerId, range, format: 'csv' },
              { onSuccess: (b) => saveBlob(`estratto-conto-${today}.csv`, b) },
            )
          }
        >
          <DownloadIcon />
          {t('customerBilling:statement.csv')}
        </Button>
      </div>
      <BillingProblemAlert error={file.error} />
      {statement.isPending ? (
        <LoadingState rows={5} />
      ) : statement.isError || !s ? (
        <QueryErrorState error={statement.error} onRetry={() => void statement.refetch()} />
      ) : (
        <>
          <section className="grid gap-2 sm:grid-cols-2">
            <Stat label={t('customerBilling:statement.opening')}>
              <Money cents={s.opening_cents} />
            </Stat>
            <Stat label={t('customerBilling:statement.closing')} testId="statement-closing">
              <Money cents={s.closing_cents} />
            </Stat>
          </section>
          <SimpleTable
            head={[
              t('customerBilling:statement.date'),
              t('customerBilling:statement.kind'),
              t('customerBilling:statement.number'),
              t('customerBilling:statement.dueOn'),
              t('customerBilling:statement.debit'),
              t('customerBilling:statement.credit'),
              t('customerBilling:statement.balance'),
            ]}
            empty={t('customerBilling:statement.empty')}
            rows={s.rows.length}
          >
            {s.rows.map((r, i) => (
              <TableRow key={`${r.kind}-${r.id ?? i}`} data-testid="statement-row">
                <TableCell>
                  <IsoDate date={r.date} />
                </TableCell>
                <TableCell>
                  {t(`billingEnums:statementKind.${r.kind}`)}
                  {r.description ? (
                    <span className="text-muted-foreground block text-xs">{r.description}</span>
                  ) : null}
                </TableCell>
                <TableCell>{r.number}</TableCell>
                <TableCell>
                  <IsoDate date={r.due_on} />
                </TableCell>
                <TableCell>{r.debit_cents ? <Money cents={r.debit_cents} /> : null}</TableCell>
                <TableCell>{r.credit_cents ? <Money cents={r.credit_cents} /> : null}</TableCell>
                <TableCell className="font-medium">
                  <Money cents={r.balance_cents} />
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </>
      )}
    </div>
  );
}
