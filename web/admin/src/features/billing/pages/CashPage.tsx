import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  useAddCashMovement,
  useCashRegisters,
  useCashSessions,
  useCloseCashSession,
  useCounterSale,
  useCurrentCashSession,
  useOpenCashSession,
  useZReport,
  type CashSession,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
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
import { PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { CustomerField, type CustomerChoice } from '../../desk/components/CustomerField';
import {
  BillingBadge,
  BillingProblemAlert,
  EnumSelect,
  IsoDate,
  LoadMore,
  Money,
  PageFrame,
  Section,
  SimpleTable,
  Stat,
} from '../components/kit';
import { LinesEditor } from '../components/LinesEditor';
import { COUNTED_METHODS, countedBody, sessionDifference, type CountedMethod } from '../lib/cash';
import { type LineDraft, emptyLine, linesValid, toLineInputs } from '../lib/drafts';
import { openBlob } from '../lib/files';
import { useReceiptOpener } from '../lib/receipt';

/** Cash desk (m5-plan §5.1, §9.1): session per register, counter sale, movements, close wizard, Z report. */
export function CashPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const registers = useCashRegisters(marinaId);
  const [registerId, setRegisterId] = useState<string>('');
  const list = registers.data?.items.filter((r) => r.active) ?? [];
  const current = registerId || list.find((r) => r.is_default)?.id || list[0]?.id || '';
  return (
    <RequirePermission allowed={can('payments.collect') || can('payments.collect_cash')}>
      <PageFrame>
        <PageHeader title={t('cash:title')} description={t('cash:description')} />
        {list.length > 1 ? (
          <EnumSelect
            id="cash-register"
            label={t('cash:register')}
            values={list.map((r) => r.id)}
            labelOf={(id) => list.find((r) => r.id === id)?.name ?? id}
            value={current}
            onChange={(v) => setRegisterId(v)}
          />
        ) : null}
        <Tabs defaultValue="session">
          <TabsList>
            <TabsTrigger value="session">{t('cash:tabs.session')}</TabsTrigger>
            <TabsTrigger value="history">{t('cash:tabs.history')}</TabsTrigger>
          </TabsList>
          <TabsContent value="session">
            {registers.isPending ? (
              <LoadingState />
            ) : registers.isError ? (
              <QueryErrorState error={registers.error} />
            ) : (
              <SessionPanel registerId={current} />
            )}
          </TabsContent>
          <TabsContent value="history">
            <History registerId={current} />
          </TabsContent>
        </Tabs>
      </PageFrame>
    </RequirePermission>
  );
}

function SessionPanel({ registerId }: { registerId: string }) {
  const { marinaId } = useCurrentMarina();
  const session = useCurrentCashSession(marinaId, registerId || undefined);
  // The close wizard lives here, not in the open-session view: once the session is closed the current session
  // query turns empty and the view unmounts, while the wizard must stay open to show the result and the Z report.
  const [closing, setClosing] = useState<CashSession | null>(null);
  const wizard = closing ? (
    <CloseSessionDialog session={closing} onClose={() => setClosing(null)} />
  ) : null;
  if (session.isPending) return <LoadingState rows={4} />;
  if (session.isError) return <QueryErrorState error={session.error} />;
  if (!session.data)
    return (
      <>
        <OpenSession registerId={registerId} />
        {wizard}
      </>
    );
  return (
    <>
      <OpenSessionView session={session.data} onCloseSession={setClosing} />
      {wizard}
    </>
  );
}

function OpenSession({ registerId }: { registerId: string }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const open = useOpenCashSession(marinaId);
  const [float, setFloat] = useState<number | null>(0);
  return (
    <Section title={t('cash:open.title')} description={t('cash:open.description')}>
      <div className="flex flex-wrap items-end gap-3">
        <Field className="w-48">
          <FieldLabel htmlFor="cash-float">{t('cash:open.float')}</FieldLabel>
          <MoneyInput
            id="cash-float"
            locale={i18n.language}
            value={float}
            onValueChange={setFloat}
          />
        </Field>
        <Button
          disabled={open.isPending || float === null || float < 0}
          onClick={() =>
            open.mutate({
              float_cents: float ?? 0,
              ...(registerId ? { register_id: registerId } : {}),
            })
          }
        >
          {t('cash:open.submit')}
        </Button>
      </div>
      <BillingProblemAlert error={open.error} />
    </Section>
  );
}

function OpenSessionView({
  session,
  onCloseSession,
}: {
  session: CashSession;
  onCloseSession: (s: CashSession) => void;
}) {
  const { t, i18n } = useT();
  const [dialog, setDialog] = useState<'sale' | 'movement' | null>(null);
  const close = () => setDialog(null);
  const s = session.summary;
  return (
    <div className="flex flex-col gap-4" data-testid="cash-session">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm">
          {t('cash:session.openedAt', {
            at: formatDateTime(session.opened_at, i18n.language),
          })}{' '}
          · <IsoDate date={session.business_date} />
        </p>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => setDialog('sale')}>{t('cash:sale.open')}</Button>
          <Button variant="outline" onClick={() => setDialog('movement')}>
            {t('cash:movement.open')}
          </Button>
          <Button variant="outline" onClick={() => onCloseSession(session)}>
            {t('cash:close.open')}
          </Button>
        </div>
      </div>
      <section aria-label={t('cash:session.totals')} className="grid gap-2 sm:grid-cols-4">
        <Stat label={t('cash:session.float')}>
          <Money cents={session.float_cents} />
        </Stat>
        <Stat label={t('cash:session.payments')} testId="cash-payments">
          <Money cents={s.payments_cents} />
        </Stat>
        <Stat label={t('cash:session.refunds')}>
          <Money cents={s.refunds_cents} />
        </Stat>
        <Stat label={t('cash:session.movements')}>
          <Money cents={s.movements_in_cents - s.movements_out_cents} />
        </Stat>
      </section>
      <Section title={t('cash:session.byMethod')}>
        <SimpleTable
          head={[t('cash:fields.method'), t('cash:fields.expected')]}
          empty={t('cash:session.noPayments')}
          rows={s.expected.length}
        >
          {s.expected.map((e) => (
            <TableRow key={e.method} data-testid="cash-expected">
              <TableCell>{t(`billingEnums:paymentMethod.${e.method}`)}</TableCell>
              <TableCell>
                <Money cents={e.amount_cents} />
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      </Section>
      <Section title={t('cash:session.movementsTitle')}>
        <SimpleTable
          head={[t('cash:fields.kind'), t('cash:fields.amount'), t('cash:fields.reason')]}
          empty={t('cash:session.noMovements')}
          rows={session.movements?.length ?? 0}
        >
          {(session.movements ?? []).map((m) => (
            <TableRow key={m.id}>
              <TableCell>{t(`billingEnums:movementKind.${m.kind}`)}</TableCell>
              <TableCell>
                <Money cents={m.amount_cents} />
              </TableCell>
              <TableCell>{m.reason}</TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      </Section>
      {dialog === 'sale' ? <CounterSaleDialog session={session} onClose={close} /> : null}
      {dialog === 'movement' ? <MovementDialog session={session} onClose={close} /> : null}
    </div>
  );
}

const RECEIPTS = ['print', 'email', 'none'] as const;
const PRINT_FORMATS = ['80', '58', 'a4'] as const;

/**
 * Vendita al banco (m5-plan §5.3): item lines, optional customer, method, commercial document toggle (required
 * without a customer, D23), receipt printed (80/58 mm or A4) or emailed.
 */
export function CounterSaleDialog({
  session,
  onClose,
}: {
  session: Pick<CashSession, 'register_id'>;
  onClose: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const sale = useCounterSale(marinaId);
  const receipt = useReceiptOpener(marinaId);
  const [lines, setLines] = useState<LineDraft[]>(() => [emptyLine()]);
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [method, setMethod] = useState<CountedMethod>('cash');
  const [fiscal, setFiscal] = useState(true);
  const [mode, setMode] = useState<(typeof RECEIPTS)[number]>('print');
  const [format, setFormat] = useState<(typeof PRINT_FORMATS)[number]>('80');
  const fiscalRequired = !customer;
  const valid =
    linesValid(lines) && (!fiscalRequired || fiscal) && (mode !== 'email' || !!customer);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-4xl" data-testid="counter-sale-dialog">
        <DialogHeader>
          <DialogTitle>{t('cash:sale.title')}</DialogTitle>
          <DialogDescription>{t('cash:sale.description')}</DialogDescription>
        </DialogHeader>
        <LinesEditor marinaId={marinaId} lines={lines} onChange={setLines} idPrefix="sale" />
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="sale-customer">{t('cash:sale.customer')}</FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="sale-customer"
              value={customer}
              onChange={setCustomer}
            />
          </Field>
          <EnumSelect
            id="sale-method"
            label={t('cash:fields.method')}
            values={COUNTED_METHODS}
            labelOf={(m) => t(`billingEnums:paymentMethod.${m}`)}
            value={method}
            onChange={(m) => m && setMethod(m)}
          />
          <EnumSelect
            id="sale-receipt"
            label={t('cash:sale.receipt')}
            values={RECEIPTS}
            labelOf={(m) => t(`cash:sale.receipts.${m}`)}
            value={mode}
            onChange={(m) => m && setMode(m)}
          />
          {mode === 'print' ? (
            <EnumSelect
              id="sale-format"
              label={t('cash:sale.format')}
              values={PRINT_FORMATS}
              labelOf={(f) => t(`payments:receipt.${f === 'a4' ? 'a4' : `mm${f}`}`)}
              value={format}
              onChange={(f) => f && setFormat(f)}
            />
          ) : null}
        </div>
        <label className="flex items-center gap-2 text-sm">
          <Checkbox
            checked={fiscal || fiscalRequired}
            disabled={fiscalRequired}
            onCheckedChange={(v) => setFiscal(v === true)}
          />
          {t('cash:sale.fiscalDocument')}
        </label>
        {fiscalRequired ? (
          <p className="text-muted-foreground text-xs">{t('cash:sale.walkInHint')}</p>
        ) : null}
        <p className="text-muted-foreground text-xs">{t('cash:sale.notFiscal')}</p>
        <BillingProblemAlert error={sale.error ?? receipt.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={!valid || sale.isPending}
            onClick={() =>
              sale.mutate(
                {
                  lines: toLineInputs(lines),
                  payment: { method, register_id: session.register_id },
                  fiscal_document: fiscal || fiscalRequired,
                  receipt: mode,
                  ...(customer ? { customer_id: customer.id } : {}),
                },
                {
                  onSuccess: (r) => {
                    toast.success(t('cash:sale.done', { number: r.payment.number }));
                    if (mode === 'print') receipt.open(r.payment.id, format);
                    onClose();
                  },
                },
              )
            }
          >
            {t('cash:sale.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MovementDialog({ session, onClose }: { session: CashSession; onClose: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const add = useAddCashMovement(marinaId);
  const [kind, setKind] = useState<'in' | 'out'>('out');
  const [amount, setAmount] = useState<number | null>(null);
  const [reason, setReason] = useState('');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('cash:movement.title')}</DialogTitle>
          <DialogDescription>{t('cash:movement.description')}</DialogDescription>
        </DialogHeader>
        <EnumSelect
          id="mov-kind"
          label={t('cash:fields.kind')}
          values={['in', 'out'] as const}
          labelOf={(k) => t(`billingEnums:movementKind.${k}`)}
          value={kind}
          onChange={(k) => k && setKind(k)}
        />
        <Field>
          <FieldLabel htmlFor="mov-amount">{t('cash:fields.amount')}</FieldLabel>
          <MoneyInput
            id="mov-amount"
            locale={i18n.language}
            value={amount}
            onValueChange={setAmount}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="mov-reason">{t('cash:fields.reason')}</FieldLabel>
          <Input
            id="mov-reason"
            maxLength={300}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <BillingProblemAlert error={add.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={add.isPending || !amount || amount <= 0 || !reason.trim()}
            onClick={() =>
              add.mutate(
                {
                  sessionId: session.id,
                  body: { kind, amount_cents: amount ?? 0, reason: reason.trim() },
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

/** Close wizard: counted amount per method, live difference against the expected, notes; then the Z report. */
export function CloseSessionDialog({
  session,
  onClose,
}: {
  session: CashSession;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const closeSession = useCloseCashSession(marinaId);
  const z = useZReport(marinaId);
  const [counted, setCounted] = useState<Partial<Record<CountedMethod, number | null>>>({});
  const [notes, setNotes] = useState('');
  const [closed, setClosed] = useState<CashSession | null>(null);
  const diff = useMemo(
    () => sessionDifference(session.summary.expected, counted),
    [session.summary.expected, counted],
  );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl" data-testid="close-session-dialog">
        <DialogHeader>
          <DialogTitle>{t('cash:close.title')}</DialogTitle>
          <DialogDescription>{t('cash:close.description')}</DialogDescription>
        </DialogHeader>
        {closed ? (
          <div className="flex flex-col gap-3">
            <p role="status" className="text-sm">
              {t('cash:close.done')} <Money cents={closed.summary.difference_cents} />
            </p>
            <div className="flex gap-2">
              <Button
                variant="outline"
                disabled={z.isPending}
                onClick={() =>
                  z.mutate(
                    { sessionId: closed.id, format: 'html' },
                    {
                      onSuccess: (b) =>
                        openBlob(new Blob([b], { type: 'text/html;charset=utf-8' })),
                    },
                  )
                }
              >
                {t('cash:z.html')}
              </Button>
              <Button
                variant="outline"
                disabled={z.isPending}
                onClick={() =>
                  z.mutate({ sessionId: closed.id, format: 'pdf' }, { onSuccess: openBlob })
                }
              >
                {t('cash:z.pdf')}
              </Button>
            </div>
          </div>
        ) : (
          <>
            <SimpleTable
              head={[
                t('cash:fields.method'),
                t('cash:fields.expected'),
                t('cash:fields.counted'),
                t('cash:fields.difference'),
              ]}
              empty=""
              rows={diff.rows.length}
            >
              {diff.rows.map((r) => (
                <TableRow key={r.method} data-testid="close-row">
                  <TableCell>{t(`billingEnums:paymentMethod.${r.method}`)}</TableCell>
                  <TableCell>
                    <Money cents={r.expected} />
                  </TableCell>
                  <TableCell className="w-36">
                    <MoneyInput
                      aria-label={t('cash:close.countedOf', {
                        method: t(`billingEnums:paymentMethod.${r.method}`),
                      })}
                      locale={i18n.language}
                      value={counted[r.method] ?? null}
                      onValueChange={(v) => setCounted({ ...counted, [r.method]: v })}
                    />
                  </TableCell>
                  <TableCell>
                    <Money cents={r.difference} />
                  </TableCell>
                </TableRow>
              ))}
            </SimpleTable>
            <p className="text-sm font-medium" data-testid="close-difference">
              {t('cash:close.total')} <Money cents={diff.total} />
            </p>
            <Field>
              <FieldLabel htmlFor="close-notes">{t('cash:close.notes')}</FieldLabel>
              <Input
                id="close-notes"
                maxLength={500}
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
              />
            </Field>
          </>
        )}
        <BillingProblemAlert error={closeSession.error ?? z.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          {closed ? null : (
            <Button
              disabled={closeSession.isPending}
              onClick={() =>
                closeSession.mutate(
                  {
                    id: session.id,
                    version: session.version,
                    body: {
                      counted: countedBody(diff),
                      ...(notes.trim() ? { notes: notes.trim() } : {}),
                    },
                  },
                  { onSuccess: setClosed },
                )
              }
            >
              {t('cash:close.submit')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function History({ registerId }: { registerId: string }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const list = useCashSessions(marinaId, {
    ...(registerId ? { register_id: registerId } : {}),
    status: 'closed',
  });
  const z = useZReport(marinaId);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  if (list.isPending) return <LoadingState />;
  if (list.isError) return <QueryErrorState error={list.error} />;
  return (
    <div className="flex flex-col gap-3">
      <SimpleTable
        head={[
          t('cash:fields.businessDate'),
          t('cash:fields.closedAt'),
          t('cash:session.payments'),
          t('cash:fields.difference'),
          t('cash:fields.status'),
          '',
        ]}
        empty={t('cash:history.empty')}
        rows={items.length}
      >
        {items.map((s) => (
          <TableRow key={s.id} data-testid="cash-history-row">
            <TableCell>
              <IsoDate date={s.business_date} />
            </TableCell>
            <TableCell>{s.closed_at ? formatDateTime(s.closed_at, i18n.language) : ''}</TableCell>
            <TableCell>
              <Money cents={s.summary.payments_cents} />
            </TableCell>
            <TableCell>
              <Money cents={s.summary.difference_cents} />
            </TableCell>
            <TableCell>
              <BillingBadge group="sessionStatus" value={s.status} />
            </TableCell>
            <TableCell className="text-right">
              <Button
                size="sm"
                variant="outline"
                disabled={z.isPending}
                onClick={() =>
                  z.mutate({ sessionId: s.id, format: 'pdf' }, { onSuccess: openBlob })
                }
              >
                {t('cash:z.pdf')}
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      <BillingProblemAlert error={z.error} />
      <LoadMore query={list} />
    </div>
  );
}
