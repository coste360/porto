import { useInfiniteQuery } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { useState } from 'react';

import { queryKeys, unwrap, useApi, type AuditEntry } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DataTable, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../components/PageStates';

const ACTOR_TYPES = ['user', 'api_key', 'device', 'system'];

export function AuditPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const [entityType, setEntityType] = useState('');
  return (
    <RequirePermission allowed={can('audit.read')}>
      <PageHeader title={t('audit:title')} description={t('audit:description')} />
      <Field className="mb-4 max-w-xs">
        <FieldLabel htmlFor="audit-entity">{t('audit:entityFilter')}</FieldLabel>
        <Input
          id="audit-entity"
          value={entityType}
          onChange={(e) => setEntityType(e.target.value.trim())}
          placeholder={t('audit:allEntities')}
        />
      </Field>
      <AuditTable entityType={entityType} />
    </RequirePermission>
  );
}

function AuditTable({ entityType }: { entityType: string }) {
  const { t, i18n } = useT();
  const api = useApi();
  const { marinaId } = useCurrentMarina();
  const filters = entityType ? { entity_type: entityType } : {};
  const query = useInfiniteQuery({
    queryKey: queryKeys.audit(marinaId, filters),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/audit-log', {
          params: {
            path: { marinaId },
            query: { limit: 50, ...filters, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });

  if (query.isPending) return <LoadingState rows={6} />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const columns: ColumnDef<AuditEntry, unknown>[] = [
    {
      header: t('audit:when'),
      cell: ({ row }) => formatDateTime(row.original.created_at, i18n.language),
    },
    {
      header: t('audit:actor'),
      cell: ({ row }) => {
        const type = row.original.actor.type;
        const label = ACTOR_TYPES.includes(type) ? t(`audit:actorTypes.${type}`) : type;
        return (
          <div className="flex flex-col">
            <span>{label}</span>
            {row.original.actor.id ? (
              <code className="text-muted-foreground font-mono text-xs">
                {row.original.actor.id}
              </code>
            ) : null}
          </div>
        );
      },
    },
    {
      header: t('audit:action'),
      cell: ({ row }) => <code className="font-mono text-xs">{row.original.action}</code>,
    },
    {
      header: t('audit:entity'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span>{row.original.entity_type}</span>
          <code className="text-muted-foreground font-mono text-xs">{row.original.entity_id}</code>
        </div>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <DataTable
        columns={columns}
        data={query.data.pages.flatMap((p) => p.items)}
        getRowId={(e) => e.id}
        empty={t('audit:empty')}
      />
      {query.hasNextPage ? (
        <Button
          variant="outline"
          onClick={() => void query.fetchNextPage()}
          disabled={query.isFetchingNextPage}
        >
          {t('common:actions.loadMore')}
        </Button>
      ) : null}
    </div>
  );
}
