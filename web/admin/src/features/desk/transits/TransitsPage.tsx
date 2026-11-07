import { PlusIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';

import { useTransits } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DataTable, PageHeader } from '@porto/shared/ui/patterns';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { useContractColumns } from '../contracts/useContractColumns';
import { transitTabFilters, type TransitTab } from '../lib/transit';
import { NewTransitDialog } from './NewTransitDialog';

export function TransitsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [tab, setTab] = useState<TransitTab>('today');
  const [q, setQ] = useState('');
  const [creating, setCreating] = useState(false);
  const deferred = useDeferredValue(q.trim());
  const transits = useTransits(marinaId, { ...transitTabFilters(tab, today), q: deferred });
  const columns = useContractColumns('transit');
  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageHeader
        title={t('desk:transits.title')}
        description={t('desk:transits.description')}
        actions={
          can('transits.desk') ? (
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              {t('desk:transits.new')}
            </Button>
          ) : null
        }
      />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <ToggleGroup
          type="single"
          variant="outline"
          value={tab}
          onValueChange={(v) => v && setTab(v as TransitTab)}
          aria-label={t('desk:transits.title')}
        >
          <ToggleGroupItem value="today">{t('desk:transits.tabs.today')}</ToggleGroupItem>
          <ToggleGroupItem value="upcoming">{t('desk:transits.tabs.upcoming')}</ToggleGroupItem>
          <ToggleGroupItem value="inPort">{t('desk:transits.tabs.inPort')}</ToggleGroupItem>
          <ToggleGroupItem value="all">{t('desk:transits.tabs.all')}</ToggleGroupItem>
        </ToggleGroup>
        <Field className="sm:max-w-sm">
          <FieldLabel htmlFor="transits-q">{t('desk:filters.search')}</FieldLabel>
          <Input
            id="transits-q"
            type="search"
            value={q}
            maxLength={64}
            onChange={(e) => setQ(e.target.value)}
          />
        </Field>
      </div>
      {transits.isPending ? (
        <LoadingState rows={6} />
      ) : transits.isError ? (
        <QueryErrorState error={transits.error} onRetry={() => void transits.refetch()} />
      ) : (
        <div className="flex flex-col gap-3">
          <DataTable
            columns={columns}
            data={transits.data.pages.flatMap((p) => p.items)}
            getRowId={(r) => r.id}
            empty={t('desk:transits.empty')}
          />
          {transits.hasNextPage ? (
            <Button
              variant="outline"
              onClick={() => void transits.fetchNextPage()}
              disabled={transits.isFetchingNextPage}
            >
              {t('common:actions.loadMore')}
            </Button>
          ) : null}
        </div>
      )}
      <NewTransitDialog marinaId={marinaId} open={creating} onOpenChange={setCreating} />
    </RequirePermission>
  );
}
