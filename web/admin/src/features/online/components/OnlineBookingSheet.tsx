import { Link } from '@tanstack/react-router';
import { CheckIcon, ExternalLinkIcon, LinkIcon, XIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useApproveOnlineBooking,
  useDocumentFileUrl,
  useOnlineBooking,
  useRejectOnlineBooking,
  useSendOnlineBookingPaymentLink,
  useVerifyDocument,
  type OnlineBookingDetail,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatCm, formatDateTime, formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
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
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Label } from '@porto/shared/ui/label';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Separator } from '@porto/shared/ui/separator';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@porto/shared/ui/sheet';
import { Skeleton } from '@porto/shared/ui/skeleton';
import { Switch } from '@porto/shared/ui/switch';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { DeadlineLeft, OnlineStatusBadge } from './badges';

/** Detail sheet of an online booking: contact, vessel, documents, consents, payment and the desk actions. */
export function OnlineBookingSheet({
  marinaId,
  contractId,
  onClose,
}: {
  marinaId: string;
  contractId: string | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const q = useOnlineBooking(marinaId, contractId);
  return (
    <Sheet open={contractId !== null} onOpenChange={(o) => (o ? null : onClose())}>
      <SheetContent
        className="w-full overflow-y-auto sm:max-w-lg"
        data-testid="online-booking-sheet"
      >
        <SheetHeader>
          <SheetTitle>
            {q.data
              ? t('online:detail.title', { reference: q.data.reference })
              : t('online:nav.bookings')}
          </SheetTitle>
          <SheetDescription>
            {q.data
              ? `${formatIsoDate(q.data.arrival)} – ${formatIsoDate(q.data.departure)}`
              : null}
          </SheetDescription>
        </SheetHeader>
        <div className="flex flex-col gap-4 px-4">
          {q.isPending ? (
            <Skeleton className="h-64 w-full" />
          ) : q.isError ? (
            <ProblemAlert error={q.error} />
          ) : (
            <Detail marinaId={marinaId} booking={q.data} />
          )}
        </div>
        {q.data ? <Actions marinaId={marinaId} booking={q.data} /> : null}
      </SheetContent>
    </Sheet>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="text-right break-words">{children}</dd>
    </div>
  );
}

function Detail({ marinaId, booking: b }: { marinaId: string; booking: OnlineBookingDetail }) {
  const { t, i18n } = useT();
  const { can } = useCurrentMarina();
  const verify = useVerifyDocument(marinaId);
  const fileUrl = useDocumentFileUrl(marinaId);
  const lang = i18n.language;
  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <OnlineStatusBadge status={b.status} />
        <span className="text-muted-foreground text-sm">
          {b.channel_code ?? t(`online:channel.${b.channel}`)}
        </span>
      </div>
      {b.payment.capture_failed ? (
        <Alert variant="destructive">
          <AlertDescription>{t('online:detail.captureFailed')}</AlertDescription>
        </Alert>
      ) : null}
      <dl className="flex flex-col gap-2">
        {b.approval_reason ? (
          <Row label={t('online:detail.approvalReason')}>
            {t(`online:approvalReason.${b.approval_reason}`)}
          </Row>
        ) : null}
        {b.status === 'awaiting_approval' && b.approval_deadline ? (
          <Row label={t('online:detail.deadline')}>
            <DeadlineLeft at={b.approval_deadline} />
          </Row>
        ) : null}
        <Row label={t('online:columns.amount')}>{formatCents(b.total_cents, 'EUR', lang)}</Row>
        <Row label={t('online:detail.payment')}>
          {t(`online:payment.${b.payment.state}`)}
          {b.payment.amount_cents ? ` · ${formatCents(b.payment.amount_cents, 'EUR', lang)}` : ''}
        </Row>
        {b.approved_at ? (
          <p className="text-sm">
            {t('online:detail.approved', { at: formatDateTime(b.approved_at, lang) })}
          </p>
        ) : null}
        {b.rejected_at ? (
          <p className="text-sm">
            {t('online:detail.rejected', {
              at: formatDateTime(b.rejected_at, lang),
              reason: b.reject_reason ?? '',
            })}
          </p>
        ) : null}
      </dl>
      <Separator />
      <section className="flex flex-col gap-2">
        <h3 className="font-medium">{t('online:detail.contact')}</h3>
        {b.contact ? (
          <dl className="flex flex-col gap-1">
            <Row
              label={t('online:detail.contact')}
            >{`${b.contact.first_name} ${b.contact.last_name}`}</Row>
            <Row label={t('online:detail.email')}>{b.contact.email}</Row>
            {b.contact.phone ? <Row label={t('online:detail.phone')}>{b.contact.phone}</Row> : null}
          </dl>
        ) : (
          <p className="text-muted-foreground text-sm">{t('online:detail.contactHidden')}</p>
        )}
      </section>
      <section className="flex flex-col gap-2">
        <h3 className="font-medium">{t('online:detail.vessel')}</h3>
        <dl className="flex flex-col gap-1">
          <Row label={t('online:detail.vessel')}>
            {t('online:detail.dimensions', {
              loa: formatCm(b.loa_cm, lang),
              beam: formatCm(b.beam_cm, lang),
            })}
          </Row>
          {b.people_on_board ? (
            <Row label={t('online:detail.people')}>{b.people_on_board}</Row>
          ) : null}
          {b.expected_arrival_time ? (
            <Row label={t('online:detail.arrivalTime')}>{b.expected_arrival_time}</Row>
          ) : null}
          {b.notes ? <Row label={t('online:detail.notes')}>{b.notes}</Row> : null}
        </dl>
      </section>
      <Separator />
      <section className="flex flex-col gap-2">
        <h3 className="font-medium">{t('online:detail.documents')}</h3>
        {b.documents.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('online:detail.noDocuments')}</p>
        ) : (
          <ul className="flex flex-col gap-3">
            {b.documents.map((d) => {
              const id = `verify-${d.id}`;
              return (
                <li
                  key={d.id}
                  className="flex flex-wrap items-center justify-between gap-2 text-sm"
                >
                  <span>
                    {t(`online:documentTypes.${d.type}`, { defaultValue: d.type })}
                    {d.file_name ? ` · ${d.file_name}` : ''}
                  </span>
                  <span className="flex items-center gap-3">
                    {can('customers.read') ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={fileUrl.isPending}
                        onClick={() =>
                          fileUrl.mutate(d.id, {
                            onSuccess: (r) => window.open(r.url, '_blank', 'noopener'),
                          })
                        }
                      >
                        <ExternalLinkIcon />
                        {t('online:detail.preview')}
                      </Button>
                    ) : null}
                    <span className="flex items-center gap-2">
                      <Switch
                        id={id}
                        checked={Boolean(d.verified_at)}
                        disabled={!can('customers.write') || verify.isPending}
                        onCheckedChange={(v) => verify.mutate({ documentId: d.id, verified: v })}
                        data-testid="online-document-verify"
                      />
                      <Label htmlFor={id}>{t('online:detail.verified')}</Label>
                    </span>
                  </span>
                </li>
              );
            })}
          </ul>
        )}
        <ProblemAlert error={verify.error ?? fileUrl.error} />
      </section>
      <section className="flex flex-col gap-2">
        <h3 className="font-medium">{t('online:detail.consents')}</h3>
        {b.consents.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('online:detail.noConsents')}</p>
        ) : (
          <ul className="flex flex-col gap-1 text-sm">
            {b.consents.map((c) => (
              <li key={`${c.kind}-${c.version}`}>
                {t(`online:consentKind.${c.kind}`, { defaultValue: c.kind })}:{' '}
                {c.granted
                  ? t('online:detail.consentGranted', { version: c.version })
                  : t('online:detail.consentDenied', { version: c.version })}{' '}
                · {formatDateTime(c.recorded_at, lang)}
              </li>
            ))}
          </ul>
        )}
      </section>
      <Button variant="outline" asChild>
        <Link
          to="/m/$marinaId/transits/$contractId"
          params={{ marinaId, contractId: b.contract_id }}
        >
          {t('online:detail.openTransit')}
        </Link>
      </Button>
    </>
  );
}

function Actions({ marinaId, booking: b }: { marinaId: string; booking: OnlineBookingDetail }) {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const approve = useApproveOnlineBooking(marinaId);
  const link = useSendOnlineBookingPaymentLink(marinaId);
  const [rejecting, setRejecting] = useState(false);
  if (!can('transits.desk')) return null;
  const awaiting = b.status === 'awaiting_approval';
  const canLink = awaiting && Boolean(b.payment.capture_failed);
  if (!awaiting) return null;
  const v = { contractId: b.contract_id, version: b.version };
  return (
    <SheetFooter className="flex flex-col gap-2">
      <ProblemAlert error={approve.error ?? link.error} />
      <div className="flex flex-col gap-2 sm:flex-row">
        <Button
          onClick={() =>
            approve.mutate(v, { onSuccess: () => toast.success(t('online:actions.approved')) })
          }
          disabled={approve.isPending}
          data-testid="online-approve"
        >
          <CheckIcon />
          {t('online:actions.approve')}
        </Button>
        <Button
          variant="destructive"
          onClick={() => setRejecting(true)}
          data-testid="online-reject"
        >
          <XIcon />
          {t('online:actions.reject')}
        </Button>
        {canLink ? (
          <Button
            variant="outline"
            onClick={() =>
              link.mutate(v, {
                onSuccess: () => toast.success(t('online:actions.paymentLinkSent')),
              })
            }
            disabled={link.isPending}
          >
            <LinkIcon />
            {t('online:actions.paymentLink')}
          </Button>
        ) : null}
      </div>
      <RejectDialog marinaId={marinaId} booking={b} open={rejecting} onOpenChange={setRejecting} />
    </SheetFooter>
  );
}

function RejectDialog({
  marinaId,
  booking: b,
  open,
  onOpenChange,
}: {
  marinaId: string;
  booking: OnlineBookingDetail;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  const { t } = useT();
  const reject = useRejectOnlineBooking(marinaId);
  const [reason, setReason] = useState('');
  const [notify, setNotify] = useState(true);
  const submit = () =>
    reject.mutate(
      { contractId: b.contract_id, version: b.version, body: { reason: reason.trim(), notify } },
      {
        onSuccess: () => {
          toast.success(t('online:actions.rejected'));
          onOpenChange(false);
        },
      },
    );
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('online:actions.rejectTitle')}</DialogTitle>
          <DialogDescription>{b.reference}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="online-reject-reason">
              {t('online:actions.rejectReason')}
            </FieldLabel>
            <Textarea
              id="online-reject-reason"
              value={reason}
              maxLength={500}
              onChange={(e) => setReason(e.target.value)}
            />
          </Field>
          <Field orientation="horizontal">
            <Checkbox
              id="online-reject-notify"
              checked={notify}
              onCheckedChange={(c) => setNotify(c === true)}
            />
            <FieldLabel htmlFor="online-reject-notify">{t('online:actions.notify')}</FieldLabel>
          </Field>
        </FieldGroup>
        <ProblemAlert error={reject.error} />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('online:actions.cancel')}
          </Button>
          <Button
            variant="destructive"
            onClick={submit}
            disabled={reject.isPending || reason.trim() === ''}
            data-testid="online-reject-confirm"
          >
            {t('online:actions.rejectConfirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
