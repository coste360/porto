import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

import {
  useAcceptQuote,
  useDeclineQuote,
  useMarkQuoteSent,
  useQuote,
  type PlanChoice,
  type Quote,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
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
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { AcceptPlanFields, SendQuoteDialog } from '../../contracts-m4/public';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { draftFromQuote, expiryCountdown } from '../lib/quote';
import { ManualConfirmFields } from '../transits/NewTransitDialog';
import { QuoteEditor } from './QuoteEditor';

const route = getRouteApi('/auth/m/$marinaId/quotes/$quoteId');

export function QuotePage() {
  const { marinaId, can } = useCurrentMarina();
  const { quoteId } = route.useParams();
  const quote = useQuote(marinaId, quoteId);
  return (
    <RequirePermission allowed={can('contracts.read')}>
      {quote.isPending ? (
        <LoadingState rows={6} />
      ) : quote.isError ? (
        <QueryErrorState error={quote.error} onRetry={() => void quote.refetch()} />
      ) : (
        <QuoteDetail marinaId={marinaId} quote={quote.data} canWrite={can('contracts.write')} />
      )}
    </RequirePermission>
  );
}

function QuoteDetail({
  marinaId,
  quote,
  canWrite,
}: {
  marinaId: string;
  quote: Quote;
  canWrite: boolean;
}) {
  const { t, i18n } = useT();
  const [editing, setEditing] = useState(false);
  const [dialog, setDialog] = useState<'accept' | 'decline' | 'send' | null>(null);
  const send = useMarkQuoteSent(marinaId);
  const open = quote.status === 'draft' || quote.status === 'sent';
  const left = expiryCountdown(quote.expires_at, new Date());

  if (editing)
    return (
      <>
        <PageHeader title={t('desk:quote.editTitle', { code: quote.code })} />
        <QuoteEditor
          marinaId={marinaId}
          quote={quote}
          initial={draftFromQuote(quote)}
          initialCustomer={{ id: quote.customer.id, label: quote.customer.display_name ?? '' }}
          initialVessel={{
            kind: 'existing',
            id: quote.vessel.id,
            label: quote.vessel.name,
            dims: {
              loa_cm: quote.vessel.loa_cm,
              beam_cm: quote.vessel.beam_cm,
              hull_type: quote.vessel.hull_type,
            },
          }}
          onCancel={() => setEditing(false)}
          onSaved={() => setEditing(false)}
        />
      </>
    );

  return (
    <>
      <PageHeader
        title={quote.code}
        description={`${quote.vessel.name}${quote.customer.display_name ? ` · ${quote.customer.display_name}` : ''}`}
        actions={
          canWrite && open ? (
            <>
              {quote.status === 'draft' ? (
                <>
                  <Button variant="outline" onClick={() => setEditing(true)}>
                    {t('common:actions.edit')}
                  </Button>
                  <Button
                    variant="outline"
                    disabled={send.isPending}
                    onClick={() => send.mutate({ id: quote.id, version: quote.version })}
                  >
                    {t('desk:quote.markSent')}
                  </Button>
                </>
              ) : null}
              <Button variant="outline" onClick={() => setDialog('send')}>
                {quote.status === 'draft'
                  ? t('contractsM4:quote.send')
                  : t('contractsM4:quote.resend')}
              </Button>
              <Button onClick={() => setDialog('accept')}>{t('desk:quote.accept')}</Button>
              <Button variant="outline" onClick={() => setDialog('decline')}>
                {t('desk:quote.decline')}
              </Button>
            </>
          ) : null
        }
      />
      <DeskProblemAlert error={send.error} />
      <div className="mb-4 flex flex-wrap items-center gap-3 text-sm">
        <Badge variant="secondary">{t(`occupancyEnums:quoteStatus.${quote.status}`)}</Badge>
        <span>
          {quote.term
            ? t(`occupancyEnums:contractTerm.${quote.term}`)
            : t(`occupancyEnums:quoteKind.${quote.kind}`)}
        </span>
        <span>{`${formatIsoDate(quote.starts_on)} – ${formatIsoDate(quote.ends_on)}`}</span>
        {open ? (
          <span data-testid="desk-quote-expiry">
            {left
              ? t('desk:quote.expiresIn', { days: left.days, hours: left.hours })
              : t('desk:quote.expiresPast')}
          </span>
        ) : null}
        {quote.contract_id ? (
          <Link
            to="/m/$marinaId/contracts/$contractId"
            params={{ marinaId, contractId: quote.contract_id }}
            className="underline-offset-4 hover:underline"
          >
            {t('desk:quote.openContract')}
          </Link>
        ) : null}
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        {quote.options.map((o) => (
          <Card key={o.id} data-testid="desk-quote-option-card">
            <CardHeader>
              <CardTitle>{o.label || t('desk:quote.option', { n: o.seq })}</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 text-sm">
              <ul>
                {o.placements.map((p) => (
                  <li key={p.seq}>
                    {t('desk:search.segment', {
                      code: p.resource_code ?? p.resource_id,
                      from: formatIsoDate(p.first_night),
                      to: formatIsoDate(p.last_night),
                    })}
                  </li>
                ))}
              </ul>
              <span className="text-lg font-semibold tabular-nums">
                {formatCents(o.total_cents, 'EUR', i18n.language)}
              </span>
              {o.deposit_cents > 0 ? (
                <span className="text-muted-foreground">
                  {t('desk:quote.deposit', {
                    amount: formatCents(o.deposit_cents, 'EUR', i18n.language),
                  })}
                </span>
              ) : null}
              <div className="flex flex-wrap gap-1">
                {o.hold ? (
                  <Badge
                    variant={o.placements.some((p) => p.allocation_id) ? 'default' : 'outline'}
                  >
                    {o.placements.some((p) => p.allocation_id)
                      ? t('desk:quote.holdActive')
                      : t('desk:quote.holdLost')}
                  </Badge>
                ) : null}
                {quote.accepted_option_id === o.id ? (
                  <Badge>{t('desk:quote.accepted')}</Badge>
                ) : null}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
      {dialog === 'accept' ? (
        <AcceptDialog marinaId={marinaId} quote={quote} onClose={() => setDialog(null)} />
      ) : null}
      {dialog === 'send' ? (
        <SendQuoteDialog marinaId={marinaId} quote={quote} onClose={() => setDialog(null)} />
      ) : null}
      {dialog === 'decline' ? (
        <DeclineDialog marinaId={marinaId} quote={quote} onClose={() => setDialog(null)} />
      ) : null}
    </>
  );
}

function AcceptDialog({
  marinaId,
  quote,
  onClose,
}: {
  marinaId: string;
  quote: Quote;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const navigate = useNavigate();
  const accept = useAcceptQuote(marinaId);
  const [optionId, setOptionId] = useState(quote.options[0]?.id ?? '');
  const [activate, setActivate] = useState(false);
  const [plan, setPlan] = useState<PlanChoice>('standard');
  const [confirm, setConfirm] = useState<{ reference: string; payAtDeparture: boolean } | null>(
    null,
  );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desk:quote.acceptTitle')}</DialogTitle>
          <DialogDescription>{t('desk:quote.acceptDescription')}</DialogDescription>
        </DialogHeader>
        <RadioGroup
          value={optionId}
          onValueChange={setOptionId}
          aria-label={t('desk:quote.chooseOption')}
        >
          {quote.options.map((o) => (
            <Field key={o.id} orientation="horizontal">
              <RadioGroupItem id={`accept-${o.id}`} value={o.id} />
              <FieldLabel htmlFor={`accept-${o.id}`}>
                {`${o.label || t('desk:quote.option', { n: o.seq })} · ${formatCents(o.total_cents, 'EUR', i18n.language)}`}
              </FieldLabel>
            </Field>
          ))}
        </RadioGroup>
        {quote.kind === 'mooring' ? (
          <Field orientation="horizontal">
            <Checkbox
              id="accept-activate"
              checked={activate}
              onCheckedChange={(c) => setActivate(c === true)}
            />
            <FieldLabel htmlFor="accept-activate">{t('desk:quote.activateNow')}</FieldLabel>
          </Field>
        ) : null}
        {quote.kind === 'mooring' ? (
          <AcceptPlanFields value={plan} onChange={setPlan} />
        ) : (
          <>
            <Field orientation="horizontal">
              <Checkbox
                id="accept-confirm"
                checked={confirm !== null}
                onCheckedChange={(c) =>
                  setConfirm(c === true ? { reference: '', payAtDeparture: false } : null)
                }
              />
              <FieldLabel htmlFor="accept-confirm">{t('desk:transit.confirmManual')}</FieldLabel>
            </Field>
            {confirm ? (
              <ManualConfirmFields idPrefix="accept" value={confirm} onChange={setConfirm} />
            ) : null}
          </>
        )}
        <DeskProblemAlert error={accept.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={!optionId || accept.isPending}
            onClick={() =>
              accept.mutate(
                {
                  id: quote.id,
                  version: quote.version,
                  body: {
                    option_id: optionId,
                    ...(quote.kind === 'mooring'
                      ? { activate, ...(plan !== 'standard' ? { plan } : {}) }
                      : {}),
                    ...(confirm
                      ? {
                          confirm: {
                            pay_at_departure: confirm.payAtDeparture,
                            ...(confirm.reference.trim()
                              ? { reference: confirm.reference.trim() }
                              : {}),
                          },
                        }
                      : {}),
                  },
                },
                {
                  onSuccess: (r) =>
                    void navigate(
                      r.contract.kind === 'transit'
                        ? {
                            to: '/m/$marinaId/transits/$contractId',
                            params: { marinaId, contractId: r.contract.id },
                          }
                        : {
                            to: '/m/$marinaId/contracts/$contractId',
                            params: { marinaId, contractId: r.contract.id },
                          },
                    ),
                },
              )
            }
          >
            {t('desk:quote.accept')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DeclineDialog({
  marinaId,
  quote,
  onClose,
}: {
  marinaId: string;
  quote: Quote;
  onClose: () => void;
}) {
  const { t } = useT();
  const decline = useDeclineQuote(marinaId);
  const [reason, setReason] = useState('');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desk:quote.declineTitle')}</DialogTitle>
          <DialogDescription>{t('desk:quote.declineDescription')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="decline-reason">{t('desk:fields.reason')}</FieldLabel>
          <Input
            id="decline-reason"
            value={reason}
            maxLength={500}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <DeskProblemAlert error={decline.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            variant="destructive"
            disabled={decline.isPending}
            onClick={() =>
              decline.mutate(
                {
                  id: quote.id,
                  version: quote.version,
                  ...(reason.trim() ? { reason: reason.trim() } : {}),
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('desk:quote.decline')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
