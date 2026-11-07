import { useState } from 'react';

import {
  useCancelPaymentRequest,
  usePaymentRequests,
  useSendPaymentRequest,
  type PaymentRequest,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import {
  BillingBadge,
  BillingProblemAlert,
  EnumSelect,
  LoadMore,
  Money,
  PageFrame,
  SimpleTable,
} from '../components/kit';
import { PayLinkField } from '../components/PaymentRequestDialog';

const STATUSES: readonly PaymentRequest['status'][] = ['open', 'paid', 'cancelled', 'expired'];

/** Payment requests (m5-plan §5.5, §9.1): status, resend (optionally with a new link), cancel, copy link. */
export function PaymentRequestsPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<PaymentRequest['status'] | ''>('open');
  const list = usePaymentRequests(marinaId, status ? { status } : {});
  const send = useSendPaymentRequest(marinaId);
  const cancel = useCancelPaymentRequest(marinaId);
  const [link, setLink] = useState<string | null>(null);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <RequirePermission allowed={can('charges.manage')}>
      <PageFrame>
        <PageHeader
          title={t('receivables:requests.title')}
          description={t('receivables:requests.description')}
        />
        <EnumSelect
          id="req-status"
          label={t('receivables:fields.status')}
          values={STATUSES}
          labelOf={(s) => t(`billingEnums:requestStatus.${s}`)}
          value={status}
          onChange={setStatus}
          allLabel={t('receivables:all')}
        />
        <BillingProblemAlert error={send.error ?? cancel.error} />
        {list.isPending ? (
          <LoadingState rows={5} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('receivables:fields.customer'),
              t('receivables:requests.charges'),
              t('receivables:fields.amount'),
              t('receivables:requests.source'),
              t('receivables:fields.status'),
              t('receivables:requests.sentAt'),
              '',
            ]}
            empty={t('receivables:requests.empty')}
            rows={items.length}
          >
            {items.map((r) => (
              <TableRow key={r.id} data-testid="payment-request-row">
                <TableCell>{r.customer.display_name ?? ''}</TableCell>
                <TableCell>{r.charges.map((c) => c.number).join(', ')}</TableCell>
                <TableCell>
                  <Money cents={r.amount_cents} />
                </TableCell>
                <TableCell>{t(`billingEnums:requestSource.${r.source}`)}</TableCell>
                <TableCell>
                  <BillingBadge group="requestStatus" value={r.status} />
                </TableCell>
                <TableCell>
                  {r.sent_at ? `${formatDateTime(r.sent_at, i18n.language)} (${r.sent_count})` : ''}
                </TableCell>
                <TableCell className="flex justify-end gap-2">
                  {r.status === 'open' ? (
                    <>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={send.isPending}
                        onClick={() => send.mutate({ id: r.id, version: r.version })}
                      >
                        {t('receivables:requests.resend')}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={send.isPending}
                        onClick={() =>
                          send.mutate(
                            { id: r.id, version: r.version, rotate: true },
                            { onSuccess: (c) => c.url && setLink(c.url) },
                          )
                        }
                      >
                        {t('receivables:requests.newLink')}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={cancel.isPending}
                        onClick={() => cancel.mutate({ id: r.id, version: r.version })}
                      >
                        {t('receivables:requests.cancel')}
                      </Button>
                    </>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        <LoadMore query={list} />
        {link ? (
          <Dialog open onOpenChange={(o) => !o && setLink(null)}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>{t('receivables:request.link')}</DialogTitle>
                <DialogDescription>{t('receivables:request.linkOnce')}</DialogDescription>
              </DialogHeader>
              <PayLinkField url={link} />
              <DialogFooter>
                <Button variant="outline" onClick={() => setLink(null)}>
                  {t('common:actions.close')}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}
