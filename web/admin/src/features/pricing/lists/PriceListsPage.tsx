import { zodResolver } from '@hookform/resolvers/zod';
import { Link, useNavigate } from '@tanstack/react-router';
import type { ColumnDef } from '@tanstack/react-table';
import { MoreHorizontalIcon, PlusIcon } from 'lucide-react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import {
  useArchivePriceList,
  useCreatePriceList,
  useDeletePriceList,
  usePriceLists,
  type PriceListSummary,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { cn } from '@porto/shared/lib/utils';
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
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DataTable, DatePicker, FormField, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';
import { CONTRACT_TYPES, PRICE_LIST_STATUSES, localText } from '../lib/enums';
import { GRID_COLS, monthCells, rowsByContractType, timelineYears } from '../lib/timeline';

/** Price lists (spec 12.2): grouped by status, validity timeline per contract type, actions. */
export function PriceListsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const query = usePriceLists(marinaId);
  const [creating, setCreating] = useState(false);
  const canWrite = can('pricing.configure');

  return (
    <RequirePermission allowed={can('pricing.read')}>
      <PageHeader
        title={t('pricing:lists.title')}
        description={t('pricing:lists.description')}
        actions={
          canWrite ? (
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              {t('pricing:lists.new')}
            </Button>
          ) : null
        }
      />
      {query.isPending ? (
        <LoadingState rows={6} />
      ) : query.isError ? (
        <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <div className="flex flex-col gap-6">
          <ValidityTimeline lists={query.data.items} />
          {PRICE_LIST_STATUSES.map((status) => (
            <StatusSection
              key={status}
              status={status}
              lists={query.data.items.filter((l) => l.status === status)}
              canWrite={canWrite}
            />
          ))}
        </div>
      )}
      <CreatePriceListDialog open={creating} onClose={() => setCreating(false)} />
    </RequirePermission>
  );
}

function ValidityTimeline({ lists }: { lists: PriceListSummary[] }) {
  const { t } = useT();
  const span = timelineYears(lists);
  if (!span) return null;
  const rows = rowsByContractType(lists, CONTRACT_TYPES);
  const cols = GRID_COLS[span.years] ?? 'grid-cols-12';
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('pricing:lists.timeline')}</CardTitle>
        <CardDescription>{t('pricing:lists.timelineHint')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 overflow-x-auto" data-testid="validity-timeline">
        <div className="flex items-center gap-2">
          <span className="w-40 shrink-0" />
          <div className={cn('grid min-w-96 flex-1 gap-px', cols)}>
            {Array.from({ length: span.years }, (_, i) => (
              <span key={i} className="text-muted-foreground col-span-12 text-xs">
                {span.start + i}
              </span>
            ))}
          </div>
        </div>
        {rows.map((row) => (
          <div key={row.contractType} className="flex flex-col gap-1">
            <span className="text-sm font-medium">
              {t(`pricingEnums:contractTypes.${row.contractType}`)}
            </span>
            {row.lists.map((l) => (
              <div key={l.id} className="flex items-center gap-2">
                <span className="w-40 shrink-0 truncate font-mono text-xs">{l.code}</span>
                <div
                  className={cn('grid min-w-96 flex-1 gap-px', cols)}
                  aria-label={t('pricing:lists.timelineRow', {
                    code: l.code,
                    from: formatIsoDate(l.valid_from),
                    to: formatIsoDate(l.valid_to),
                  })}
                  role="img"
                >
                  {monthCells(l, span.start, span.years).map((c) => (
                    <span
                      key={c.month}
                      className={cn(
                        'h-3 rounded-sm',
                        c.covered
                          ? l.status === 'active'
                            ? 'bg-chart-2'
                            : 'bg-chart-4'
                          : 'bg-muted',
                      )}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        ))}
        <ul className="flex flex-wrap gap-4 text-xs">
          <li className="flex items-center gap-2">
            <span className="bg-chart-2 size-3 rounded-sm" aria-hidden />
            {t('pricingEnums:priceListStatuses.active')}
          </li>
          <li className="flex items-center gap-2">
            <span className="bg-chart-4 size-3 rounded-sm" aria-hidden />
            {t('pricingEnums:priceListStatuses.draft')}
          </li>
        </ul>
      </CardContent>
    </Card>
  );
}

function StatusSection({
  status,
  lists,
  canWrite,
}: {
  status: PriceListSummary['status'];
  lists: PriceListSummary[];
  canWrite: boolean;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const navigate = useNavigate();
  const archive = useArchivePriceList(marinaId);
  const remove = useDeletePriceList(marinaId);
  const [confirm, setConfirm] = useState<{
    list: PriceListSummary;
    action: 'archive' | 'delete';
  } | null>(null);

  const run = async (list: PriceListSummary, action: 'archive' | 'delete') => {
    try {
      if (action === 'archive') {
        await archive.mutateAsync({ id: list.id, version: list.version });
        toast.success(t('pricing:lists.archived'));
      } else {
        await remove.mutateAsync({ id: list.id, version: list.version });
        toast.success(t('pricing:lists.deleted'));
      }
    } catch (err) {
      toastProblem(t, err);
    }
  };

  const columns: ColumnDef<PriceListSummary, unknown>[] = [
    {
      header: t('pricing:lists.columns.code'),
      cell: ({ row }) => (
        <Link
          to="/m/$marinaId/price-lists/$priceListId"
          params={{ marinaId, priceListId: row.original.id }}
          className="font-mono font-medium underline-offset-4 hover:underline"
        >
          {row.original.code}
        </Link>
      ),
    },
    {
      header: t('pricing:lists.columns.name'),
      cell: ({ row }) => localText(row.original.name, i18n.language),
    },
    {
      header: t('pricing:lists.columns.validity'),
      cell: ({ row }) => (
        <span className="text-sm whitespace-nowrap">
          {formatIsoDate(row.original.valid_from)} – {formatIsoDate(row.original.valid_to)}
        </span>
      ),
    },
    {
      header: t('pricing:lists.columns.contractTypes'),
      cell: ({ row }) => (
        <span className="flex flex-wrap gap-1">
          {row.original.contract_types.map((c) => (
            <Badge key={c} variant="outline">
              {t(`pricingEnums:contractTypes.${c}`)}
            </Badge>
          ))}
        </span>
      ),
    },
    { header: t('pricing:lists.columns.rules'), accessorKey: 'rule_count' },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) => {
        const l = row.original;
        return (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('pricing:lists.actionsFor', { code: l.code })}
              >
                <MoreHorizontalIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onSelect={() =>
                  void navigate({
                    to: '/m/$marinaId/price-lists/$priceListId',
                    params: { marinaId, priceListId: l.id },
                  })
                }
              >
                {t('pricing:lists.open')}
              </DropdownMenuItem>
              {canWrite ? (
                <DropdownMenuItem
                  onSelect={() =>
                    void navigate({
                      to: '/m/$marinaId/price-lists/$priceListId/renew',
                      params: { marinaId, priceListId: l.id },
                    })
                  }
                >
                  {t('pricing:lists.renew')}
                </DropdownMenuItem>
              ) : null}
              {canWrite && l.status === 'active' ? (
                <DropdownMenuItem onSelect={() => setConfirm({ list: l, action: 'archive' })}>
                  {t('pricing:lists.archive')}
                </DropdownMenuItem>
              ) : null}
              {canWrite && l.status === 'draft' ? (
                <DropdownMenuItem
                  variant="destructive"
                  onSelect={() => setConfirm({ list: l, action: 'delete' })}
                >
                  {t('common:actions.delete')}
                </DropdownMenuItem>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        );
      },
    },
  ];

  return (
    <section className="flex flex-col gap-2" aria-labelledby={`lists-${status}`}>
      <h2 id={`lists-${status}`} className="text-lg font-semibold">
        {t(`pricing:lists.sections.${status}`)}{' '}
        <span className="text-muted-foreground text-sm font-normal">({lists.length})</span>
      </h2>
      <DataTable
        columns={columns}
        data={lists}
        getRowId={(l) => l.id}
        empty={t(`pricing:lists.empty.${status}`)}
      />
      <AlertDialog open={confirm !== null} onOpenChange={(o) => (o ? null : setConfirm(null))}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirm?.action === 'archive'
                ? t('pricing:lists.archiveTitle', { code: confirm.list.code })
                : t('pricing:lists.deleteTitle', { code: confirm?.list.code ?? '' })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                confirm?.action === 'archive'
                  ? 'pricing:lists.archiveHint'
                  : 'pricing:lists.deleteHint',
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common:actions.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className={
                confirm?.action === 'delete'
                  ? buttonVariants({ variant: 'destructive' })
                  : undefined
              }
              onClick={() => {
                if (confirm) void run(confirm.list, confirm.action);
                setConfirm(null);
              }}
            >
              {confirm?.action === 'archive'
                ? t('pricing:lists.archive')
                : t('common:actions.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/;

const createListSchema = z
  .object({
    code: z.string().trim().min(1, 'common:validation.required').max(32),
    name_it: z.string().trim().min(1, 'common:validation.required').max(200),
    name_en: z.string().trim().max(200),
    valid_from: z.string().regex(ISO_DATE, 'common:validation.required'),
    valid_to: z.string().regex(ISO_DATE, 'common:validation.required'),
    contract_types: z.array(z.enum(CONTRACT_TYPES)).min(1, 'common:validation.atLeastOne'),
  })
  .refine((v) => v.valid_to >= v.valid_from, {
    path: ['valid_to'],
    message: 'pricing:lists.validation.validTo',
  });
type CreateValues = z.infer<typeof createListSchema>;

function CreatePriceListDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useT();
  return (
    <Dialog open={open} onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent>
        {open ? (
          <CreateForm onClose={onClose} />
        ) : (
          <DialogTitle className="sr-only">{t('pricing:lists.new')}</DialogTitle>
        )}
      </DialogContent>
    </Dialog>
  );
}

function CreateForm({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const navigate = useNavigate();
  const create = useCreatePriceList(marinaId);
  const year = new Date().getFullYear();
  const form = useForm<CreateValues>({
    resolver: zodResolver(createListSchema),
    defaultValues: {
      code: '',
      name_it: '',
      name_en: '',
      valid_from: `${year}-01-01`,
      valid_to: `${year}-12-31`,
      contract_types: ['transit'],
    },
  });
  const submit = form.handleSubmit(async (v) => {
    try {
      const doc = await create.mutateAsync({
        code: v.code,
        name: v.name_en ? { it: v.name_it, en: v.name_en } : { it: v.name_it },
        valid_from: v.valid_from,
        valid_to: v.valid_to,
        contract_types: v.contract_types,
      });
      toast.success(t('pricing:lists.created'));
      onClose();
      await navigate({
        to: '/m/$marinaId/price-lists/$priceListId',
        params: { marinaId, priceListId: doc.id },
      });
    } catch (err) {
      toastProblem(t, err);
    }
  });
  return (
    <form onSubmit={(e) => void submit(e)} noValidate>
      <DialogHeader>
        <DialogTitle>{t('pricing:lists.new')}</DialogTitle>
        <DialogDescription>{t('pricing:lists.newHint')}</DialogDescription>
      </DialogHeader>
      <FieldGroup className="py-4">
        <FormField
          control={form.control}
          name="code"
          label={t('pricing:lists.fields.code')}
          render={(field) => <Input {...field} autoComplete="off" maxLength={32} />}
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField
            control={form.control}
            name="name_it"
            label={t('pricing:lists.fields.nameIt')}
            render={(field) => <Input {...field} autoComplete="off" />}
          />
          <FormField
            control={form.control}
            name="name_en"
            label={t('pricing:lists.fields.nameEn')}
            render={(field) => <Input {...field} autoComplete="off" />}
          />
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField
            control={form.control}
            name="valid_from"
            label={t('pricing:lists.fields.validFrom')}
            render={({ value, onChange, id, ...rest }) => (
              <DatePicker
                id={id}
                value={value || null}
                onChange={(d) => onChange(d ?? '')}
                aria-invalid={rest['aria-invalid']}
              />
            )}
          />
          <FormField
            control={form.control}
            name="valid_to"
            label={t('pricing:lists.fields.validTo')}
            render={({ value, onChange, id, ...rest }) => (
              <DatePicker
                id={id}
                value={value || null}
                onChange={(d) => onChange(d ?? '')}
                aria-invalid={rest['aria-invalid']}
              />
            )}
          />
        </div>
        <FormField
          control={form.control}
          name="contract_types"
          label={t('pricing:lists.fields.contractTypes')}
          render={({ value, onChange, id }) => (
            <div id={id} className="flex flex-wrap gap-4">
              {CONTRACT_TYPES.map((c) => (
                <Field key={c} orientation="horizontal" className="w-auto">
                  <Checkbox
                    id={`${id}-${c}`}
                    checked={value.includes(c)}
                    onCheckedChange={(on) =>
                      onChange(
                        CONTRACT_TYPES.filter((x) => (x === c ? on === true : value.includes(x))),
                      )
                    }
                  />
                  <FieldLabel htmlFor={`${id}-${c}`} className="font-normal">
                    {t(`pricingEnums:contractTypes.${c}`)}
                  </FieldLabel>
                </Field>
              ))}
            </div>
          )}
        />
      </FieldGroup>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={form.formState.isSubmitting}>
          {t('common:actions.create')}
        </Button>
      </DialogFooter>
    </form>
  );
}
