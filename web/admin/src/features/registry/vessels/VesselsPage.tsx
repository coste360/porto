import { Link } from '@tanstack/react-router';
import type { ColumnDef } from '@tanstack/react-table';
import { PlusIcon, SearchIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';

import { useCustomers, useVessels, type VesselSummary } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { InputGroup, InputGroupAddon, InputGroupInput } from '@porto/shared/ui/input-group';
import { DataTable, ElementCombobox, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { HULL_TYPES } from '../lib/enums';
import { customerName } from '../lib/format';

const ALL = '__all__';
type HullType = (typeof HULL_TYPES)[number];

export function VesselsPage() {
  const { t } = useT();
  const { can, marinaId } = useCurrentMarina();
  const [q, setQ] = useState('');
  const [hullType, setHullType] = useState<HullType | ''>('');
  const [owner, setOwner] = useState<{ id: string; label: string } | null>(null);
  const [ownerQ, setOwnerQ] = useState('');
  const [archived, setArchived] = useState(false);
  const deferredQ = useDeferredValue(q.trim());
  const deferredOwnerQ = useDeferredValue(ownerQ.trim());
  const owners = useCustomers(marinaId, { q: deferredOwnerQ });
  const ownerOptions = (owners.data?.pages.flatMap((p) => p.items) ?? []).map((c) => ({
    value: c.id,
    label: customerName(t, c),
  }));
  if (owner && !ownerOptions.some((o) => o.value === owner.id)) {
    ownerOptions.unshift({ value: owner.id, label: owner.label });
  }

  return (
    <RequirePermission allowed={can('customers.read')}>
      <PageHeader
        title={t('vessels:title')}
        description={t('vessels:description')}
        actions={
          can('customers.write') ? (
            <Button asChild>
              <Link to="/m/$marinaId/vessels/new" params={{ marinaId }}>
                <PlusIcon />
                {t('vessels:new')}
              </Link>
            </Button>
          ) : null
        }
      />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end">
        <Field className="sm:max-w-sm">
          <FieldLabel htmlFor="vessels-search">{t('vessels:search')}</FieldLabel>
          <InputGroup>
            <InputGroupAddon>
              <SearchIcon />
            </InputGroupAddon>
            <InputGroupInput
              id="vessels-search"
              type="search"
              value={q}
              maxLength={100}
              placeholder={t('vessels:searchPlaceholder')}
              onChange={(e) => setQ(e.target.value)}
            />
          </InputGroup>
        </Field>
        <Field className="sm:max-w-48">
          <FieldLabel htmlFor="vessels-hull">{t('vessels:filters.hullType')}</FieldLabel>
          <Select
            value={hullType || ALL}
            onValueChange={(v) => setHullType(v === ALL ? '' : (v as HullType))}
          >
            <SelectTrigger id="vessels-hull" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('vessels:filters.allHullTypes')}</SelectItem>
              {HULL_TYPES.map((h) => (
                <SelectItem key={h} value={h}>
                  {t(`vessels:hullTypes.${h}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field className="sm:max-w-64">
          <FieldLabel htmlFor="vessels-owner">{t('vessels:filters.owner')}</FieldLabel>
          <ElementCombobox
            id="vessels-owner"
            options={ownerOptions}
            value={owner?.id ?? null}
            onSearch={setOwnerQ}
            onChange={(id) =>
              setOwner(
                id ? { id, label: ownerOptions.find((o) => o.value === id)?.label ?? '' } : null,
              )
            }
            placeholder={t('vessels:filters.anyOwner')}
            searchPlaceholder={t('vessels:parties.searchCustomer')}
            emptyText={t('vessels:parties.noCustomers')}
          />
        </Field>
        <Field orientation="horizontal" className="sm:w-auto sm:pb-2">
          <Checkbox
            id="vessels-archived"
            checked={archived}
            onCheckedChange={(v) => setArchived(v === true)}
          />
          <FieldLabel htmlFor="vessels-archived">{t('vessels:filters.archived')}</FieldLabel>
        </Field>
      </div>
      <VesselsTable
        q={deferredQ}
        hullType={hullType}
        customerId={owner?.id ?? ''}
        archived={archived}
      />
    </RequirePermission>
  );
}

function VesselsTable({
  q,
  hullType,
  customerId,
  archived,
}: {
  q: string;
  hullType: HullType | '';
  customerId: string;
  archived: boolean;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const query = useVessels(marinaId, {
    q,
    ...(hullType ? { hull_type: hullType } : {}),
    customer_id: customerId,
    archived,
  });
  if (query.isPending) return <LoadingState rows={6} />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const columns: ColumnDef<VesselSummary, unknown>[] = [
    {
      header: t('vessels:columns.name'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <Link
            to="/m/$marinaId/vessels/$vesselId"
            params={{ marinaId, vesselId: row.original.id }}
            className="font-medium underline-offset-4 hover:underline"
          >
            {row.original.name}
          </Link>
          <span className="text-muted-foreground text-xs">
            {t(`vessels:hullTypes.${row.original.hull_type}`)}
          </span>
        </div>
      ),
    },
    {
      header: t('vessels:columns.registration'),
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.registration ?? ''}</span>
      ),
    },
    {
      header: t('vessels:columns.dimensions'),
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          <span className="tabular-nums">
            {`${formatCm(row.original.effective_loa_cm, i18n.language)} × ${formatCm(
              row.original.effective_beam_cm,
              i18n.language,
            )}`}
          </span>
          {row.original.dimensions_source === 'measured' ? (
            <Badge variant="secondary">{t('vessels:dimensionsSource.measured')}</Badge>
          ) : null}
        </div>
      ),
    },
    {
      header: t('vessels:columns.unitClass'),
      cell: ({ row }) => t(`vessels:unitClasses.${row.original.unit_class}`),
    },
    {
      header: t('vessels:columns.owners'),
      cell: ({ row }) => (
        <div className="flex flex-col text-sm">
          {row.original.owners.map((o) => (
            <Link
              key={o.customer_id}
              to="/m/$marinaId/customers/$customerId"
              params={{ marinaId, customerId: o.customer_id }}
              className="underline-offset-4 hover:underline"
            >
              {customerName(t, { display_name: o.display_name, anonymized_at: null })}
            </Link>
          ))}
          {row.original.archived_at ? (
            <Badge variant="secondary">{t('vessels:status.archived')}</Badge>
          ) : null}
        </div>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <DataTable
        columns={columns}
        data={query.data.pages.flatMap((p) => p.items)}
        getRowId={(v) => v.id}
        empty={t('vessels:empty')}
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
