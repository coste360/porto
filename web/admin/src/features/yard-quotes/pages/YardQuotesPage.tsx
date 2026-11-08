import { Link } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import { useYardQuotes, type YardQuoteStatus } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, IsoDate, Money, PageFrame } from '../../contracts-m4/components/kit';
import { statusVariant } from '../components/status';

const STATUSES: readonly YardQuoteStatus[] = ['draft', 'sent', 'accepted', 'declined', 'expired'];

/** Yard quotes (m10-plan §14.1): status, number, total and validity; the wizard starts from here. */
export function YardQuotesPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<YardQuoteStatus | ''>('');
  const list = useYardQuotes(marinaId, status ? { status } : {});
  const items = list.data?.items ?? [];
  return (
    <RequirePermission allowed={can('yard.read') || can('yard.manage')}>
      <PageFrame>
        <div data-testid="yard-quotes-page" className="flex flex-col gap-4">
          <PageHeader
            title={t('yardQuotes:title')}
            description={t('yardQuotes:description')}
            actions={
              can('yard.manage') ? (
                <Button asChild>
                  <Link to="/m/$marinaId/yard/quotes/new" params={{ marinaId }}>
                    <PlusIcon />
                    {t('yardQuotes:new')}
                  </Link>
                </Button>
              ) : null
            }
          />
          <EnumSelect
            id="yq-status"
            label={t('yardQuotes:fields.status')}
            values={STATUSES}
            value={status}
            onChange={setStatus}
            labelOf={(s) => t(`yardQuotes:statuses.${s}`)}
            allLabel={t('yardQuotes:all')}
          />
          {list.isPending ? (
            <LoadingState rows={4} />
          ) : list.isError ? (
            <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
          ) : (
            <SimpleTable
              head={[
                t('yardQuotes:fields.number'),
                t('yardQuotes:fields.status'),
                t('yardQuotes:fields.quotedOn'),
                t('yardQuotes:fields.validUntil'),
                t('yardQuotes:totals.gross'),
              ]}
              empty={t('yardQuotes:empty')}
              rows={items.length}
            >
              {items.map((q) => (
                <TableRow key={q.id} data-testid="yard-quote-row">
                  <TableCell>
                    <Link
                      to="/m/$marinaId/yard/quotes/$quoteId"
                      params={{ marinaId, quoteId: q.id }}
                      className="font-medium underline-offset-4 hover:underline"
                    >
                      {q.number}
                    </Link>
                    {q.kind === 'variation' ? (
                      <Badge variant="outline" className="ml-2">
                        {t('yardQuotes:variation')}
                      </Badge>
                    ) : null}
                  </TableCell>
                  <TableCell>
                    <Badge variant={statusVariant(q.status)}>
                      {t(`yardQuotes:statuses.${q.status}`)}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <IsoDate date={q.quoted_on} />
                  </TableCell>
                  <TableCell>
                    <IsoDate date={q.valid_until} />
                  </TableCell>
                  <TableCell>
                    <Money cents={q.totals.gross_cents} />
                  </TableCell>
                </TableRow>
              ))}
            </SimpleTable>
          )}
        </div>
      </PageFrame>
    </RequirePermission>
  );
}
