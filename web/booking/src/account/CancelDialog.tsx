import { useState } from 'react';

import {
  isProblem,
  newIdempotencyKey,
  useAccountCancellation,
  useCancelAccountBooking,
  type AccountBooking,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp, formatCents } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@porto/shared/ui/alert-dialog';
import { Button } from '@porto/shared/ui/button';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Skeleton } from '@porto/shared/ui/skeleton';
import { Spinner } from '@porto/shared/ui/spinner';

/**
 * "Annulla" with the refund preview from the contract's policy snapshot. The confirmation sends the refund the
 * customer saw; when it moved (409 refund_changed) the preview reloads and the customer confirms again.
 */
export function CancelDialog({
  slug,
  session,
  booking,
}: {
  slug: string;
  session: string;
  booking: AccountBooking;
}) {
  const { t, i18n } = useT();
  const [open, setOpen] = useState(false);
  const [key, setKey] = useState(newIdempotencyKey);
  const preview = useAccountCancellation(slug, session, booking.contract_id, open);
  const cancel = useCancelAccountBooking(slug, session, booking.contract_id);
  const changed = isProblem(cancel.error, 'refund_changed');
  const money = (c: number) => formatCents(c, 'EUR', i18n.language);

  const confirm = () => {
    if (!preview.data) return;
    cancel.mutate(
      { body: { expected_refund_cents: preview.data.refund_cents }, idempotencyKey: key },
      {
        onSuccess: () => setOpen(false),
        onError: (e) => {
          if (isProblem(e, 'refund_changed')) {
            setKey(newIdempotencyKey());
            void preview.refetch();
          }
        },
      },
    );
  };

  const p = preview.data;
  return (
    <AlertDialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) cancel.reset();
      }}
    >
      <AlertDialogTrigger asChild>
        <Button variant="outline" data-testid="account-cancel">
          {t('account:cancel.button')}
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent data-testid="account-cancel-dialog">
        <AlertDialogHeader>
          <AlertDialogTitle>{t('account:cancel.title')}</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="flex flex-col gap-2">
              {preview.isPending ? (
                <Skeleton className="h-12 w-full" />
              ) : preview.isError ? (
                <ProblemAlert error={preview.error} />
              ) : p && !p.allowed ? (
                <span>{t('account:cancel.notAllowed')}</span>
              ) : p ? (
                <>
                  <span>{t('account:cancel.paid', { amount: money(p.paid_cents) })}</span>
                  <strong data-testid="account-refund">
                    {p.refund_cents === 0
                      ? t('account:cancel.noRefund')
                      : p.refund_as === 'credit'
                        ? t('account:cancel.credit', { amount: money(p.refund_cents) })
                        : t('account:cancel.refund', { amount: money(p.refund_cents) })}
                  </strong>
                  {p.tier ? (
                    <span>
                      {t('account:cancel.tier', {
                        days: p.tier.min_days_before,
                        percent: formatBp(p.tier.refund_bp, i18n.language),
                      })}
                    </span>
                  ) : null}
                </>
              ) : null}
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        {changed ? (
          <Alert data-testid="account-refund-changed">
            <AlertDescription>{t('account:cancel.changed')}</AlertDescription>
          </Alert>
        ) : (
          <ProblemAlert error={cancel.error} />
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('account:cancel.keep')}</AlertDialogCancel>
          <Button
            variant="destructive"
            onClick={confirm}
            disabled={!p?.allowed || cancel.isPending || preview.isFetching}
            data-testid="account-cancel-confirm"
          >
            {cancel.isPending ? <Spinner /> : null}
            {t('account:cancel.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
