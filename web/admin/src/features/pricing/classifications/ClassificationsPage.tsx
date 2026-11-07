import { zodResolver } from '@hookform/resolvers/zod';
import { PlusIcon, RefreshCwIcon } from 'lucide-react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';

import {
  inUseReferencesOf,
  isProblem,
  useBerthCategories,
  useCreateBerthCategory,
  useCreateRevenueClass,
  useCustomerCategoryCounts,
  useDeleteBerthCategory,
  useDeleteRevenueClass,
  useRevenueClasses,
  useSyncBerthCategories,
  useUpdateBerthCategory,
  useUpdateRevenueClass,
  type BerthCategory,
  type RevenueClass,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
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
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { ConfirmDialog, FormField, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';
import { IntegerInput } from '../components/IntegerInput';
import {
  CATEGORY_TARGETS,
  CUSTOMER_CATEGORIES,
  HULL_TYPES,
  MULTIHULLS,
  localText,
} from '../lib/enums';
import {
  NO_PARENT,
  categoryCreateBody,
  categoryDefaults,
  categoryPatchBody,
  categorySchema,
  revenueClassCreateBody,
  revenueClassDefaults,
  revenueClassPatchBody,
  revenueClassSchema,
  revenueClassTree,
  type CategoryForm,
  type CategoryFormIn,
  type RevenueClassForm,
  type RevenueClassFormIn,
} from './forms';

/** Classifications used by prices (spec 07, 11.1, 12.1): berth categories, revenue classes, enums. */
export function ClassificationsPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <RequirePermission allowed={can('pricing.read')}>
      <PageHeader
        title={t('pricing:classifications.title')}
        description={t('pricing:classifications.description')}
      />
      <Tabs defaultValue="categories">
        <TabsList className="flex-wrap">
          <TabsTrigger value="categories">
            {t('pricing:classifications.tabs.categories')}
          </TabsTrigger>
          <TabsTrigger value="revenue">{t('pricing:classifications.tabs.revenue')}</TabsTrigger>
          <TabsTrigger value="customers">{t('pricing:classifications.tabs.customers')}</TabsTrigger>
          <TabsTrigger value="hulls">{t('pricing:classifications.tabs.hulls')}</TabsTrigger>
        </TabsList>
        <TabsContent value="categories" className="pt-4">
          <BerthCategoriesTab />
        </TabsContent>
        <TabsContent value="revenue" className="pt-4">
          <RevenueClassesTab />
        </TabsContent>
        <TabsContent value="customers" className="pt-4">
          <CustomerCategoriesTab />
        </TabsContent>
        <TabsContent value="hulls" className="pt-4">
          <EnumCard
            title={t('pricing:classifications.hulls.title')}
            hint={t('pricing:classifications.hulls.hint')}
            rows={HULL_TYPES.map((h) => ({
              code: h,
              label: t(`pricingEnums:hullTypes.${h}`),
              badge: MULTIHULLS.includes(h) ? t('pricing:classifications.hulls.multihull') : null,
            }))}
          />
        </TabsContent>
      </Tabs>
    </RequirePermission>
  );
}

/** The fixed customer categories with the number of customers in each (counts only). */
function CustomerCategoriesTab() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const query = useCustomerCategoryCounts(marinaId);
  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const counts = new Map(query.data.items.map((c) => [c.code, c.count]));
  return (
    <EnumCard
      title={t('pricing:classifications.customers.title')}
      hint={t('pricing:classifications.customers.hint')}
      countHeader={t('pricing:classifications.columns.customers')}
      rows={[
        ...CUSTOMER_CATEGORIES.map((c) => ({
          code: c,
          label: t(`pricingEnums:customerCategories.${c}`),
          count: counts.get(c) ?? 0,
        })),
        {
          code: '—',
          label: t('pricing:classifications.customers.uncategorised'),
          count: query.data.uncategorised,
        },
      ]}
    />
  );
}

function EnumCard({
  title,
  hint,
  rows,
  countHeader,
}: {
  title: string;
  hint: string;
  rows: { code: string; label: string; badge?: string | null; count?: number }[];
  /** When set, a right-aligned column with each row's count. */
  countHeader?: string;
}) {
  const { t } = useT();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{hint}</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('pricing:classifications.columns.code')}</TableHead>
              <TableHead>{t('pricing:classifications.columns.name')}</TableHead>
              {countHeader ? <TableHead className="text-right">{countHeader}</TableHead> : null}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.code}>
                <TableCell className="font-mono text-xs">{r.code}</TableCell>
                <TableCell>
                  <span className="flex flex-wrap items-center gap-2">
                    {r.label}
                    {r.badge ? <Badge variant="secondary">{r.badge}</Badge> : null}
                  </span>
                </TableCell>
                {countHeader ? (
                  <TableCell className="text-right tabular-nums">{r.count ?? 0}</TableCell>
                ) : null}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------------------------------------
// Berth categories

function BerthCategoriesTab() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const canWrite = can('pricing.configure');
  const [archived, setArchived] = useState(false);
  const query = useBerthCategories(marinaId, { includeArchived: archived });
  const sync = useSyncBerthCategories(marinaId);
  const update = useUpdateBerthCategory(marinaId);
  const remove = useDeleteBerthCategory(marinaId);
  const [dialog, setDialog] = useState<{ category: BerthCategory | null } | null>(null);

  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const doSync = async () => {
    try {
      const res = await sync.mutateAsync();
      toast.success(t('pricing:classifications.categories.synced', { count: res.created.length }));
    } catch (err) {
      toastProblem(t, err);
    }
  };
  const doDelete = async (c: BerthCategory) => {
    try {
      await remove.mutateAsync({ id: c.id, version: c.version });
      toast.success(t('pricing:classifications.deleted'));
    } catch (err) {
      if (isProblem(err, 'in_use')) {
        const n = inUseReferencesOf(err);
        toast.error(
          n !== undefined
            ? t('pricing:classifications.categories.inUseCount', { count: n })
            : t('pricing:classifications.categories.inUse'),
        );
      } else toastProblem(t, err);
    }
  };
  const toggleArchived = async (c: BerthCategory) => {
    try {
      await update.mutateAsync({ id: c.id, version: c.version, patch: { archived: !c.archived } });
    } catch (err) {
      toastProblem(t, err);
    }
  };

  const { items, unknown_in_use: unknown } = query.data;
  return (
    <div className="flex flex-col gap-4">
      {unknown.length > 0 ? (
        <Alert>
          <AlertTitle>{t('pricing:classifications.categories.unknownTitle')}</AlertTitle>
          <AlertDescription>
            <p>{t('pricing:classifications.categories.unknownHint')}</p>
            <ul className="flex flex-wrap gap-2 py-2">
              {unknown.map((u) => (
                <li key={u.code}>
                  <Badge variant="outline">
                    {t('pricing:classifications.categories.unknownCode', {
                      code: u.code,
                      count: u.count,
                    })}
                  </Badge>
                </li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      ) : null}
      <div className="flex flex-wrap items-center gap-3">
        {canWrite ? (
          <>
            <Button onClick={() => setDialog({ category: null })}>
              <PlusIcon />
              {t('pricing:classifications.categories.new')}
            </Button>
            <Button variant="outline" onClick={() => void doSync()} disabled={sync.isPending}>
              <RefreshCwIcon />
              {t('pricing:classifications.categories.sync')}
            </Button>
          </>
        ) : null}
        <div className="flex items-center gap-2">
          <Checkbox
            id="categories-archived"
            checked={archived}
            onCheckedChange={(v) => setArchived(v === true)}
          />
          <Label htmlFor="categories-archived">{t('pricing:classifications.showArchived')}</Label>
        </div>
      </div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('pricing:classifications.columns.code')}</TableHead>
              <TableHead>{t('pricing:classifications.columns.name')}</TableHead>
              <TableHead>{t('pricing:classifications.columns.appliesTo')}</TableHead>
              <TableHead>{t('pricing:classifications.columns.usage')}</TableHead>
              <TableHead>
                <span className="sr-only">{t('common:actions.actions')}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-muted-foreground h-24 text-center">
                  {t('pricing:classifications.categories.empty')}
                </TableCell>
              </TableRow>
            ) : (
              items.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="font-mono text-sm font-medium">{c.code}</TableCell>
                  <TableCell>
                    <span className="flex flex-wrap items-center gap-2">
                      {localText(c.name, i18n.language)}
                      {c.archived ? (
                        <Badge variant="secondary">{t('pricing:classifications.archived')}</Badge>
                      ) : null}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="flex flex-wrap gap-1">
                      {c.applies_to.map((a) => (
                        <Badge key={a} variant="outline">
                          {t(`pricingEnums:categoryTargets.${a}`)}
                        </Badge>
                      ))}
                    </span>
                  </TableCell>
                  <TableCell className="text-sm">
                    {t('pricing:classifications.categories.usage', {
                      berths: c.usage.berths,
                      areas: c.usage.dynamic_areas,
                      land: c.usage.land_resources,
                      rules: c.usage.rules,
                    })}
                  </TableCell>
                  <TableCell>
                    {canWrite ? (
                      <div className="flex flex-wrap justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDialog({ category: c })}
                          aria-label={t('pricing:classifications.editNamed', { code: c.code })}
                        >
                          {t('common:actions.edit')}
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => void toggleArchived(c)}>
                          {t(
                            c.archived
                              ? 'pricing:classifications.restore'
                              : 'pricing:classifications.archive',
                          )}
                        </Button>
                        <ConfirmDialog
                          trigger={
                            <Button
                              variant="ghost"
                              size="sm"
                              aria-label={t('pricing:classifications.deleteNamed', {
                                code: c.code,
                              })}
                            >
                              {t('common:actions.delete')}
                            </Button>
                          }
                          title={t('pricing:classifications.deleteTitle', { code: c.code })}
                          description={t('pricing:classifications.categories.deleteHint')}
                          confirmLabel={t('common:actions.delete')}
                          cancelLabel={t('common:actions.cancel')}
                          destructive
                          onConfirm={() => void doDelete(c)}
                        />
                      </div>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      <CategoryDialog
        marinaId={marinaId}
        category={dialog?.category ?? null}
        open={dialog !== null}
        onClose={() => setDialog(null)}
      />
    </div>
  );
}

function CategoryDialog({
  marinaId,
  category,
  open,
  onClose,
}: {
  marinaId: string;
  category: BerthCategory | null;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useT();
  return (
    <Dialog open={open} onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent>
        {open ? (
          <CategoryFormBody
            key={category?.id ?? 'new'}
            marinaId={marinaId}
            category={category}
            onClose={onClose}
          />
        ) : (
          <DialogTitle className="sr-only">
            {t('pricing:classifications.categories.new')}
          </DialogTitle>
        )}
      </DialogContent>
    </Dialog>
  );
}

function CategoryFormBody({
  marinaId,
  category,
  onClose,
}: {
  marinaId: string;
  category: BerthCategory | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateBerthCategory(marinaId);
  const update = useUpdateBerthCategory(marinaId);
  const form = useForm<CategoryFormIn, unknown, CategoryForm>({
    resolver: zodResolver(categorySchema),
    defaultValues: categoryDefaults(category),
  });
  const submit = form.handleSubmit(async (v) => {
    try {
      if (category) {
        await update.mutateAsync({
          id: category.id,
          version: category.version,
          patch: categoryPatchBody(v),
        });
      } else {
        await create.mutateAsync(categoryCreateBody(v));
      }
      toast.success(t('common:status.saved'));
      onClose();
    } catch (err) {
      toastProblem(t, err);
    }
  });
  return (
    <form onSubmit={(e) => void submit(e)} noValidate>
      <DialogHeader>
        <DialogTitle>
          {t(
            category
              ? 'pricing:classifications.categories.editTitle'
              : 'pricing:classifications.categories.new',
          )}
        </DialogTitle>
        <DialogDescription>{t('pricing:classifications.categories.formHint')}</DialogDescription>
      </DialogHeader>
      <FieldGroup className="py-4">
        <FormField
          control={form.control}
          name="code"
          label={t('pricing:classifications.fields.code')}
          render={(field) => (
            <Input {...field} autoComplete="off" disabled={category !== null} maxLength={32} />
          )}
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField
            control={form.control}
            name="name_it"
            label={t('pricing:classifications.fields.nameIt')}
            render={(field) => <Input {...field} autoComplete="off" />}
          />
          <FormField
            control={form.control}
            name="name_en"
            label={t('pricing:classifications.fields.nameEn')}
            render={(field) => <Input {...field} autoComplete="off" />}
          />
        </div>
        <FormField
          control={form.control}
          name="applies_to"
          label={t('pricing:classifications.fields.appliesTo')}
          render={({ value, onChange, id }) => (
            <div id={id} className="flex flex-wrap gap-4">
              {CATEGORY_TARGETS.map((target) => (
                <Field key={target} orientation="horizontal" className="w-auto">
                  <Checkbox
                    id={`${id}-${target}`}
                    checked={value.includes(target)}
                    onCheckedChange={(c) =>
                      onChange(
                        CATEGORY_TARGETS.filter((x) =>
                          x === target ? c === true : value.includes(x),
                        ),
                      )
                    }
                  />
                  <FieldLabel htmlFor={`${id}-${target}`} className="font-normal">
                    {t(`pricingEnums:categoryTargets.${target}`)}
                  </FieldLabel>
                </Field>
              ))}
            </div>
          )}
        />
        <FormField
          control={form.control}
          name="sort_order"
          label={t('pricing:classifications.fields.sortOrder')}
          render={({ value, onChange, onBlur, id, ...rest }) => (
            <IntegerInput
              id={id}
              value={value}
              onChange={onChange}
              onBlur={onBlur}
              allowNegative
              aria-invalid={rest['aria-invalid']}
            />
          )}
        />
      </FieldGroup>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={form.formState.isSubmitting}>
          {t('common:actions.save')}
        </Button>
      </DialogFooter>
    </form>
  );
}

// ---------------------------------------------------------------------------------------------------------
// Revenue classes

function RevenueClassesTab() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const canWrite = can('pricing.configure');
  const [archived, setArchived] = useState(false);
  const query = useRevenueClasses(marinaId, { includeArchived: archived });
  const update = useUpdateRevenueClass(marinaId);
  const remove = useDeleteRevenueClass(marinaId);
  const [dialog, setDialog] = useState<{ cls: RevenueClass | null; parentId?: string } | null>(
    null,
  );

  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const classes = query.data.items;
  const tree = revenueClassTree(classes);
  const doDelete = async (c: RevenueClass) => {
    try {
      await remove.mutateAsync({ id: c.id, version: c.version });
      toast.success(t('pricing:classifications.deleted'));
    } catch (err) {
      if (isProblem(err, 'in_use')) toast.error(t('pricing:classifications.revenue.inUse'));
      else toastProblem(t, err);
    }
  };
  const toggleArchived = async (c: RevenueClass) => {
    try {
      await update.mutateAsync({ id: c.id, version: c.version, patch: { archived: !c.archived } });
    } catch (err) {
      toastProblem(t, err);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">{t('pricing:classifications.revenue.hint')}</p>
      <div className="flex flex-wrap items-center gap-3">
        {canWrite ? (
          <Button onClick={() => setDialog({ cls: null })}>
            <PlusIcon />
            {t('pricing:classifications.revenue.new')}
          </Button>
        ) : null}
        <div className="flex items-center gap-2">
          <Checkbox
            id="revenue-archived"
            checked={archived}
            onCheckedChange={(v) => setArchived(v === true)}
          />
          <Label htmlFor="revenue-archived">{t('pricing:classifications.showArchived')}</Label>
        </div>
      </div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('pricing:classifications.columns.name')}</TableHead>
              <TableHead>{t('pricing:classifications.columns.code')}</TableHead>
              <TableHead>{t('pricing:classifications.columns.items')}</TableHead>
              <TableHead>
                <span className="sr-only">{t('common:actions.actions')}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tree.map(({ cls, depth }) => (
              <TableRow key={cls.id}>
                <TableCell>
                  <span
                    className={
                      depth === 1
                        ? 'flex items-center gap-2 pl-6'
                        : 'flex items-center gap-2 font-medium'
                    }
                  >
                    {localText(cls.name, i18n.language)}
                    {cls.archived ? (
                      <Badge variant="secondary">{t('pricing:classifications.archived')}</Badge>
                    ) : null}
                  </span>
                </TableCell>
                <TableCell className="font-mono text-xs">{cls.code}</TableCell>
                <TableCell>{cls.item_count}</TableCell>
                <TableCell>
                  {canWrite ? (
                    <div className="flex flex-wrap justify-end gap-1">
                      {depth === 0 ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDialog({ cls: null, parentId: cls.id })}
                          aria-label={t('pricing:classifications.revenue.addChildTo', {
                            code: cls.code,
                          })}
                        >
                          {t('pricing:classifications.revenue.addChild')}
                        </Button>
                      ) : null}
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setDialog({ cls })}
                        aria-label={t('pricing:classifications.editNamed', { code: cls.code })}
                      >
                        {t('common:actions.edit')}
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => void toggleArchived(cls)}>
                        {t(
                          cls.archived
                            ? 'pricing:classifications.restore'
                            : 'pricing:classifications.archive',
                        )}
                      </Button>
                      <ConfirmDialog
                        trigger={
                          <Button
                            variant="ghost"
                            size="sm"
                            aria-label={t('pricing:classifications.deleteNamed', {
                              code: cls.code,
                            })}
                          >
                            {t('common:actions.delete')}
                          </Button>
                        }
                        title={t('pricing:classifications.deleteTitle', { code: cls.code })}
                        description={t('pricing:classifications.revenue.deleteHint')}
                        confirmLabel={t('common:actions.delete')}
                        cancelLabel={t('common:actions.cancel')}
                        destructive
                        onConfirm={() => void doDelete(cls)}
                      />
                    </div>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <Dialog open={dialog !== null} onOpenChange={(o) => (o ? null : setDialog(null))}>
        <DialogContent>
          {dialog ? (
            <RevenueClassFormBody
              key={dialog.cls?.id ?? `new-${dialog.parentId ?? ''}`}
              marinaId={marinaId}
              cls={dialog.cls}
              parentId={dialog.parentId}
              roots={classes.filter((c) => !c.parent_id && c.id !== dialog.cls?.id)}
              onClose={() => setDialog(null)}
            />
          ) : (
            <DialogTitle className="sr-only">
              {t('pricing:classifications.revenue.new')}
            </DialogTitle>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function RevenueClassFormBody({
  marinaId,
  cls,
  parentId,
  roots,
  onClose,
}: {
  marinaId: string;
  cls: RevenueClass | null;
  parentId: string | undefined;
  roots: RevenueClass[];
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const create = useCreateRevenueClass(marinaId);
  const update = useUpdateRevenueClass(marinaId);
  const form = useForm<RevenueClassFormIn, unknown, RevenueClassForm>({
    resolver: zodResolver(revenueClassSchema),
    defaultValues: revenueClassDefaults(cls, parentId),
  });
  const submit = form.handleSubmit(async (v) => {
    try {
      if (cls) {
        await update.mutateAsync({
          id: cls.id,
          version: cls.version,
          patch: revenueClassPatchBody(v),
        });
      } else {
        await create.mutateAsync(revenueClassCreateBody(v));
      }
      toast.success(t('common:status.saved'));
      onClose();
    } catch (err) {
      toastProblem(t, err);
    }
  });
  return (
    <form onSubmit={(e) => void submit(e)} noValidate>
      <DialogHeader>
        <DialogTitle>
          {t(
            cls
              ? 'pricing:classifications.revenue.editTitle'
              : 'pricing:classifications.revenue.new',
          )}
        </DialogTitle>
        <DialogDescription>{t('pricing:classifications.revenue.formHint')}</DialogDescription>
      </DialogHeader>
      <FieldGroup className="py-4">
        <FormField
          control={form.control}
          name="code"
          label={t('pricing:classifications.fields.code')}
          description={t('pricing:classifications.fields.classCodeHint')}
          render={(field) => <Input {...field} autoComplete="off" maxLength={32} />}
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField
            control={form.control}
            name="name_it"
            label={t('pricing:classifications.fields.nameIt')}
            render={(field) => <Input {...field} autoComplete="off" />}
          />
          <FormField
            control={form.control}
            name="name_en"
            label={t('pricing:classifications.fields.nameEn')}
            render={(field) => <Input {...field} autoComplete="off" />}
          />
        </div>
        <FormField
          control={form.control}
          name="parent_id"
          label={t('pricing:classifications.fields.parent')}
          render={({ value, onChange, id }) => (
            <Select value={value} onValueChange={onChange}>
              <SelectTrigger id={id} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_PARENT}>
                  {t('pricing:classifications.fields.noParent')}
                </SelectItem>
                {roots.map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {localText(r.name, i18n.language)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        />
        <FormField
          control={form.control}
          name="sort_order"
          label={t('pricing:classifications.fields.sortOrder')}
          render={({ value, onChange, onBlur, id, ...rest }) => (
            <IntegerInput
              id={id}
              value={value}
              onChange={onChange}
              onBlur={onBlur}
              allowNegative
              aria-invalid={rest['aria-invalid']}
            />
          )}
        />
      </FieldGroup>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={form.formState.isSubmitting}>
          {t('common:actions.save')}
        </Button>
      </DialogFooter>
    </form>
  );
}
