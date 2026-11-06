import { Link } from '@tanstack/react-router';
import type { ColumnDef } from '@tanstack/react-table';
import { PlusIcon, SearchIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';

import { useCustomers, type CustomerSummary } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { InputGroup, InputGroupAddon, InputGroupInput } from '@porto/shared/ui/input-group';
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
import { CUSTOMER_CATEGORIES } from '../lib/enums';
import { customerName } from '../lib/format';

const ALL = '__all__';

export function CustomersPage() {
  const { t } = useT();
  const { can, marinaId } = useCurrentMarina();
  const [q, setQ] = useState('');
  const [category, setCategory] = useState<(typeof CUSTOMER_CATEGORIES)[number] | ''>('');
  const [tag, setTag] = useState('');
  const [archived, setArchived] = useState(false);
  const deferredQ = useDeferredValue(q.trim());
  const deferredTag = useDeferredValue(tag.trim());

  return (
    <RequirePermission allowed={can('customers.read')}>
      <PageHeader
        title={t('customers:title')}
        description={t('customers:description')}
        actions={
          can('customers.write') ? (
            <Button asChild>
              <Link to="/m/$marinaId/customers/new" params={{ marinaId }}>
                <PlusIcon />
                {t('customers:new')}
              </Link>
            </Button>
          ) : null
        }
      />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end">
        <Field className="sm:max-w-sm">
          <FieldLabel htmlFor="customers-search">{t('customers:search')}</FieldLabel>
          <InputGroup>
            <InputGroupAddon>
              <SearchIcon />
            </InputGroupAddon>
            <InputGroupInput
              id="customers-search"
              type="search"
              value={q}
              maxLength={100}
              placeholder={t('customers:searchPlaceholder')}
              onChange={(e) => setQ(e.target.value)}
            />
          </InputGroup>
        </Field>
        <Field className="sm:max-w-48">
          <FieldLabel htmlFor="customers-category">{t('customers:filters.category')}</FieldLabel>
          <Select
            value={category || ALL}
            onValueChange={(v) => setCategory(v === ALL ? '' : (v as typeof category))}
          >
            <SelectTrigger id="customers-category" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('customers:filters.allCategories')}</SelectItem>
              {CUSTOMER_CATEGORIES.map((c) => (
                <SelectItem key={c} value={c}>
                  {t(`customers:categories.${c}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field className="sm:max-w-40">
          <FieldLabel htmlFor="customers-tag">{t('customers:filters.tag')}</FieldLabel>
          <Input
            id="customers-tag"
            value={tag}
            maxLength={50}
            onChange={(e) => setTag(e.target.value)}
          />
        </Field>
        <Field orientation="horizontal" className="sm:w-auto sm:pb-2">
          <Checkbox
            id="customers-archived"
            checked={archived}
            onCheckedChange={(v) => setArchived(v === true)}
          />
          <FieldLabel htmlFor="customers-archived">{t('customers:filters.archived')}</FieldLabel>
        </Field>
      </div>
      <CustomersTable q={deferredQ} category={category} tag={deferredTag} archived={archived} />
    </RequirePermission>
  );
}

function CustomersTable({
  q,
  category,
  tag,
  archived,
}: {
  q: string;
  category: (typeof CUSTOMER_CATEGORIES)[number] | '';
  tag: string;
  archived: boolean;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const query = useCustomers(marinaId, {
    q,
    ...(category ? { category } : {}),
    tag,
    archived,
  });

  if (query.isPending) return <LoadingState rows={6} />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const columns: ColumnDef<CustomerSummary, unknown>[] = [
    {
      header: t('customers:columns.name'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <Link
            to="/m/$marinaId/customers/$customerId"
            params={{ marinaId, customerId: row.original.id }}
            className="font-medium underline-offset-4 hover:underline"
          >
            {customerName(t, row.original)}
          </Link>
          <span className="text-muted-foreground text-xs">
            {t(`customers:kind.${row.original.kind}`)}
            {row.original.category
              ? ` · ${t(`customers:categories.${row.original.category}`)}`
              : ''}
          </span>
        </div>
      ),
    },
    {
      header: t('customers:columns.fiscal'),
      cell: ({ row }) => (
        <span className="font-mono text-xs">
          {row.original.fiscal_code ?? row.original.vat_number ?? ''}
        </span>
      ),
    },
    {
      header: t('customers:columns.contact'),
      cell: ({ row }) => (
        <div className="flex flex-col text-xs">
          {row.original.primary_email ? <span>{row.original.primary_email}</span> : null}
          {row.original.primary_phone ? <span>{row.original.primary_phone}</span> : null}
        </div>
      ),
    },
    { header: t('customers:columns.vessels'), accessorKey: 'vessel_count' },
    {
      header: t('customers:columns.status'),
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          {row.original.tags.map((tagName) => (
            <Badge key={tagName} variant="outline">
              {tagName}
            </Badge>
          ))}
          {row.original.anonymized_at ? (
            <Badge variant="secondary">{t('customers:status.anonymized')}</Badge>
          ) : row.original.merged_into_id ? (
            <Badge variant="secondary">{t('customers:status.merged')}</Badge>
          ) : row.original.archived_at ? (
            <Badge variant="secondary">{t('customers:status.archived')}</Badge>
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
        getRowId={(c) => c.id}
        empty={t('customers:empty')}
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
