import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import {
  isProblem,
  newIdempotencyKey,
  useChangeAccountDates,
  usePreviewAccountDateChange,
  type AccountBooking,
  type DateChangePreview,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@porto/shared/ui/dialog';
import { Field, FieldError, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Spinner } from '@porto/shared/ui/spinner';

import { zodFormResolver } from '../common/formResolver';

const iso = /^\d{4}-\d{2}-\d{2}$/;
const schema = z
  .object({
    arrival: z.string().regex(iso, 'common:validation.required'),
    departure: z.string().regex(iso, 'common:validation.required'),
  })
  .refine((v) => v.departure > v.arrival, {
    path: ['departure'],
    message: 'account:changeDates.invalid',
  });

type Values = z.infer<typeof schema>;

/**
 * "Cambia date": the preview shows availability and the price effect (supplement to pay with "Paga saldo", or a
 * refund per the cancellation tiers); the change sends the total the customer saw (409 price_changed otherwise).
 */
export function ChangeDatesDialog({
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
  const [preview, setPreview] = useState<(DateChangePreview & Values) | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const previewM = usePreviewAccountDateChange(slug, session, booking.contract_id);
  const change = useChangeAccountDates(slug, session, booking.contract_id);
  const form = useForm<Values>({
    resolver: zodFormResolver(schema),
    defaultValues: { arrival: booking.arrival, departure: booking.departure },
  });
  const money = (c: number) => formatCents(c, 'EUR', i18n.language);

  const runPreview = form.handleSubmit((v) => {
    setPreview(null);
    change.reset();
    previewM.mutate(v, { onSuccess: (p) => setPreview({ ...p, ...v }) });
  });

  const confirm = () => {
    if (!preview) return;
    change.mutate(
      {
        body: {
          arrival: preview.arrival,
          departure: preview.departure,
          expected_total_cents: preview.new_total_cents,
        },
        idempotencyKey: key,
      },
      {
        onSuccess: () => setOpen(false),
        onError: (e) => {
          if (isProblem(e, 'price_changed')) {
            setKey(newIdempotencyKey());
            previewM.mutate(
              { arrival: preview.arrival, departure: preview.departure },
              {
                onSuccess: (p) =>
                  setPreview({ ...p, arrival: preview.arrival, departure: preview.departure }),
              },
            );
          }
        },
      },
    );
  };

  const errors = form.formState.errors;
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) {
          setPreview(null);
          previewM.reset();
          change.reset();
        }
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" data-testid="account-change-dates">
          {t('account:changeDates.button')}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('account:changeDates.title')}</DialogTitle>
          <DialogDescription className="sr-only">
            {t('account:changeDates.title')}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={runPreview} className="flex flex-col gap-4" noValidate>
          <FieldGroup className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field data-invalid={Boolean(errors.arrival)}>
              <FieldLabel htmlFor="change-arrival">{t('account:changeDates.arrival')}</FieldLabel>
              <Input id="change-arrival" type="date" {...form.register('arrival')} />
              {errors.arrival?.message ? (
                <FieldError>{t(errors.arrival.message)}</FieldError>
              ) : null}
            </Field>
            <Field data-invalid={Boolean(errors.departure)}>
              <FieldLabel htmlFor="change-departure">
                {t('account:changeDates.departure')}
              </FieldLabel>
              <Input id="change-departure" type="date" {...form.register('departure')} />
              {errors.departure?.message ? (
                <FieldError>{t(errors.departure.message)}</FieldError>
              ) : null}
            </Field>
          </FieldGroup>
          <Button type="submit" variant="outline" disabled={previewM.isPending}>
            {previewM.isPending ? <Spinner /> : null}
            {t('account:changeDates.preview')}
          </Button>
        </form>
        <ProblemAlert error={previewM.error} />
        {preview ? (
          preview.available ? (
            <dl className="flex flex-col gap-1 text-sm" data-testid="account-dates-preview">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t('account:changeDates.oldTotal')}</dt>
                <dd>{money(preview.old_total_cents)}</dd>
              </div>
              <div className="flex justify-between font-medium">
                <dt>{t('account:changeDates.newTotal')}</dt>
                <dd>{money(preview.new_total_cents)}</dd>
              </div>
              {preview.supplement_cents > 0 ? (
                <p>
                  {t('account:changeDates.supplement', { amount: money(preview.supplement_cents) })}
                </p>
              ) : null}
              {preview.refund_cents > 0 ? (
                <p>{t('account:changeDates.refund', { amount: money(preview.refund_cents) })}</p>
              ) : null}
            </dl>
          ) : (
            <Alert>
              <AlertDescription>
                {preview.reason
                  ? t(`account:blockReason.${preview.reason}`)
                  : t('account:changeDates.unavailable')}
              </AlertDescription>
            </Alert>
          )
        ) : null}
        {isProblem(change.error, 'price_changed') ? (
          <Alert>
            <AlertDescription>{t('account:changeDates.changed')}</AlertDescription>
          </Alert>
        ) : (
          <ProblemAlert error={change.error} />
        )}
        <DialogFooter>
          <Button
            onClick={confirm}
            disabled={!preview?.available || change.isPending || previewM.isPending}
          >
            {change.isPending ? <Spinner /> : null}
            {t('account:changeDates.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
