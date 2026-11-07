import { Link } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';

import { useContracts, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DataTable, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useContractColumns } from './useContractColumns';

const ALL = '__all__';
const STATUSES = ['draft', 'active', 'expired', 'terminated', 'cancelled'] as const;
const TERMS = ['annual', 'seasonal', 'monthly', 'period'] as const;

export function ContractsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<Schemas['ContractStatus'] | ''>('');
  const [term, setTerm] = useState<Schemas['ContractTerm'] | ''>('');
  const [q, setQ] = useState('');
  const deferred = useDeferredValue(q.trim());
  const contracts = useContracts(marinaId, {
    ...(status ? { status } : {}),
    ...(term ? { term } : {}),
    q: deferred,
  });
  const columns = useContractColumns('mooring');
  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageHeader
        title={t('desk:contracts.title')}
        description={t('desk:contracts.description')}
        actions={
          can('contracts.write') ? (
            <Button asChild>
              <Link to="/m/$marinaId/contracts/new" params={{ marinaId }}>
                <PlusIcon />
                {t('desk:contracts.new')}
              </Link>
            </Button>
          ) : null
        }
      />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end">
        <Field className="sm:max-w-sm">
          <FieldLabel htmlFor="contracts-q">{t('desk:filters.search')}</FieldLabel>
          <Input
            id="contracts-q"
            type="search"
            value={q}
            maxLength={64}
            onChange={(e) => setQ(e.target.value)}
          />
        </Field>
        <Field className="sm:max-w-48">
          <FieldLabel htmlFor="contracts-status">{t('desk:filters.status')}</FieldLabel>
          <Select
            value={status || ALL}
            onValueChange={(v) => setStatus(v === ALL ? '' : (v as typeof status))}
          >
            <SelectTrigger id="contracts-status" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('desk:filters.all')}</SelectItem>
              {STATUSES.map((s) => (
                <SelectItem key={s} value={s}>
                  {t(`occupancyEnums:contractStatus.${s}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field className="sm:max-w-48">
          <FieldLabel htmlFor="contracts-term">{t('desk:fields.term')}</FieldLabel>
          <Select
            value={term || ALL}
            onValueChange={(v) => setTerm(v === ALL ? '' : (v as typeof term))}
          >
            <SelectTrigger id="contracts-term" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('desk:filters.all')}</SelectItem>
              {TERMS.map((s) => (
                <SelectItem key={s} value={s}>
                  {t(`occupancyEnums:contractTerm.${s}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>
      {contracts.isPending ? (
        <LoadingState rows={6} />
      ) : contracts.isError ? (
        <QueryErrorState error={contracts.error} onRetry={() => void contracts.refetch()} />
      ) : (
        <div className="flex flex-col gap-3">
          <DataTable
            columns={columns}
            data={contracts.data.pages.flatMap((p) => p.items)}
            getRowId={(r) => r.id}
            empty={t('desk:contracts.empty')}
          />
          {contracts.hasNextPage ? (
            <Button
              variant="outline"
              onClick={() => void contracts.fetchNextPage()}
              disabled={contracts.isFetchingNextPage}
            >
              {t('common:actions.loadMore')}
            </Button>
          ) : null}
        </div>
      )}
    </RequirePermission>
  );
}
