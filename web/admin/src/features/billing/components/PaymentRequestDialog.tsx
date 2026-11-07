import { CopyIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import { useCreatePaymentRequest, type PaymentRequestCreated } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
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

import { useCurrentMarina } from '../../../auth/hooks';
import { BillingProblemAlert, Money } from './kit';

/** Copies a text to the clipboard with a toast (the pay link is shown once). */
export function CopyButton({ text, label }: { text: string; label: string }) {
  const { t } = useT();
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      onClick={() =>
        void navigator.clipboard
          ?.writeText(text)
          .then(() => toast.success(t('paymentProviders:copied')))
      }
    >
      <CopyIcon />
      {label}
    </Button>
  );
}

/** The link of a created request, shown once (only its hash is stored). */
export function PayLinkField({ url }: { url: string }) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-2" data-testid="pay-link">
      <Field>
        <FieldLabel htmlFor="pay-link-url">{t('receivables:request.link')}</FieldLabel>
        <Input id="pay-link-url" readOnly value={url} />
      </Field>
      <div>
        <CopyButton text={url} label={t('receivables:request.copy')} />
      </div>
      <p className="text-muted-foreground text-xs">{t('receivables:request.linkOnce')}</p>
    </div>
  );
}

/** "Richiesta di pagamento" for charges of one customer: email with bank details and, if enabled, the online link. */
export function PaymentRequestDialog({
  customerId,
  chargeIds,
  totalCents,
  onClose,
}: {
  customerId: string;
  chargeIds: string[];
  totalCents: number;
  onClose: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreatePaymentRequest(marinaId);
  const [send, setSend] = useState(true);
  const [expires, setExpires] = useState<string | null>(null);
  const [created, setCreated] = useState<PaymentRequestCreated | null>(null);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent data-testid="payment-request-dialog">
        <DialogHeader>
          <DialogTitle>{t('receivables:request.title')}</DialogTitle>
          <DialogDescription>
            {t('receivables:request.description', { count: chargeIds.length })}{' '}
            <Money cents={totalCents} />
          </DialogDescription>
        </DialogHeader>
        {created ? (
          <>
            <p role="status" className="text-sm">
              {created.request.sent_count > 0
                ? t('receivables:request.sent')
                : t('receivables:request.created')}
            </p>
            {created.url ? <PayLinkField url={created.url} /> : null}
          </>
        ) : (
          <>
            <Field>
              <FieldLabel htmlFor="req-expires">{t('receivables:request.expires')}</FieldLabel>
              <DatePicker id="req-expires" value={expires} onChange={setExpires} />
            </Field>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={send} onCheckedChange={(v) => setSend(v === true)} />
              {t('receivables:request.send')}
            </label>
          </>
        )}
        <BillingProblemAlert error={create.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          {created ? null : (
            <Button
              disabled={create.isPending}
              onClick={() =>
                create.mutate(
                  {
                    customer_id: customerId,
                    charge_ids: chargeIds,
                    send,
                    ...(expires ? { expires_on: expires } : {}),
                  },
                  { onSuccess: setCreated },
                )
              }
            >
              {t('receivables:request.submit')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
