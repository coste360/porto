import { getRouteApi } from '@tanstack/react-router';
import { useState } from 'react';

import {
  useCreateExternalCost,
  useCreateStockMovement,
  useCreateTimeEntry,
  useCreateWorkTask,
  useDeleteExternalCost,
  useEmployees,
  useExternalCosts,
  useFinalAccount,
  useItems,
  useStockMovements,
  useTaskFileLink,
  useTaskFiles,
  useTaskProgress,
  useTimesheets,
  useUploadTaskFile,
  useWorkOrder,
  useWorkOrderBilling,
  useWorkOrderBillings,
  useWorkTasks,
  useWorkTypes,
  type WorkOrder,
  type WorkOrderBillingResult,
  type WorkTask,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp, formatQtyMilli } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { MoneyInput, PageHeader } from '@porto/shared/ui/patterns';
import { Progress } from '@porto/shared/ui/progress';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import {
  Detail,
  DetailGrid,
  EnumSelect,
  IsoDate,
  M4ProblemAlert,
  Money,
  PageFrame,
  Section,
} from '../../contracts-m4/components/kit';
import { WorkOrderLiftingBookings } from '../../lifting/public';
import { localName } from '../../maps/lib';
import { OrderActions, OrderStatusBadge } from '../components/kit';
import { groupAccount, hoursText, parseHours, parseQty } from '../lib/orders';

const route = getRouteApi('/auth/m/$marinaId/yard/orders/$orderId');

const TABS = [
  'summary',
  'tasks',
  'hours',
  'materials',
  'external',
  'account',
  'billings',
  'variations',
] as const;

/** Work order (m10-plan §14.2): summary and lifting, tasks, hours, materials, external costs, final account,
 * billings and variations. Writes need yard.manage; billings need charges.manage. */
export function WorkOrderPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { orderId } = route.useParams();
  const order = useWorkOrder(marinaId, orderId);
  const [warnings, setWarnings] = useState<string[]>([]);
  if (order.isPending) return <LoadingState rows={6} />;
  if (order.isError)
    return <QueryErrorState error={order.error} onRetry={() => void order.refetch()} />;
  const o = order.data;
  return (
    <RequirePermission allowed={can('yard.read') || can('yard.manage')}>
      <PageFrame>
        <div data-testid="work-order-page" className="flex flex-col gap-4">
          <PageHeader
            title={`${t('yardOrders:orderTitle')} ${o.number}`}
            description={o.vessel_name ?? undefined}
            actions={
              <OrderActions
                orderId={o.id}
                status={o.status}
                deliveredAt={o.delivered_at}
                onDone={setWarnings}
              />
            }
          />
          {warnings.map((w) => (
            <p key={w} className="text-muted-foreground text-sm" role="status">
              {t(`yardOrders:warnings.${w}`, { defaultValue: w })}
            </p>
          ))}
          <Tabs defaultValue="summary">
            <TabsList className="flex-wrap">
              {TABS.map((tab) => (
                <TabsTrigger key={tab} value={tab}>
                  {t(`yardOrders:tabs.${tab}`)}
                </TabsTrigger>
              ))}
            </TabsList>
            <TabsContent value="summary">
              <SummaryTab order={o} />
            </TabsContent>
            <TabsContent value="tasks">
              <TasksTab order={o} />
            </TabsContent>
            <TabsContent value="hours">
              <HoursTab order={o} />
            </TabsContent>
            <TabsContent value="materials">
              <MaterialsTab order={o} />
            </TabsContent>
            <TabsContent value="external">
              <ExternalTab order={o} />
            </TabsContent>
            <TabsContent value="account">
              <AccountTab order={o} />
            </TabsContent>
            <TabsContent value="billings">
              <BillingsTab order={o} />
            </TabsContent>
            <TabsContent value="variations">
              <VariationsTab order={o} />
            </TabsContent>
          </Tabs>
        </div>
      </PageFrame>
    </RequirePermission>
  );
}

function SummaryTab({ order: o }: { order: WorkOrder }) {
  const { t, i18n } = useT();
  return (
    <div className="flex flex-col gap-4">
      <Section title={t('yardOrders:tabs.summary')}>
        <DetailGrid>
          <Detail label={t('yardOrders:fields.status')}>
            <OrderStatusBadge status={o.status} />
          </Detail>
          <Detail label={t('yardOrders:fields.billingMode')}>
            {t(`yardOrders:billingMode.${o.billing_mode}`)}
          </Detail>
          <Detail label={t('yardOrders:fields.plannedStart')}>
            <IsoDate date={o.planned_start_on} />
          </Detail>
          <Detail label={t('yardOrders:fields.plannedDelivery')}>
            <IsoDate date={o.planned_delivery_on} />
          </Detail>
          <Detail label={t('yardOrders:fields.position')}>{o.position_note}</Detail>
          <Detail label={t('yardOrders:fields.completion')}>
            {formatBp(o.completion_bp, i18n.language)}
          </Detail>
          <Detail label={t('yardOrders:fields.hours')}>
            {t('yardOrders:fields.hoursValue', {
              approved: hoursText(o.hours_approved_milli, i18n.language),
              estimated: hoursText(o.hours_estimated_milli, i18n.language),
            })}
          </Detail>
          <Detail label={t('yardOrders:fields.deposit')}>
            <Money cents={o.deposit_cents ?? 0} />
          </Detail>
          {o.hold_reason ? (
            <Detail label={t('yardOrders:fields.holdReason')}>{o.hold_reason}</Detail>
          ) : null}
        </DetailGrid>
        {o.notes ? <p className="text-sm whitespace-pre-line">{o.notes}</p> : null}
      </Section>
      <Section title={t('yardOrders:summary.budget')}>
        <BudgetTable order={o} />
      </Section>
      <Section title={t('yardOrders:summary.lifting')}>
        <WorkOrderLiftingBookings workOrderId={o.id} vesselId={o.vessel_id} />
      </Section>
    </div>
  );
}

function BudgetTable({ order: o, origin }: { order: WorkOrder; origin?: string }) {
  const { t, i18n } = useT();
  const lines = origin ? o.lines.filter((l) => l.origin === origin) : o.lines;
  return (
    <SimpleTable
      head={[
        t('yardOrders:fields.item'),
        t('yardOrders:fields.description'),
        t('yardOrders:fields.qty'),
        t('yardOrders:fields.unit'),
        t('yardOrders:fields.net'),
        t('yardOrders:fields.vat'),
      ]}
      empty={t('yardOrders:empty.variations')}
      rows={lines.length}
    >
      {lines.map((l) => (
        <TableRow key={l.id}>
          <TableCell>{l.item_code ?? ''}</TableCell>
          <TableCell>{l.description}</TableCell>
          <TableCell className="tabular-nums">
            {formatQtyMilli(l.qty_milli, i18n.language)}
          </TableCell>
          <TableCell>
            <Money cents={l.unit_cents} />
          </TableCell>
          <TableCell>
            <Money cents={l.net_cents} />
          </TableCell>
          <TableCell>
            <Money cents={l.vat_cents} />
          </TableCell>
        </TableRow>
      ))}
    </SimpleTable>
  );
}

function TasksTab({ order: o }: { order: WorkOrder }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const tasks = useWorkTasks(marinaId, o.id);
  const progress = useTaskProgress(marinaId);
  const [adding, setAdding] = useState(false);
  const [filesOf, setFilesOf] = useState<WorkTask | null>(null);
  const write = can('yard.manage');
  if (tasks.isPending) return <LoadingState rows={3} />;
  if (tasks.isError)
    return <QueryErrorState error={tasks.error} onRetry={() => void tasks.refetch()} />;
  const title = (id: string) => tasks.data.items.find((x) => x.id === id)?.title ?? '';
  return (
    <Section
      title={t('yardOrders:tabs.tasks')}
      action={
        write ? (
          <Button size="sm" onClick={() => setAdding(true)}>
            {t('yardOrders:tasks.add')}
          </Button>
        ) : undefined
      }
    >
      <M4ProblemAlert error={progress.error} />
      <SimpleTable
        head={[
          t('yardOrders:fields.title'),
          t('yardOrders:fields.status'),
          t('yardOrders:fields.progress'),
          t('yardOrders:fields.estimated'),
          t('yardOrders:fields.assigned'),
          t('yardOrders:fields.approved'),
          t('yardOrders:fields.dependsOn'),
          '',
        ]}
        empty={t('yardOrders:empty.tasks')}
        rows={tasks.data.items.length}
      >
        {tasks.data.items.map((task) => (
          <TableRow key={task.id} data-testid={`task-${task.seq}`}>
            <TableCell>{task.title}</TableCell>
            <TableCell>
              <Badge variant={task.status === 'done' ? 'default' : 'secondary'}>
                {t(`yardOrders:taskStatus.${task.status}`)}
              </Badge>
            </TableCell>
            <TableCell className="min-w-32">
              <div className="flex items-center gap-2">
                <Progress
                  value={task.progress_bp / 100}
                  aria-label={t('yardOrders:fields.progress')}
                />
                <span className="tabular-nums">{formatBp(task.progress_bp, i18n.language)}</span>
              </div>
            </TableCell>
            <TableCell className="tabular-nums">
              {hoursText(task.estimated_hours_milli, i18n.language)}
            </TableCell>
            <TableCell className="tabular-nums">
              {hoursText(task.hours_assigned_milli, i18n.language)}
            </TableCell>
            <TableCell className="tabular-nums">
              {hoursText(task.hours_approved_milli, i18n.language)}
            </TableCell>
            <TableCell>{task.depends_on.map(title).join(', ')}</TableCell>
            <TableCell className="flex flex-wrap gap-2">
              {write && task.status !== 'done' && task.status !== 'cancelled' ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={progress.isPending}
                  onClick={() => progress.mutate({ id: task.id, done: true })}
                >
                  {t('yardOrders:tasks.markDone')}
                </Button>
              ) : null}
              <Button size="sm" variant="ghost" onClick={() => setFilesOf(task)}>
                {t('yardOrders:tasks.files')}
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      <NewTaskDialog orderId={o.id} open={adding} onClose={() => setAdding(false)} />
      <TaskFilesDialog task={filesOf} onClose={() => setFilesOf(null)} />
    </Section>
  );
}

function NewTaskDialog({
  orderId,
  open,
  onClose,
}: {
  orderId: string;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreateWorkTask(marinaId);
  const [title, setTitle] = useState('');
  const [hours, setHours] = useState('');
  const est = hours ? parseHours(hours) : 0;
  const submit = () =>
    create.mutate(
      { workOrderId: orderId, body: { title: title.trim(), estimated_hours_milli: est ?? 0 } },
      {
        onSuccess: () => {
          setTitle('');
          setHours('');
          onClose();
        },
      },
    );
  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('yardOrders:tasks.addTitle')}</DialogTitle>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="task-title">{t('yardOrders:fields.title')}</FieldLabel>
          <Input
            id="task-title"
            value={title}
            maxLength={200}
            onChange={(e) => setTitle(e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="task-hours">{t('yardOrders:fields.estimated')}</FieldLabel>
          <Input
            id="task-hours"
            inputMode="decimal"
            value={hours}
            onChange={(e) => setHours(e.target.value)}
          />
        </Field>
        <M4ProblemAlert error={create.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('yardOrders:dialog.cancel')}
          </Button>
          <Button disabled={!title.trim() || est === null || create.isPending} onClick={submit}>
            {t('yardOrders:dialog.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TaskFilesDialog({ task, onClose }: { task: WorkTask | null; onClose: () => void }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const files = useTaskFiles(marinaId, task?.id);
  const upload = useUploadTaskFile(marinaId);
  const link = useTaskFileLink(marinaId);
  return (
    <Dialog open={task !== null} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{task?.title ?? ''}</DialogTitle>
        </DialogHeader>
        <ul className="flex flex-col gap-2 text-sm">
          {(files.data?.items ?? []).map((f) => (
            <li key={f.file_id} className="flex items-center justify-between gap-2">
              <span>{f.kind}</span>
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  task &&
                  link.mutate(
                    { taskId: task.id, fileId: f.file_id },
                    { onSuccess: (l) => window.open(l.url, '_blank', 'noopener') },
                  )
                }
              >
                {t('yardOrders:tasks.open')}
              </Button>
            </li>
          ))}
        </ul>
        {can('yard.manage') && task ? (
          <Field>
            <FieldLabel htmlFor="task-file">{t('yardOrders:tasks.photo')}</FieldLabel>
            <Input
              id="task-file"
              type="file"
              accept="image/jpeg,image/png,application/pdf"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) upload.mutate({ taskId: task.id, file });
              }}
            />
          </Field>
        ) : null}
        <M4ProblemAlert error={upload.error ?? link.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('yardOrders:dialog.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function HoursTab({ order: o }: { order: WorkOrder }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const entries = useTimesheets(marinaId, { work_order_id: o.id });
  const employees = useEmployees(marinaId, {});
  const tasks = useWorkTasks(marinaId, o.id);
  const create = useCreateTimeEntry(marinaId);
  const [emp, setEmp] = useState('');
  const [task, setTask] = useState('');
  const [hours, setHours] = useState('');
  const milli = parseHours(hours);
  const name = (id: string) => {
    const e = employees.data?.items.find((x) => x.id === id);
    return e ? `${e.code} ${e.first_name} ${e.last_name}` : id;
  };
  if (entries.isPending) return <LoadingState rows={3} />;
  if (entries.isError)
    return <QueryErrorState error={entries.error} onRetry={() => void entries.refetch()} />;
  return (
    <Section title={t('yardOrders:tabs.hours')}>
      {can('yard.manage') ? (
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="entry-employee"
            label={t('yardOrders:fields.employee')}
            values={(employees.data?.items ?? []).map((e) => e.id)}
            value={emp}
            onChange={setEmp}
            labelOf={name}
          />
          <EnumSelect
            id="entry-task"
            label={t('yardOrders:fields.task')}
            values={(tasks.data?.items ?? []).map((x) => x.id)}
            value={task}
            onChange={setTask}
            labelOf={(id) => tasks.data?.items.find((x) => x.id === id)?.title ?? id}
            allLabel={t('yardOrders:filters.all')}
          />
          <Field className="w-auto">
            <FieldLabel htmlFor="entry-hours">{t('yardOrders:hours.hoursLabel')}</FieldLabel>
            <Input
              id="entry-hours"
              inputMode="decimal"
              value={hours}
              onChange={(e) => setHours(e.target.value)}
            />
          </Field>
          <Button
            disabled={!emp || milli === null || create.isPending}
            onClick={() =>
              create.mutate(
                {
                  employee_id: emp,
                  work_order_id: o.id,
                  task_id: task || undefined,
                  hours_milli: milli ?? 0,
                },
                { onSuccess: () => setHours('') },
              )
            }
          >
            {t('yardOrders:hours.add')}
          </Button>
        </div>
      ) : null}
      <M4ProblemAlert error={create.error} />
      <SimpleTable
        head={[
          t('yardOrders:fields.date'),
          t('yardOrders:fields.employee'),
          t('yardOrders:fields.hours'),
          t('yardOrders:fields.status'),
          t('yardOrders:fields.cost'),
        ]}
        empty={t('yardOrders:empty.entries')}
        rows={entries.data.items.length}
      >
        {entries.data.items.map((e) => (
          <TableRow key={e.id}>
            <TableCell>
              <IsoDate date={e.work_date} />
            </TableCell>
            <TableCell>{name(e.employee_id)}</TableCell>
            <TableCell className="tabular-nums">
              {hoursText(e.hours_milli, i18n.language)}
            </TableCell>
            <TableCell>
              <Badge variant={e.status === 'approved' ? 'default' : 'secondary'}>
                {t(`yardOrders:entryStatus.${e.status}`)}
              </Badge>
              {e.late ? <Badge variant="outline">{t('yardOrders:fields.late')}</Badge> : null}
            </TableCell>
            <TableCell>
              <Money cents={e.cost_cents ?? null} />
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
    </Section>
  );
}

function MaterialsTab({ order: o }: { order: WorkOrder }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const moves = useStockMovements(marinaId, o.id);
  const materials = useItems(marinaId, { type: 'material', active: true });
  const create = useCreateStockMovement(marinaId);
  const [item, setItem] = useState('');
  const [qty, setQty] = useState('');
  const milli = parseQty(qty);
  const items = materials.data?.pages.flatMap((p) => p.items) ?? [];
  const add = (kind: 'issue' | 'return') =>
    create.mutate(
      { work_order_id: o.id, item_id: item, kind, qty_milli: milli ?? 0 },
      { onSuccess: () => setQty('') },
    );
  if (moves.isPending) return <LoadingState rows={3} />;
  if (moves.isError)
    return <QueryErrorState error={moves.error} onRetry={() => void moves.refetch()} />;
  return (
    <Section
      title={t('yardOrders:tabs.materials')}
      description={t('yardOrders:materials.stockNote')}
    >
      {can('yard.manage') ? (
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="move-item"
            label={t('yardOrders:fields.item')}
            values={items.map((i) => i.id)}
            value={item}
            onChange={setItem}
            labelOf={(id) => {
              const it = items.find((x) => x.id === id);
              return it ? `${it.code} ${localName(it.name, i18n.language)}` : id;
            }}
          />
          <Field className="w-auto">
            <FieldLabel htmlFor="move-qty">{t('yardOrders:fields.qty')}</FieldLabel>
            <Input
              id="move-qty"
              inputMode="decimal"
              value={qty}
              onChange={(e) => setQty(e.target.value)}
            />
          </Field>
          <Button
            disabled={!item || milli === null || create.isPending}
            onClick={() => add('issue')}
          >
            {t('yardOrders:materials.issue')}
          </Button>
          <Button
            variant="outline"
            disabled={!item || milli === null || create.isPending}
            onClick={() => add('return')}
          >
            {t('yardOrders:materials.return')}
          </Button>
        </div>
      ) : null}
      <M4ProblemAlert error={create.error} />
      <SimpleTable
        head={[
          t('yardOrders:fields.date'),
          t('yardOrders:fields.item'),
          t('yardOrders:fields.kind'),
          t('yardOrders:fields.qty'),
          t('yardOrders:fields.unitCost'),
          t('yardOrders:fields.unitPrice'),
        ]}
        empty={t('yardOrders:empty.movements')}
        rows={moves.data.items.length}
      >
        {moves.data.items.map((m) => (
          <TableRow key={m.id}>
            <TableCell>
              <IsoDate date={m.moved_on} />
            </TableCell>
            <TableCell>{m.item_code ?? ''}</TableCell>
            <TableCell>
              {m.kind === 'issue'
                ? t('yardOrders:materials.kindIssue')
                : t('yardOrders:materials.kindReturn')}
            </TableCell>
            <TableCell className="tabular-nums">
              {formatQtyMilli(m.qty_milli, i18n.language)}
            </TableCell>
            <TableCell>
              <Money cents={m.unit_cost_cents} />
            </TableCell>
            <TableCell>
              <Money cents={m.unit_price_cents} />
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
    </Section>
  );
}

function ExternalTab({ order: o }: { order: WorkOrder }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const costs = useExternalCosts(marinaId, o.id);
  const workTypes = useWorkTypes(marinaId);
  const create = useCreateExternalCost(marinaId);
  const remove = useDeleteExternalCost(marinaId);
  const [open, setOpen] = useState(false);
  const [supplier, setSupplier] = useState('');
  const [reference, setReference] = useState('');
  const [cost, setCost] = useState<number | null>(null);
  const [price, setPrice] = useState<number | null>(null);
  const [billable, setBillable] = useState(true);
  const [wt, setWt] = useState('');
  if (costs.isPending) return <LoadingState rows={3} />;
  if (costs.isError)
    return <QueryErrorState error={costs.error} onRetry={() => void costs.refetch()} />;
  const wtItems = workTypes.data?.items ?? [];
  const submit = () =>
    create.mutate(
      {
        workOrderId: o.id,
        body: {
          supplier_name: supplier.trim(),
          reference: reference.trim() || undefined,
          cost_cents: cost ?? 0,
          billable,
          ...(price !== null ? { price_cents: price } : {}),
          ...(wt ? { work_type_id: wt } : {}),
        },
      },
      {
        onSuccess: () => {
          setOpen(false);
          setSupplier('');
          setReference('');
          setCost(null);
          setPrice(null);
        },
      },
    );
  return (
    <Section
      title={t('yardOrders:tabs.external')}
      action={
        can('yard.manage') ? (
          <Button size="sm" onClick={() => setOpen(true)}>
            {t('yardOrders:external.add')}
          </Button>
        ) : undefined
      }
    >
      <M4ProblemAlert error={remove.error} />
      <SimpleTable
        head={[
          t('yardOrders:fields.date'),
          t('yardOrders:fields.supplier'),
          t('yardOrders:fields.reference'),
          t('yardOrders:fields.cost'),
          t('yardOrders:fields.price'),
          '',
        ]}
        empty={t('yardOrders:empty.costs')}
        rows={costs.data.items.length}
      >
        {costs.data.items.map((c) => (
          <TableRow key={c.id}>
            <TableCell>
              <IsoDate date={c.incurred_on} />
            </TableCell>
            <TableCell>{c.supplier_name}</TableCell>
            <TableCell>{c.reference}</TableCell>
            <TableCell>
              <Money cents={c.cost_cents} />
            </TableCell>
            <TableCell>{c.billable ? <Money cents={c.price_cents} /> : null}</TableCell>
            <TableCell>
              {can('yard.manage') ? (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => remove.mutate({ id: c.id, version: c.version })}
                >
                  {t('yardOrders:external.delete')}
                </Button>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('yardOrders:external.addTitle')}</DialogTitle>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="ext-supplier">{t('yardOrders:fields.supplier')}</FieldLabel>
            <Input
              id="ext-supplier"
              value={supplier}
              maxLength={100}
              onChange={(e) => setSupplier(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="ext-ref">{t('yardOrders:fields.reference')}</FieldLabel>
            <Input
              id="ext-ref"
              value={reference}
              maxLength={60}
              onChange={(e) => setReference(e.target.value)}
            />
          </Field>
          <EnumSelect
            id="ext-wt"
            label={t('yardOrders:fields.workType')}
            values={wtItems.map((w) => w.id)}
            value={wt}
            onChange={setWt}
            labelOf={(id) => wtItems.find((w) => w.id === id)?.code ?? id}
            allLabel={t('yardOrders:filters.all')}
          />
          <Field>
            <FieldLabel htmlFor="ext-cost">{t('yardOrders:fields.cost')}</FieldLabel>
            <MoneyInput id="ext-cost" value={cost} onChange={setCost} locale={i18n.language} />
          </Field>
          <div className="flex items-center gap-2">
            <Switch id="ext-billable" checked={billable} onCheckedChange={setBillable} />
            <FieldLabel htmlFor="ext-billable">{t('yardOrders:fields.billable')}</FieldLabel>
          </div>
          {billable ? (
            <Field>
              <FieldLabel htmlFor="ext-price">{t('yardOrders:fields.price')}</FieldLabel>
              <MoneyInput id="ext-price" value={price} onChange={setPrice} locale={i18n.language} />
              <FieldDescription>{t('yardOrders:external.priceHint')}</FieldDescription>
            </Field>
          ) : null}
          <M4ProblemAlert error={create.error} />
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              {t('yardOrders:dialog.cancel')}
            </Button>
            <Button disabled={!supplier.trim() || !cost || create.isPending} onClick={submit}>
              {t('yardOrders:dialog.save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Section>
  );
}

function AccountTab({ order: o }: { order: WorkOrder }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const acc = useFinalAccount(marinaId, o.id);
  const workTypes = useWorkTypes(marinaId);
  if (acc.isPending) return <LoadingState rows={4} />;
  if (acc.isError) return <QueryErrorState error={acc.error} onRetry={() => void acc.refetch()} />;
  const a = acc.data;
  const wtCode = (id: string) => workTypes.data?.items.find((w) => w.id === id)?.code ?? id;
  const tot = a.totals;
  return (
    <div className="flex flex-col gap-4" data-testid="final-account">
      <Section title={t('yardOrders:account.totals')}>
        <DetailGrid>
          <Detail label={t('yardOrders:fields.revenue')}>
            <Money cents={tot.revenue_cents} />
          </Detail>
          <Detail label={t('yardOrders:fields.cost')}>
            <Money cents={tot.cost_cents} />
          </Detail>
          <Detail label={t('yardOrders:fields.margin')}>
            <Money cents={tot.margin_cents} />
          </Detail>
          <Detail label={t('yardOrders:fields.marginPct')}>
            {tot.margin_bp !== undefined && tot.margin_bp !== null
              ? formatBp(tot.margin_bp, i18n.language)
              : ''}
          </Detail>
          <Detail label={t('yardOrders:account.hoursLabour')}>
            {hoursText(tot.hours_labour_milli, i18n.language)}
          </Detail>
          <Detail label={t('yardOrders:fields.gross')}>
            <Money cents={tot.gross_cents} />
          </Detail>
          <Detail label={t('yardOrders:account.quoteNet')}>
            <Money cents={a.quote_net_cents} />
          </Detail>
          <Detail label={t('yardOrders:account.delta')}>
            <Money cents={a.delta_net_cents} />
          </Detail>
          <Detail label={t('yardOrders:account.charged')}>
            <Money cents={a.charged_gross_cents} />
          </Detail>
          <Detail label={t('yardOrders:account.remaining')}>
            <Money cents={a.remaining_gross_cents} />
          </Detail>
        </DetailGrid>
      </Section>
      {groupAccount(a).map((g) => (
        <Section key={g.source} title={t(`yardOrders:account.source.${g.source}`)}>
          <SimpleTable
            head={[
              t('yardOrders:fields.item'),
              t('yardOrders:fields.workType'),
              t('yardOrders:fields.qty'),
              t('yardOrders:fields.revenue'),
              t('yardOrders:fields.cost'),
              t('yardOrders:fields.margin'),
              t('yardOrders:fields.budgetQty'),
              t('yardOrders:fields.budgetNet'),
              t('yardOrders:fields.delta'),
            ]}
            empty=""
            rows={g.lines.length}
          >
            {g.lines.map((l) => (
              <TableRow key={l.key} data-testid={`account-line-${l.item_code ?? l.key}`}>
                <TableCell>{l.item_code ?? l.description ?? ''}</TableCell>
                <TableCell>{wtCode(l.work_type_id)}</TableCell>
                <TableCell className="tabular-nums">
                  {formatQtyMilli(l.qty_milli, i18n.language)}
                </TableCell>
                <TableCell>
                  <Money cents={l.revenue_cents} />
                </TableCell>
                <TableCell>
                  <Money cents={l.cost_cents} />
                </TableCell>
                <TableCell>
                  <Money cents={l.margin_cents} />
                </TableCell>
                <TableCell className="tabular-nums">
                  {formatQtyMilli(l.budget_qty_milli, i18n.language)}
                </TableCell>
                <TableCell>
                  <Money cents={l.budget_net_cents} />
                </TableCell>
                <TableCell>
                  <Money cents={l.delta_net_cents} />
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </Section>
      ))}
      <Section title={t('yardOrders:account.byWorkType')}>
        <SimpleTable
          head={[
            t('yardOrders:fields.workType'),
            t('yardOrders:fields.revenue'),
            t('yardOrders:fields.cost'),
            t('yardOrders:fields.margin'),
            t('yardOrders:fields.marginPct'),
          ]}
          empty=""
          rows={a.by_work_type.length}
        >
          {a.by_work_type.map((w) => (
            <TableRow key={w.work_type_id}>
              <TableCell>{wtCode(w.work_type_id)}</TableCell>
              <TableCell>
                <Money cents={w.revenue_cents} />
              </TableCell>
              <TableCell>
                <Money cents={w.cost_cents} />
              </TableCell>
              <TableCell>
                <Money cents={w.margin_cents} />
              </TableCell>
              <TableCell>
                {w.margin_bp !== undefined && w.margin_bp !== null
                  ? formatBp(w.margin_bp, i18n.language)
                  : ''}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      </Section>
    </div>
  );
}

function BillingsTab({ order: o }: { order: WorkOrder }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const bills = useWorkOrderBillings(marinaId, o.id);
  const billing = useWorkOrderBilling(marinaId);
  const [kind, setKind] = useState<'progress' | 'final' | 'adjustment'>(
    o.status === 'invoiced' ? 'adjustment' : 'final',
  );
  const [amount, setAmount] = useState<number | null>(null);
  const [preview, setPreview] = useState<WorkOrderBillingResult | null>(null);
  const body = () => ({ kind, ...(kind === 'progress' && amount ? { amount_cents: amount } : {}) });
  const { i18n } = useT();
  if (bills.isPending) return <LoadingState rows={3} />;
  if (bills.isError)
    return <QueryErrorState error={bills.error} onRetry={() => void bills.refetch()} />;
  return (
    <Section title={t('yardOrders:tabs.billings')} description={undefined}>
      <DetailGrid>
        <Detail label={t('yardOrders:billings.charged')}>
          <Money cents={bills.data.charged_gross_cents} />
        </Detail>
      </DetailGrid>
      <SimpleTable
        head={[
          t('yardOrders:fields.seq'),
          t('yardOrders:fields.kind'),
          t('yardOrders:fields.status'),
          t('yardOrders:fields.charge'),
          t('yardOrders:fields.gross'),
        ]}
        empty={t('yardOrders:empty.billings')}
        rows={bills.data.items.length}
      >
        {bills.data.items.map((b) => (
          <TableRow key={b.id}>
            <TableCell>{b.seq}</TableCell>
            <TableCell>{t(`yardOrders:billings.kind.${b.kind}`)}</TableCell>
            <TableCell>{t(`yardOrders:billings.status.${b.status}`)}</TableCell>
            <TableCell>{b.charge_number ?? ''}</TableCell>
            <TableCell>
              <Money cents={b.gross_cents} />
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      {can('charges.manage') ? (
        <div className="flex flex-col gap-3" data-testid="billing-form">
          <div className="flex flex-wrap items-end gap-3">
            <EnumSelect
              id="billing-kind"
              label={t('yardOrders:fields.kind')}
              values={['progress', 'final', 'adjustment'] as const}
              value={kind}
              onChange={(v) => {
                if (v) setKind(v);
                setPreview(null);
              }}
              labelOf={(v) => t(`yardOrders:billings.kind.${v}`)}
            />
            {kind === 'progress' ? (
              <Field className="w-auto">
                <FieldLabel htmlFor="billing-amount">{t('yardOrders:fields.amount')}</FieldLabel>
                <MoneyInput
                  id="billing-amount"
                  value={amount}
                  onChange={setAmount}
                  locale={i18n.language}
                />
                <FieldDescription>{t('yardOrders:billings.amountHint')}</FieldDescription>
              </Field>
            ) : null}
            <Button
              variant="outline"
              disabled={billing.isPending}
              onClick={() =>
                billing.mutate(
                  { workOrderId: o.id, body: body(), dryRun: true },
                  { onSuccess: setPreview },
                )
              }
            >
              {t('yardOrders:billings.preview')}
            </Button>
            <Button
              disabled={billing.isPending || !preview}
              onClick={() =>
                billing.mutate(
                  { workOrderId: o.id, body: body(), dryRun: false },
                  { onSuccess: () => setPreview(null) },
                )
              }
            >
              {t('yardOrders:billings.create')}
            </Button>
          </div>
          <M4ProblemAlert error={billing.error} />
          {preview ? (
            <div data-testid="billing-preview" className="flex flex-col gap-2">
              <SimpleTable
                head={[
                  t('yardOrders:fields.description'),
                  t('yardOrders:fields.net'),
                  t('yardOrders:fields.vat'),
                  t('yardOrders:fields.gross'),
                ]}
                empty=""
                rows={preview.lines.length}
              >
                {preview.lines.map((l) => (
                  <TableRow key={l.plan_key}>
                    <TableCell>{l.description}</TableCell>
                    <TableCell>
                      <Money cents={l.net_cents} />
                    </TableCell>
                    <TableCell>
                      <Money cents={l.vat_cents} />
                    </TableCell>
                    <TableCell>
                      <Money cents={l.gross_cents} />
                    </TableCell>
                  </TableRow>
                ))}
              </SimpleTable>
              <DetailGrid>
                <Detail label={t('yardOrders:billings.previewTotal')}>
                  <Money cents={preview.gross_cents} />
                </Detail>
              </DetailGrid>
            </div>
          ) : null}
        </div>
      ) : null}
    </Section>
  );
}

function VariationsTab({ order: o }: { order: WorkOrder }) {
  const { t } = useT();
  return (
    <Section
      title={t('yardOrders:tabs.variations')}
      description={t('yardOrders:variations.description')}
    >
      <BudgetTable order={o} origin="variation" />
    </Section>
  );
}
