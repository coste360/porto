import { getRouteApi } from '@tanstack/react-router';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useAcceptYardQuote,
  useDeclineYardQuote,
  useRecomputeYardQuote,
  useSendYardQuote,
  useYardQualifications,
  useYardQuote,
  yardQuotePdfUrl,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import {
  Detail,
  DetailGrid,
  IsoDate,
  M4ProblemAlert,
  PageFrame,
  ReasonDialog,
  Section,
} from '../../contracts-m4/components/kit';
import { QuoteLines, QuoteTotals } from '../components/QuoteLines';
import { statusVariant } from '../components/status';

const route = getRouteApi('/auth/m/$marinaId/yard/quotes/$quoteId');

/** A yard quote (m10-plan §14.1): lines with the manual badge, totals, VAT, hours; send, accept, decline, PDF and
 * Ricalcola on drafts. */
export function YardQuotePage() {
  const { t } = useT();
  const { quoteId } = route.useParams();
  const { marinaId, can } = useCurrentMarina();
  const q = useYardQuote(marinaId, quoteId);
  const quals = useYardQualifications(marinaId);
  const send = useSendYardQuote(marinaId);
  const accept = useAcceptYardQuote(marinaId);
  const decline = useDeclineYardQuote(marinaId);
  const recompute = useRecomputeYardQuote(marinaId);
  const [declining, setDeclining] = useState(false);
  const [link, setLink] = useState<string | null>(null);
  const manage = can('yard.manage');
  return (
    <RequirePermission allowed={can('yard.read') || manage}>
      <PageFrame>
        {q.isPending ? (
          <LoadingState rows={6} />
        ) : q.isError ? (
          <QueryErrorState error={q.error} onRetry={() => void q.refetch()} />
        ) : (
          <div className="flex flex-col gap-4" data-testid="yard-quote-page">
            <PageHeader
              title={t('yardQuotes:quoteTitle', { number: q.data.number })}
              actions={
                <div className="flex flex-wrap gap-2">
                  <Button variant="outline" asChild>
                    <a href={yardQuotePdfUrl(marinaId, q.data.id)} target="_blank" rel="noreferrer">
                      {t('yardQuotes:actions.pdf')}
                    </a>
                  </Button>
                  {manage && q.data.status === 'draft' ? (
                    <Button
                      variant="outline"
                      disabled={recompute.isPending}
                      onClick={() =>
                        recompute.mutate(
                          { id: q.data.id, version: q.data.version },
                          { onSuccess: () => toast.success(t('yardQuotes:actions.recomputed')) },
                        )
                      }
                    >
                      {t('yardQuotes:actions.recompute')}
                    </Button>
                  ) : null}
                  {manage && (q.data.status === 'draft' || q.data.status === 'sent') ? (
                    <>
                      <Button
                        disabled={send.isPending || q.data.blocking}
                        onClick={() =>
                          send.mutate(
                            { id: q.data.id, version: q.data.version },
                            {
                              onSuccess: (r) => {
                                setLink(r.link_url ?? null);
                                toast.success(t('yardQuotes:actions.sentToast'));
                              },
                            },
                          )
                        }
                      >
                        {t('yardQuotes:actions.send')}
                      </Button>
                      <Button
                        variant="secondary"
                        disabled={accept.isPending || q.data.blocking}
                        onClick={() =>
                          accept.mutate(
                            { id: q.data.id, version: q.data.version },
                            {
                              onSuccess: () => toast.success(t('yardQuotes:actions.acceptedToast')),
                            },
                          )
                        }
                      >
                        {t('yardQuotes:actions.accept')}
                      </Button>
                      <Button variant="ghost" onClick={() => setDeclining(true)}>
                        {t('yardQuotes:actions.decline')}
                      </Button>
                    </>
                  ) : null}
                </div>
              }
            />
            <DetailGrid>
              <Detail label={t('yardQuotes:fields.status')}>
                <Badge variant={statusVariant(q.data.status)} data-testid="yard-quote-status">
                  {t(`yardQuotes:statuses.${q.data.status}`)}
                </Badge>
              </Detail>
              <Detail label={t('yardQuotes:fields.quotedOn')}>
                <IsoDate date={q.data.quoted_on} />
              </Detail>
              <Detail label={t('yardQuotes:fields.validUntil')}>
                <IsoDate date={q.data.valid_until} />
              </Detail>
              <Detail label={t('yardQuotes:fields.billingMode')}>
                {t(`yardQuotes:billingModes.${q.data.billing_mode}`)}
              </Detail>
              {q.data.planned_start_on ? (
                <Detail label={t('yardQuotes:fields.plannedStart')}>
                  <IsoDate date={q.data.planned_start_on} />
                </Detail>
              ) : null}
              {q.data.work_order_id ? (
                <Detail label={t('yardQuotes:fields.workOrder')}>{q.data.work_order_id}</Detail>
              ) : null}
            </DetailGrid>
            {link ? (
              <p className="text-sm break-all" data-testid="yard-quote-link">
                {t('yardQuotes:actions.link')} {link}
              </p>
            ) : null}
            <M4ProblemAlert
              error={send.error ?? accept.error ?? decline.error ?? recompute.error}
            />
            <Section title={t('yardQuotes:lines.title')}>
              <QuoteLines lines={q.data.lines} />
            </Section>
            <Section title={t('yardQuotes:totals.title')}>
              <QuoteTotals preview={q.data} qualifications={quals.data?.items ?? []} />
            </Section>
            {q.data.conditions ? (
              <Section title={t('yardQuotes:fields.conditions')}>
                <p className="text-sm whitespace-pre-line">{q.data.conditions}</p>
              </Section>
            ) : null}
            <ReasonDialog
              open={declining}
              title={t('yardQuotes:actions.decline')}
              confirmLabel={t('yardQuotes:actions.decline')}
              pending={decline.isPending}
              error={decline.error}
              destructive
              onClose={() => setDeclining(false)}
              onConfirm={(reason) =>
                decline.mutate(
                  { id: q.data.id, version: q.data.version, ...(reason ? { reason } : {}) },
                  { onSuccess: () => setDeclining(false) },
                )
              }
            />
          </div>
        )}
      </PageFrame>
    </RequirePermission>
  );
}
