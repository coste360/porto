import { ChevronLeftIcon, ChevronRightIcon, SparklesIcon, XIcon } from 'lucide-react';
import { useEffect, useMemo, useReducer, useState, type KeyboardEvent } from 'react';
import { toast } from 'sonner';

import {
  toProblem,
  useDeleteAssignment,
  usePlanner,
  useProposeYardPlan,
  usePutAssignments,
  type Assignment,
  type Planner,
  type PlannerRow,
  type PlannerTask,
  type YardPlanProposal,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { YardPlannerNav } from '../components/kit';
import { useQualificationIndex } from '../lib/qualifications';
import { heatClass } from '../lib/load';
import {
  datesOf,
  defaultHours,
  dropFeedback,
  existingHours,
  gridReducer,
  hoursLabel,
  initialGrid,
  parseHours,
  periodOf,
  shiftAnchor,
  trayTasks,
  type PlannerView,
} from '../lib/planner';

const ORDER_COLOURS = [
  'border-chart-1',
  'border-chart-2',
  'border-chart-3',
  'border-chart-4',
  'border-chart-5',
];

function colourOf(orderId: string, orders: readonly string[]): string {
  const i = orders.indexOf(orderId);
  return ORDER_COLOURS[(i < 0 ? 0 : i) % ORDER_COLOURS.length]!;
}

/**
 * Yard planner (m10-plan §6, §14.3): employees × days with capacity and assigned hours, the tray "Da pianificare",
 * pointer drag with a keyboard alternative (pick a task, arrows, Enter), qualification and capacity feedback with
 * explicit confirmation, and the auto-plan proposal confirmed as a batch.
 */
export function YardPlannerPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [view, setView] = useState<PlannerView>('week');
  const [anchor, setAnchor] = useState<string | null>(null);
  const [department, setDepartment] = useState('');
  const a = anchor ?? today;
  const { from, to } = periodOf(view, a);
  const planner = usePlanner(marinaId, from, to, department.trim() || undefined);
  const manage = can('yard.manage');
  const [proposal, setProposal] = useState(false);
  return (
    <RequirePermission allowed={can('yard.read') || manage}>
      <div className="flex w-full flex-col gap-4">
        <PageHeader
          title={t('yardPlanner:planner.title')}
          description={t('yardPlanner:planner.description')}
          actions={
            manage ? (
              <Button variant="outline" onClick={() => setProposal(true)}>
                <SparklesIcon />
                {t('yardPlanner:planner.autoPlan')}
              </Button>
            ) : null
          }
        />
        <YardPlannerNav marinaId={marinaId} current="planner" />
        <div className="flex flex-wrap items-end gap-3">
          <ToggleGroup
            type="single"
            variant="outline"
            value={view}
            onValueChange={(v) => v && setView(v as PlannerView)}
            aria-label={t('yardPlanner:planner.view')}
          >
            {(['day', 'week', 'month'] as const).map((v) => (
              <ToggleGroupItem key={v} value={v}>
                {t(`yardPlanner:planner.views.${v}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('yardPlanner:planner.previous')}
              onClick={() => setAnchor(shiftAnchor(view, a, -1))}
            >
              <ChevronLeftIcon />
            </Button>
            <span className="text-sm font-medium" data-testid="planner-period">
              {from === to ? formatIsoDate(from) : `${formatIsoDate(from)} – ${formatIsoDate(to)}`}
            </span>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('yardPlanner:planner.next')}
              onClick={() => setAnchor(shiftAnchor(view, a, 1))}
            >
              <ChevronRightIcon />
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setAnchor(null)}>
              {t('yardPlanner:planner.today')}
            </Button>
          </div>
          <Field className="w-48">
            <FieldLabel htmlFor="planner-department">
              {t('yardPlanner:fields.department')}
            </FieldLabel>
            <Input
              id="planner-department"
              value={department}
              maxLength={40}
              onChange={(e) => setDepartment(e.target.value)}
            />
          </Field>
        </div>
        {planner.isPending ? (
          <LoadingState rows={6} />
        ) : planner.isError ? (
          <QueryErrorState error={planner.error} onRetry={() => void planner.refetch()} />
        ) : (
          <PlannerGrid
            marinaId={marinaId}
            planner={planner.data}
            manage={manage}
            dates={datesOf(from, to)}
          />
        )}
        {proposal ? (
          <ProposalDialog
            marinaId={marinaId}
            planner={planner.data}
            onClose={() => setProposal(false)}
          />
        ) : null}
      </div>
    </RequirePermission>
  );
}

function PlannerGrid({
  marinaId,
  planner,
  manage,
  dates,
}: {
  marinaId: string;
  planner: Planner;
  manage: boolean;
  dates: string[];
}) {
  const { t } = useT();
  const quals = useQualificationIndex(marinaId);
  const del = useDeleteAssignment(marinaId);
  const [grid, dispatch] = useReducer(gridReducer, initialGrid);
  const tray = trayTasks(planner.tasks);
  const taskById = useMemo(
    () => new Map(planner.tasks.map((x) => [x.task_id, x])),
    [planner.tasks],
  );
  const orders = useMemo(
    () => [...new Set(planner.tasks.map((x) => x.work_order_id))],
    [planner.tasks],
  );
  const rows = planner.rows;

  // Pointer drag: the pointer may be captured by the tray item (touch), so the cell under it is found by position.
  useEffect(() => {
    if (!grid.dragging) return;
    const move = (e: PointerEvent) => {
      const el = document
        .elementFromPoint(e.clientX, e.clientY)
        ?.closest<HTMLElement>('[data-cell]');
      if (!el) return dispatch({ type: 'over', cell: null });
      dispatch({
        type: 'over',
        cell: { row: Number(el.dataset.row), col: Number(el.dataset.col) },
      });
    };
    const up = () => dispatch({ type: 'drop' });
    document.addEventListener('pointermove', move);
    document.addEventListener('pointerup', up);
    return () => {
      document.removeEventListener('pointermove', move);
      document.removeEventListener('pointerup', up);
    };
  }, [grid.dragging]);

  const onKey = (e: KeyboardEvent) => {
    if (!grid.taskId) return;
    if (['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Enter', 'Escape'].includes(e.key)) {
      e.preventDefault();
      dispatch({ type: 'key', key: e.key, rows: rows.length, cols: dates.length });
    }
  };
  const picked = grid.taskId ? taskById.get(grid.taskId) : undefined;
  const target =
    grid.target && picked ? { row: rows[grid.target.row], date: dates[grid.target.col] } : null;

  return (
    <div className="flex flex-col gap-4 lg:flex-row">
      {manage ? (
        <aside className="flex flex-col gap-2 lg:w-64" aria-label={t('yardPlanner:planner.tray')}>
          <h2 className="text-sm font-semibold">{t('yardPlanner:planner.tray')}</h2>
          {tray.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('yardPlanner:planner.trayEmpty')}</p>
          ) : null}
          <ul className="flex flex-col gap-2" data-testid="planner-tray">
            {tray.map((task) => (
              <li key={task.task_id}>
                <button
                  type="button"
                  data-testid="tray-task"
                  aria-pressed={grid.taskId === task.task_id}
                  className={`bg-card hover:bg-muted flex w-full touch-none flex-col items-start gap-1 rounded-md border border-l-4 p-2 text-left text-sm ${colourOf(task.work_order_id, orders)} ${grid.taskId === task.task_id ? 'ring-ring ring-2' : ''}`}
                  onPointerDown={(e) => {
                    if (e.button === 0) dispatch({ type: 'dragStart', taskId: task.task_id });
                  }}
                  onClick={() => dispatch({ type: 'pick', taskId: task.task_id })}
                >
                  <span className="font-medium">{task.title}</span>
                  <span className="text-muted-foreground text-xs">
                    {task.work_order_number}
                    {task.vessel_name ? ` · ${task.vessel_name}` : ''}
                  </span>
                  <span className="text-xs">
                    {t('yardPlanner:planner.toPlan', {
                      hours: hoursLabel(task.remaining_milli - task.assigned_milli),
                    })}
                    {task.qualification_id
                      ? ` · ${quals.byId.get(task.qualification_id)?.code ?? ''}`
                      : ''}
                  </span>
                </button>
              </li>
            ))}
          </ul>
          {picked ? (
            <p role="status" className="text-muted-foreground text-xs">
              {t('yardPlanner:planner.keyboardHint', { title: picked.title })}
            </p>
          ) : null}
        </aside>
      ) : null}
      <div
        className="min-w-0 flex-1 overflow-x-auto rounded-md border"
        tabIndex={0}
        role="grid"
        aria-label={t('yardPlanner:planner.grid')}
        aria-rowcount={rows.length}
        onKeyDown={onKey}
        data-testid="planner-grid"
      >
        {rows.length === 0 ? (
          <p className="text-muted-foreground p-4 text-sm">
            {t('yardPlanner:planner.noEmployees')}
          </p>
        ) : (
          <table className="w-full border-collapse text-sm">
            <thead>
              <tr>
                <th className="bg-muted sticky left-0 p-2 text-left font-medium">
                  {t('yardPlanner:fields.employee')}
                </th>
                {dates.map((d) => (
                  <th key={d} className="bg-muted min-w-28 p-2 text-left font-medium">
                    {formatIsoDate(d)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, ri) => (
                <tr key={row.employee_id} className="border-t">
                  <th
                    scope="row"
                    className="bg-card sticky left-0 p-2 text-left align-top font-medium"
                  >
                    <div>{row.code}</div>
                    <div className="text-muted-foreground text-xs font-normal">
                      {row.display_name}
                    </div>
                  </th>
                  {dates.map((d, ci) => (
                    <PlannerCell
                      key={d}
                      row={row}
                      date={d}
                      rowIndex={ri}
                      colIndex={ci}
                      assignments={planner.assignments}
                      taskById={taskById}
                      orders={orders}
                      highlighted={
                        !!grid.cursor &&
                        grid.cursor.row === ri &&
                        grid.cursor.col === ci &&
                        !!grid.taskId
                      }
                      manage={manage}
                      onDelete={(as) =>
                        del.mutate(
                          { id: as.id, version: as.version },
                          { onError: (err) => toast.error(t(toProblem(err).i18nKey)) },
                        )
                      }
                    />
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      {target && target.row && target.date && picked ? (
        <AssignDialog
          marinaId={marinaId}
          row={target.row}
          date={target.date}
          task={picked}
          existing={existingHours(
            planner.assignments,
            picked.task_id,
            target.row.employee_id,
            target.date,
          )}
          onClose={() => dispatch({ type: 'clear' })}
        />
      ) : null}
    </div>
  );
}

function PlannerCell({
  row,
  date,
  rowIndex,
  colIndex,
  assignments,
  taskById,
  orders,
  highlighted,
  manage,
  onDelete,
}: {
  row: PlannerRow;
  date: string;
  rowIndex: number;
  colIndex: number;
  assignments: readonly Assignment[];
  taskById: Map<string, PlannerTask>;
  orders: readonly string[];
  highlighted: boolean;
  manage: boolean;
  onDelete: (a: Assignment) => void;
}) {
  const { t } = useT();
  const day = row.days.find((x) => x.date === date);
  const mine = assignments.filter((x) => x.employee_id === row.employee_id && x.work_date === date);
  return (
    <td
      data-cell
      data-row={rowIndex}
      data-col={colIndex}
      data-testid={`planner-cell-${row.code}-${date}`}
      aria-selected={highlighted}
      className={`p-1 align-top ${highlighted ? 'ring-ring ring-2 ring-inset' : ''}`}
    >
      <div className="flex flex-col gap-1">
        {day ? (
          <span
            className={`rounded px-1 text-xs ${heatClass(day.load_bp ?? null, day.overload, day.assigned_milli)}`}
            data-testid="cell-load"
          >
            {day.absence
              ? t(`yardPlanner:absenceKinds.${day.absence}`)
              : `${hoursLabel(day.assigned_milli)} / ${hoursLabel(day.capacity_milli)}`}
          </span>
        ) : null}
        {mine.map((as) => {
          const task = taskById.get(as.task_id);
          return (
            <span
              key={as.id}
              data-testid="planner-block"
              className={`bg-card flex items-center justify-between gap-1 rounded border border-l-4 px-1 text-xs ${colourOf(as.work_order_id, orders)}`}
            >
              <span className="truncate">
                {task?.title ?? t('yardPlanner:planner.otherTask')} · {hoursLabel(as.hours_milli)}
                {as.overload || as.forced_qualification ? ' !' : ''}
              </span>
              {manage ? (
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground"
                  aria-label={t('yardPlanner:planner.removeBlock')}
                  onClick={() => onDelete(as)}
                >
                  <XIcon className="size-3" />
                </button>
              ) : null}
            </span>
          );
        })}
      </div>
    </td>
  );
}

function AssignDialog({
  marinaId,
  row,
  date,
  task,
  existing,
  onClose,
}: {
  marinaId: string;
  row: PlannerRow;
  date: string;
  task: PlannerTask;
  existing: number;
  onClose: () => void;
}) {
  const { t } = useT();
  const put = usePutAssignments(marinaId);
  const [text, setText] = useState(hoursLabel(defaultHours(row, date, task)));
  const [force, setForce] = useState(false);
  const [overload, setOverload] = useState(false);
  const hours = parseHours(text);
  const fb = dropFeedback(row, date, task, hours ?? 0);
  const ready =
    hours !== null && (!fb.qualificationMismatch || force) && (!fb.overCapacity || overload);
  const save = () => {
    if (hours === null) return;
    put.mutate(
      {
        changes: [
          {
            task_id: task.task_id,
            employee_id: row.employee_id,
            work_date: date,
            hours_milli: existing + hours,
            task_version: task.version,
          },
        ],
        force_qualification: force,
        allow_overload: overload,
      },
      {
        onSuccess: () => {
          toast.success(t('yardPlanner:planner.assigned'));
          onClose();
        },
      },
    );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent data-testid="assign-dialog">
        <DialogHeader>
          <DialogTitle>{t('yardPlanner:planner.assignTitle')}</DialogTitle>
          <DialogDescription>
            {t('yardPlanner:planner.assignWhat', {
              title: task.title,
              employee: row.code,
              date: formatIsoDate(date),
            })}
          </DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="assign-hours">{t('yardPlanner:fields.hours')}</FieldLabel>
          <Input
            id="assign-hours"
            inputMode="decimal"
            value={text}
            aria-invalid={hours === null}
            onChange={(e) => setText(e.target.value)}
          />
        </Field>
        {existing > 0 ? (
          <p className="text-muted-foreground text-sm">
            {t('yardPlanner:planner.alreadyAssigned', { hours: hoursLabel(existing) })}
          </p>
        ) : null}
        {fb.qualificationMismatch ? (
          <Alert>
            <AlertDescription className="flex items-center gap-2">
              <Checkbox
                id="assign-force"
                checked={force}
                onCheckedChange={(v) => setForce(v === true)}
              />
              <Label htmlFor="assign-force">{t('yardPlanner:planner.forceQualification')}</Label>
            </AlertDescription>
          </Alert>
        ) : null}
        {fb.overCapacity ? (
          <Alert>
            <AlertDescription className="flex items-center gap-2">
              <Checkbox
                id="assign-overload"
                checked={overload}
                onCheckedChange={(v) => setOverload(v === true)}
              />
              <Label htmlFor="assign-overload">
                {t('yardPlanner:planner.allowOverload', {
                  assigned: hoursLabel(fb.assignedAfter),
                  capacity: hoursLabel(fb.capacity),
                })}
              </Label>
            </AlertDescription>
          </Alert>
        ) : null}
        <ProblemAlert error={put.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('yardPlanner:actions.cancel')}
          </Button>
          <Button onClick={save} disabled={!ready || put.isPending}>
            {t('yardPlanner:planner.assign')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ProposalDialog({
  marinaId,
  planner,
  onClose,
}: {
  marinaId: string;
  planner: Planner | undefined;
  onClose: () => void;
}) {
  const { t } = useT();
  const propose = useProposeYardPlan(marinaId);
  const put = usePutAssignments(marinaId);
  const [horizon, setHorizon] = useState('14');
  const [result, setResult] = useState<YardPlanProposal | null>(null);
  const tasks = new Map((planner?.tasks ?? []).map((x) => [x.task_id, x]));
  const emps = new Map((planner?.rows ?? []).map((r) => [r.employee_id, r.code]));
  const run = () =>
    propose.mutate(
      { horizon_days: Math.min(Math.max(Number(horizon) || 14, 1), 90) },
      { onSuccess: setResult },
    );
  const confirm = () =>
    result &&
    put.mutate(
      { changes: result.changes },
      {
        onSuccess: () => {
          toast.success(t('yardPlanner:proposal.confirmed', { count: result.changes.length }));
          onClose();
        },
      },
    );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl" data-testid="proposal-dialog">
        <DialogHeader>
          <DialogTitle>{t('yardPlanner:proposal.title')}</DialogTitle>
          <DialogDescription>{t('yardPlanner:proposal.description')}</DialogDescription>
        </DialogHeader>
        <div className="flex items-end gap-2">
          <Field className="w-32">
            <FieldLabel htmlFor="proposal-horizon">{t('yardPlanner:proposal.horizon')}</FieldLabel>
            <Input
              id="proposal-horizon"
              inputMode="numeric"
              value={horizon}
              onChange={(e) => setHorizon(e.target.value.replace(/\D/g, ''))}
            />
          </Field>
          <Button variant="outline" onClick={run} disabled={propose.isPending}>
            {t('yardPlanner:proposal.compute')}
          </Button>
        </div>
        {result ? (
          <div className="flex max-h-80 flex-col gap-2 overflow-y-auto">
            {result.changes.length === 0 ? (
              <p className="text-muted-foreground text-sm">{t('yardPlanner:proposal.nothing')}</p>
            ) : (
              <ul className="flex flex-col gap-1 text-sm" data-testid="proposal-changes">
                {result.changes.map((c) => (
                  <li
                    key={`${c.task_id}-${c.employee_id}-${c.work_date}`}
                    className="flex justify-between gap-2"
                  >
                    <span>
                      {formatIsoDate(c.work_date)} · {emps.get(c.employee_id) ?? '—'} ·{' '}
                      {tasks.get(c.task_id)?.title ?? '—'}
                    </span>
                    <Badge variant="secondary">{hoursLabel(c.hours_milli)}</Badge>
                  </li>
                ))}
              </ul>
            )}
            {result.unplaced.length > 0 ? (
              <Alert>
                <AlertDescription>
                  {t('yardPlanner:proposal.unplaced', {
                    tasks: result.unplaced
                      .map(
                        (u) =>
                          `${tasks.get(u.task_id)?.title ?? '—'} (${hoursLabel(u.remaining_milli)})`,
                      )
                      .join(', '),
                  })}
                </AlertDescription>
              </Alert>
            ) : null}
          </div>
        ) : null}
        <ProblemAlert error={propose.error ?? put.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('yardPlanner:actions.cancel')}
          </Button>
          <Button
            onClick={confirm}
            disabled={!result || result.changes.length === 0 || put.isPending}
          >
            {t('yardPlanner:proposal.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
