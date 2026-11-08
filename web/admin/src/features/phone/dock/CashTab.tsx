import { BanknoteIcon, ReceiptIcon } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { toast } from 'sonner';

import {
  ProblemError,
  newIdempotencyKey,
  unwrap,
  useApi,
  useCurrentCashSession,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { assertOnline, enqueue, useOnlineStatus, useQueue } from '@porto/shared/offline';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@porto/shared/ui/drawer';
import { Field, FieldDescription, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { MoneyInput } from '@porto/shared/ui/patterns';
import { OfflineReason } from '@porto/shared/ui/patterns/OfflineReason';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState } from '../../../components/PageStates';
import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';
import { useCollectibles } from '../api';
import { cashAmountError, cashOp, collectReason, queuedCharges } from '../lib/ops';

type Collectible = Schemas['Collectible'];

/**
 * Cassa of the dock (m6b-plan §6.2): the open session, today's collectibles and cash collection. Invoice-mode
 * charges are queued (offline too, D6); receipt-mode charges number a fiscal document, so they are sent directly
 * and only online. The receipt of a payment recorded here opens online, plus whatever the receiptAction slot adds.
 */
export function CashTab({
  date,
  receiptAction,
}: {
  date: string;
  receiptAction?: (paymentId: string) => ReactNode;
}) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const allowed = can('payments.collect') || can('payments.collect_cash');
  const { online } = useOnlineStatus();
  const queue = useQueue(marinaId);
  const session = useCurrentCashSession(marinaId, undefined);
  const collectibles = useCollectibles(marinaId, date, allowed);
  const [collecting, setCollecting] = useState<Collectible | null>(null);
  const [recorded, setRecorded] = useState<string[]>([]);
  if (!allowed)
    return <p className="text-muted-foreground text-sm">{t('phone:dock.cash.notAllowed')}</p>;
  const open = session.data ?? null;
  const queued = queuedCharges(queue.ops);
  const items = collectibles.data?.items ?? [];
  return (
    <div className="flex flex-col gap-4" data-testid="dock-cash">
      <section className="flex flex-col gap-1" data-testid="dock-cash-session">
        <h2 className="font-semibold">{t('phone:dock.cash.session')}</h2>
        {session.isPending ? (
          <LoadingState rows={1} />
        ) : open ? (
          <p className="text-sm">
            {t('phone:dock.cash.sessionOpen', { date: formatIsoDate(open.business_date) })}
            {open.register_code ? ` · ${open.register_code}` : ''}
          </p>
        ) : (
          <Alert>
            <AlertDescription>{t('phone:dock.cash.sessionNone')}</AlertDescription>
          </Alert>
        )}
      </section>
      <section className="flex flex-col gap-2">
        <h2 className="font-semibold">{t('phone:dock.cash.collectibles')}</h2>
        {collectibles.isPending ? (
          <LoadingState rows={3} />
        ) : collectibles.isError ? (
          <p className="text-muted-foreground text-sm">{t('phone:dock.cash.unavailable')}</p>
        ) : items.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('phone:dock.cash.none')}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {items.map((c) => {
              const reason = collectReason(c, online, !!open);
              const inQueue = queued.has(c.charge_id);
              return (
                <li
                  key={c.charge_id}
                  className="border-border flex flex-col gap-2 rounded-md border p-3"
                  data-testid={`dock-collectible-${c.charge_id}`}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex min-w-0 flex-col">
                      <span className="font-medium">{c.vessel.name}</span>
                      <span className="text-muted-foreground text-xs">
                        {[c.number, c.contract_code, c.description].filter(Boolean).join(' · ')}
                      </span>
                      <span className="text-muted-foreground text-xs">
                        {t('phone:dock.cash.due', { date: formatIsoDate(c.due_on) })}
                      </span>
                    </div>
                    <span className="font-medium tabular-nums">
                      {formatCents(c.residual_cents, 'EUR', i18n.language)}
                    </span>
                  </div>
                  {inQueue ? (
                    <Badge variant="secondary" data-testid="dock-collectible-queued">
                      {t('offline:queuedBadge')}
                    </Badge>
                  ) : (
                    <Button
                      className="min-h-11"
                      disabled={reason !== null}
                      onClick={() => setCollecting(c)}
                    >
                      <BanknoteIcon />
                      {t('phone:dock.collect')}
                    </Button>
                  )}
                  {reason === 'document' ? <OfflineReason reason="document" /> : null}
                  {c.fiscal_mode === 'receipt' && online ? (
                    <p className="text-muted-foreground text-xs">
                      {t('phone:dock.cash.receiptMode')}
                    </p>
                  ) : null}
                </li>
              );
            })}
          </ul>
        )}
      </section>
      {recorded.length > 0 ? (
        <section className="flex flex-col gap-2" data-testid="dock-cash-recorded">
          {recorded.map((id) => (
            <div key={id} className="flex flex-wrap items-center gap-2">
              {online ? (
                <Button variant="outline" className="min-h-11" asChild>
                  <a
                    href={`/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/payments/${encodeURIComponent(id)}/receipt?format=80`}
                    target="_blank"
                    rel="noopener"
                  >
                    <ReceiptIcon />
                    {t('phone:dock.cash.receiptLink')}
                  </a>
                </Button>
              ) : null}
              {receiptAction?.(id)}
            </div>
          ))}
        </section>
      ) : null}
      <CollectSheet
        charge={collecting}
        registerId={open?.register_id ?? ''}
        date={date}
        onClose={() => setCollecting(null)}
        onRecorded={(paymentId) => setRecorded((r) => [paymentId, ...r])}
      />
    </div>
  );
}

function CollectSheet({
  charge,
  registerId,
  date,
  onClose,
  onRecorded,
}: {
  charge: Collectible | null;
  registerId: string;
  date: string;
  onClose: () => void;
  onRecorded: (paymentId: string) => void;
}) {
  const { t } = useT();
  return (
    <Drawer open={charge !== null} onOpenChange={(o) => !o && onClose()}>
      <DrawerContent className="p-4" data-testid="dock-collect-sheet">
        {charge ? (
          <CollectForm
            key={charge.charge_id}
            charge={charge}
            registerId={registerId}
            date={date}
            onClose={onClose}
            onRecorded={onRecorded}
          />
        ) : (
          <DrawerTitle className="sr-only">{t('phone:dock.collect')}</DrawerTitle>
        )}
      </DrawerContent>
    </Drawer>
  );
}

function CollectForm({
  charge,
  registerId,
  date,
  onClose,
  onRecorded,
}: {
  charge: Collectible;
  registerId: string;
  date: string;
  onClose: () => void;
  onRecorded: (paymentId: string) => void;
}) {
  const { t, i18n } = useT();
  const api = useApi();
  const { marinaId, me } = useCurrentMarina();
  const [amount, setAmount] = useState<number | null>(charge.residual_cents);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [failed, setFailed] = useState(false);
  const amountError = cashAmountError(amount, charge.residual_cents);

  const submit = async () => {
    if (amountError || amount === null || !me) return;
    setBusy(true);
    setError(null);
    setFailed(false);
    const amountLabel = formatCents(amount, 'EUR', i18n.language);
    try {
      if (charge.fiscal_mode === 'receipt') {
        // Numbers a commercial document: online only, never through the offline queue (29.2, D6).
        assertOnline('recordPayment', 'document');
        const op = cashOp(
          { userId: me.user.id, marinaId },
          { charge, amountCents: amount, registerId, receivedOn: date, amountLabel },
        );
        const payment = await unwrap(
          api.POST('/api/admin/v1/marinas/{marinaId}/payments', {
            params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
            body: op.request.body as Schemas['RecordPayment'],
          }),
        );
        onRecorded(payment.id);
      } else {
        await enqueue(
          cashOp(
            { userId: me.user.id, marinaId },
            { charge, amountCents: amount, registerId, receivedOn: date, amountLabel },
          ),
        );
        toast.success(t('phone:dock.cash.queuedToast'));
      }
      onClose();
    } catch (e) {
      if (e instanceof ProblemError) setError(e);
      else setFailed(true);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <DrawerHeader className="px-0">
        <DrawerTitle>{t('phone:dock.cash.sheetTitle', { number: charge.number })}</DrawerTitle>
        <DrawerDescription>
          {charge.vessel.name} · {t('phone:dock.cash.residual')}{' '}
          {formatCents(charge.residual_cents, 'EUR', i18n.language)}
        </DrawerDescription>
      </DrawerHeader>
      <Field data-invalid={amountError ? true : undefined}>
        <FieldLabel htmlFor="dock-collect-amount">{t('phone:dock.cash.amount')}</FieldLabel>
        <MoneyInput
          id="dock-collect-amount"
          value={amount}
          onChange={setAmount}
          locale={i18n.language}
          className="min-h-11"
        />
        <FieldDescription>{t('phone:dock.cash.amountHint')}</FieldDescription>
        {amountError && amount !== null ? <FieldError>{t(amountError)}</FieldError> : null}
      </Field>
      <DeskProblemAlert error={error} />
      {failed ? (
        <p className="text-destructive text-sm" role="alert">
          {t('phone:enqueueFailed')}
        </p>
      ) : null}
      <DrawerFooter className="px-0">
        <Button type="submit" className="min-h-11" disabled={busy || amountError !== null}>
          {t('phone:dock.cash.submit')}
        </Button>
        <Button type="button" variant="outline" className="min-h-11" onClick={onClose}>
          {t('common:actions.cancel')}
        </Button>
      </DrawerFooter>
    </form>
  );
}
