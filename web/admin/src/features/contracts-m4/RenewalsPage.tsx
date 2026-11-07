import { Link } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import { useRenewalCampaigns, type RenewalCampaignStatus } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { PageHeader } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { EnumSelect, IsoDate, LoadMore, PageFrame } from './components/kit';
import { campaignCounts } from './lib/campaign';

const STATUSES: readonly RenewalCampaignStatus[] = [
  'draft',
  'sending',
  'open',
  'closed',
  'discarded',
];

/** Renewal campaigns (spec 13.8): list by status and the entry to the campaign wizard. */
export function RenewalsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<RenewalCampaignStatus | ''>('');
  const list = useRenewalCampaigns(marinaId, status || undefined);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageFrame>
        <PageHeader
          title={t('renewals:list.title')}
          description={t('renewals:list.description')}
          actions={
            can('contracts.write') ? (
              <Button asChild>
                <Link
                  to="/m/$marinaId/renewals/$campaignId"
                  params={{ marinaId, campaignId: 'new' }}
                >
                  <PlusIcon />
                  {t('renewals:list.new')}
                </Link>
              </Button>
            ) : null
          }
        />
        <EnumSelect
          id="campaign-status"
          label={t('renewals:list.status')}
          group="campaignStatus"
          values={STATUSES}
          value={status}
          onChange={setStatus}
          allLabel={t('contractsM4:common.all')}
        />
        {list.isPending ? (
          <LoadingState rows={4} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('renewals:list.name')}</TableHead>
                  <TableHead>{t('renewals:list.status')}</TableHead>
                  <TableHead>{t('renewals:list.deadline')}</TableHead>
                  <TableHead>{t('renewals:list.counts')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="text-muted-foreground h-24 text-center">
                      {t('renewals:list.empty')}
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((c) => (
                    <TableRow key={c.id}>
                      <TableCell>
                        <Link
                          to="/m/$marinaId/renewals/$campaignId"
                          params={{ marinaId, campaignId: c.id }}
                          className="font-medium underline-offset-4 hover:underline"
                        >
                          {c.name}
                        </Link>
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary">
                          {t(`docsEnums:campaignStatus.${c.status}`)}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <IsoDate date={c.deadline_on} />
                      </TableCell>
                      <TableCell className="flex flex-wrap gap-1">
                        {Object.entries(campaignCounts(c.counts as Record<string, unknown>)).map(
                          ([k, n]) => (
                            <Badge key={k} variant="outline">
                              {`${t(`docsEnums:proposalStatus.${k}`)} ${n}`}
                            </Badge>
                          ),
                        )}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        )}
        <LoadMore query={list} />
      </PageFrame>
    </RequirePermission>
  );
}
