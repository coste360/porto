import type { ColumnDef } from '@tanstack/react-table';
import { MoreHorizontalIcon, PlusIcon, SearchIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';
import { toast } from 'sonner';

import {
  inUseReferencesOf,
  isProblem,
  useDeleteItem,
  useItems,
  usePricingSettings,
  useRevenueClasses,
  useUpdateItem,
  type Item,
  type ItemType,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp } from '@porto/shared/lib/format';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@porto/shared/ui/alert-dialog';
import { Badge } from '@porto/shared/ui/badge';
import { Button, buttonVariants } from '@porto/shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { InputGroup, InputGroupAddon, InputGroupInput } from '@porto/shared/ui/input-group';
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
import { toastProblem } from '../../../lib/toast';
import { ITEM_TYPES, localText } from '../lib/enums';
import { revenueClassOptions } from '../lib/labels';
import { ItemSheet } from './ItemSheet';

const ALL = '__all__';
type ActiveFilter = 'all' | 'active' | 'inactive';

/** Items and services (spec 12.1): filters, create/edit Sheet, deactivate, delete. */
export function ItemsPage() {
  const { t, i18n } = useT();
  const { can, marinaId } = useCurrentMarina();
  const [q, setQ] = useState('');
  const [type, setType] = useState<ItemType | ''>('');
  const [active, setActive] = useState<ActiveFilter>('all');
  const [sheet, setSheet] = useState<{ item: Item | null } | null>(null);
  const deferredQ = useDeferredValue(q.trim());
  const settings = usePricingSettings(marinaId);
  const classes = useRevenueClasses(marinaId, { includeArchived: true });
  const canWrite = can('pricing.configure');

  return (
    <RequirePermission allowed={can('pricing.read')}>
      <PageHeader
        title={t('pricing:items.title')}
        description={t('pricing:items.description')}
        actions={
          canWrite ? (
            <Button onClick={() => setSheet({ item: null })}>
              <PlusIcon />
              {t('pricing:items.new')}
            </Button>
          ) : null
        }
      />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end">
        <Field className="sm:max-w-sm">
          <FieldLabel htmlFor="items-search">{t('pricing:items.search')}</FieldLabel>
          <InputGroup>
            <InputGroupAddon>
              <SearchIcon />
            </InputGroupAddon>
            <InputGroupInput
              id="items-search"
              type="search"
              value={q}
              maxLength={64}
              onChange={(e) => setQ(e.target.value)}
            />
          </InputGroup>
        </Field>
        <Field className="sm:max-w-56">
          <FieldLabel htmlFor="items-type">{t('pricing:items.filters.type')}</FieldLabel>
          <Select
            value={type || ALL}
            onValueChange={(v) => setType(v === ALL ? '' : (v as ItemType))}
          >
            <SelectTrigger id="items-type" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('pricing:items.filters.allTypes')}</SelectItem>
              {ITEM_TYPES.map((x) => (
                <SelectItem key={x} value={x}>
                  {t(`pricingEnums:itemTypes.${x}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field className="sm:max-w-44">
          <FieldLabel htmlFor="items-active">{t('pricing:items.filters.status')}</FieldLabel>
          <Select value={active} onValueChange={(v) => setActive(v as ActiveFilter)}>
            <SelectTrigger id="items-active" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('pricing:items.filters.all')}</SelectItem>
              <SelectItem value="active">{t('pricing:items.filters.active')}</SelectItem>
              <SelectItem value="inactive">{t('pricing:items.filters.inactive')}</SelectItem>
            </SelectContent>
          </Select>
        </Field>
      </div>
      <ItemsTable
        q={deferredQ}
        type={type}
        active={active}
        canWrite={canWrite}
        classNames={
          new Map(
            revenueClassOptions(classes.data?.items ?? [], i18n.language).map((c) => [
              c.id,
              c.label,
            ]),
          )
        }
        onEdit={(item) => setSheet({ item })}
      />
      <ItemSheet
        marinaId={marinaId}
        open={sheet !== null}
        onOpenChange={(o) => (o ? null : setSheet(null))}
        item={sheet?.item ?? null}
        settings={settings.data}
        revenueClasses={classes.data?.items ?? []}
        readOnly={!canWrite}
      />
    </RequirePermission>
  );
}

function ItemsTable({
  q,
  type,
  active,
  canWrite,
  classNames,
  onEdit,
}: {
  q: string;
  type: ItemType | '';
  active: ActiveFilter;
  canWrite: boolean;
  classNames: Map<string, string>;
  onEdit: (item: Item) => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const query = useItems(marinaId, {
    q,
    ...(type ? { type } : {}),
    ...(active === 'all' ? {} : { active: active === 'active' }),
  });
  const update = useUpdateItem(marinaId);
  const remove = useDeleteItem(marinaId);
  const [toDelete, setToDelete] = useState<Item | null>(null);
  const [inUse, setInUse] = useState<{ item: Item; references: number | undefined } | null>(null);

  if (query.isPending) return <LoadingState rows={6} />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const setActive = async (item: Item, value: boolean) => {
    try {
      await update.mutateAsync({ id: item.id, version: item.version, patch: { active: value } });
      toast.success(t(value ? 'pricing:items.reactivated' : 'pricing:items.deactivated'));
    } catch (err) {
      toastProblem(t, err);
    }
  };

  const doDelete = async (item: Item) => {
    try {
      await remove.mutateAsync({ id: item.id, version: item.version });
      toast.success(t('pricing:items.deleted'));
    } catch (err) {
      if (isProblem(err, 'in_use')) setInUse({ item, references: inUseReferencesOf(err) });
      else toastProblem(t, err);
    }
  };

  const columns: ColumnDef<Item, unknown>[] = [
    {
      header: t('pricing:items.columns.code'),
      cell: ({ row }) => (
        <button
          type="button"
          className="font-mono text-sm font-medium underline-offset-4 hover:underline"
          onClick={() => onEdit(row.original)}
        >
          {row.original.code}
        </button>
      ),
    },
    {
      header: t('pricing:items.columns.name'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span>{localText(row.original.name, i18n.language)}</span>
          <span className="text-muted-foreground text-xs">
            {classNames.get(row.original.revenue_class_id) ?? ''}
          </span>
        </div>
      ),
    },
    {
      header: t('pricing:items.columns.type'),
      cell: ({ row }) => (
        <span>
          {t(`pricingEnums:itemTypes.${row.original.item_type}`)}
          <span className="text-muted-foreground">
            {' · '}
            {t(`pricingEnums:itemUnits.${row.original.unit}`)}
          </span>
        </span>
      ),
    },
    {
      header: t('pricing:items.columns.vat'),
      cell: ({ row }) => (
        <div className="flex flex-col text-sm">
          <span>
            {row.original.vat_rate_bp === 0 && row.original.vat_nature
              ? row.original.vat_nature
              : formatBp(row.original.vat_rate_bp, i18n.language)}
          </span>
          <span className="text-muted-foreground text-xs">
            {t(
              row.original.price_includes_vat
                ? 'pricing:items.vatIncluded'
                : 'pricing:items.vatExcluded',
            )}
          </span>
        </div>
      ),
    },
    {
      header: t('pricing:items.columns.status'),
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          {row.original.active ? null : (
            <Badge variant="secondary">{t('common:status.inactive')}</Badge>
          )}
          {row.original.online_visible ? (
            <Badge variant="outline">{t('pricing:items.online')}</Badge>
          ) : null}
        </div>
      ),
    },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) =>
        canWrite ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('pricing:items.actionsFor', { code: row.original.code })}
              >
                <MoreHorizontalIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => onEdit(row.original)}>
                {t('common:actions.edit')}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => void setActive(row.original, !row.original.active)}>
                {t(row.original.active ? 'pricing:items.deactivate' : 'pricing:items.reactivate')}
              </DropdownMenuItem>
              <DropdownMenuItem variant="destructive" onSelect={() => setToDelete(row.original)}>
                {t('common:actions.delete')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null,
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <DataTable
        columns={columns}
        data={query.data.pages.flatMap((p) => p.items)}
        getRowId={(i) => i.id}
        empty={t('pricing:items.empty')}
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
      <AlertDialog open={toDelete !== null} onOpenChange={(o) => (o ? null : setToDelete(null))}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('pricing:items.deleteTitle', { code: toDelete?.code ?? '' })}
            </AlertDialogTitle>
            <AlertDialogDescription>{t('pricing:items.deleteHint')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common:actions.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className={buttonVariants({ variant: 'destructive' })}
              onClick={() => {
                if (toDelete) void doDelete(toDelete);
                setToDelete(null);
              }}
            >
              {t('common:actions.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={inUse !== null} onOpenChange={(o) => (o ? null : setInUse(null))}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('pricing:items.inUseTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {inUse?.references !== undefined
                ? t('pricing:items.inUseCount', { count: inUse.references })
                : t('pricing:items.inUse')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common:actions.close')}</AlertDialogCancel>
            {inUse?.item.active ? (
              <AlertDialogAction
                onClick={() => {
                  if (inUse) void setActive(inUse.item, false);
                  setInUse(null);
                }}
              >
                {t('pricing:items.deactivate')}
              </AlertDialogAction>
            ) : null}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
