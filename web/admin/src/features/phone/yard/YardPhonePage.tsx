import { ArrowLeftIcon, CameraIcon, HammerIcon, PlayIcon, SquareIcon } from 'lucide-react';
import { useEffect, useReducer, useState } from 'react';
import { toast } from 'sonner';

import { useEquipmentBookings } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { useQueue } from '@porto/shared/offline';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Label } from '@porto/shared/ui/label';
import { EmptyState, ProblemAlert } from '@porto/shared/ui/patterns';
import { Slider } from '@porto/shared/ui/slider';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, RequirePermission } from '../../../components/PageStates';
import { canUseYardPhone } from '../../../lib/landing';
import { DayGrid } from '../../lifting/components/DayGrid';
import { useMarinaToday } from '../../registry/lib/today';
import { hoursLabel } from '../../yard-planner/lib/planner';
import {
  useApproveTimesheets,
  useCreateTimeEntry,
  useMyYardWork,
  useStartTimer,
  useStopTimer,
  useSubmittedTimesheets,
  useUpdateTaskProgress,
  useUploadTaskPhoto,
  type MyYardAssignment,
} from './api';
import {
  addHours,
  durationLabel,
  elapsedMinutes,
  idleTimer,
  timerReducer,
  type TimerState,
} from './timer';

/**
 * Yard phone (m10-plan §14.3, landing of yard and yard_mgr): today's assignments from the offline cache, the task
 * with timer, manual hours, checklist, progress and camera photo (all queued offline), the lifting day (read only)
 * and, for managers, the approval of submitted hours (online).
 */
export function YardPhonePage() {
  const { t } = useT();
  const { marina, marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const work = useMyYardWork(marinaId);
  const queue = useQueue(marinaId);
  const [timer, dispatch] = useReducer(timerReducer, idleTimer);
  const [open, setOpen] = useState<MyYardAssignment | null>(null);
  const items = work.data?.assignments ?? [];
  const running = work.data?.running_timer;
  useEffect(() => {
    dispatch({
      type: 'server',
      running: running
        ? { id: running.id, task_id: running.task_id, started_at: running.started_at }
        : null,
      titleOf: (id) => items.find((a) => a.task_id === id)?.task_title ?? '',
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running?.id, running?.task_id]);
  const todays = items.filter((a) => a.work_date === today);
  const later = items.filter((a) => a.work_date > today);
  return (
    <RequirePermission allowed={canUseYardPhone(marina?.permissions ?? [])}>
      <div data-testid="yard-phone" className="flex flex-col gap-4 p-4">
        {open ? (
          <TaskDetail
            marinaId={marinaId}
            assignment={open}
            timer={timer}
            dispatch={dispatch}
            today={today}
            onBack={() => setOpen(null)}
          />
        ) : (
          <Tabs defaultValue="today">
            <TabsList className="w-full">
              <TabsTrigger value="today">{t('yardPlanner:phone.tabs.today')}</TabsTrigger>
              <TabsTrigger value="lift">{t('yardPlanner:phone.tabs.lift')}</TabsTrigger>
              {can('yard.manage') ? (
                <TabsTrigger value="approve">{t('yardPlanner:phone.tabs.approve')}</TabsTrigger>
              ) : null}
            </TabsList>
            <TabsContent value="today" className="flex flex-col gap-3">
              <h1 className="text-xl font-semibold">{t('yardPlanner:phone.title')}</h1>
              {queue.pending > 0 ? (
                <p
                  role="status"
                  className="text-muted-foreground text-sm"
                  data-testid="yard-queued"
                >
                  {t('yardPlanner:phone.queued', { count: queue.pending })}
                </p>
              ) : null}
              {timer.status === 'running' ? (
                <p className="bg-muted rounded-md p-2 text-sm" data-testid="running-timer">
                  {t('yardPlanner:phone.runningOn', { title: timer.title })}
                </p>
              ) : null}
              {work.isPending ? (
                <LoadingState rows={3} />
              ) : work.data && !work.data.employee_id ? (
                <EmptyState icon={<HammerIcon />} title={t('yardPlanner:phone.noEmployee')} />
              ) : todays.length === 0 ? (
                <EmptyState icon={<HammerIcon />} title={t('yardPlanner:phone.empty')} />
              ) : (
                <AssignmentList items={todays} onOpen={setOpen} />
              )}
              {later.length > 0 ? (
                <>
                  <h2 className="text-sm font-semibold">{t('yardPlanner:phone.next')}</h2>
                  <AssignmentList items={later} onOpen={setOpen} />
                </>
              ) : null}
            </TabsContent>
            <TabsContent value="lift">
              <LiftDay marinaId={marinaId} />
            </TabsContent>
            {can('yard.manage') ? (
              <TabsContent value="approve">
                <Approvals marinaId={marinaId} />
              </TabsContent>
            ) : null}
          </Tabs>
        )}
      </div>
    </RequirePermission>
  );
}

function AssignmentList({
  items,
  onOpen,
}: {
  items: readonly MyYardAssignment[];
  onOpen: (a: MyYardAssignment) => void;
}) {
  const { t } = useT();
  return (
    <ul className="flex flex-col gap-2">
      {items.map((a) => (
        <li key={a.assignment_id}>
          <button
            type="button"
            data-testid="yard-assignment"
            className="hover:bg-muted flex min-h-11 w-full flex-col items-start gap-1 rounded-md border p-3 text-left"
            onClick={() => onOpen(a)}
          >
            <span className="flex w-full items-center justify-between gap-2">
              <span className="font-medium">{a.task_title}</span>
              <Badge variant="secondary">
                {t('yardPlanner:hoursShort', { hours: hoursLabel(a.hours_milli) })}
              </Badge>
            </span>
            <span className="text-muted-foreground text-sm">
              {a.work_order_number}
              {a.vessel_name ? ` · ${a.vessel_name}` : ''}
              {a.position ? ` · ${a.position}` : ''} · {formatIsoDate(a.work_date)}
            </span>
            <span className="text-xs">
              {t('yardPlanner:phone.progress', { value: Math.floor(a.progress_bp / 100) })}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

function TaskDetail({
  marinaId,
  assignment: a,
  timer,
  dispatch,
  today,
  onBack,
}: {
  marinaId: string;
  assignment: MyYardAssignment;
  timer: TimerState;
  dispatch: (x: Parameters<typeof timerReducer>[1]) => void;
  today: string;
  onBack: () => void;
}) {
  const { t } = useT();
  const start = useStartTimer(marinaId);
  const stop = useStopTimer(marinaId);
  const manual = useCreateTimeEntry(marinaId);
  const progress = useUpdateTaskProgress(marinaId);
  const photo = useUploadTaskPhoto(marinaId);
  const [hours, setHours] = useState(0);
  const [pct, setPct] = useState(Math.floor(a.progress_bp / 100));
  const [checks, setChecks] = useState<Record<string, boolean>>(
    Object.fromEntries((a.checklist ?? []).map((c) => [c.key, c.done])),
  );
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(id);
  }, []);
  const runningHere = timer.status === 'running' && timer.taskId === a.task_id;
  const runningElsewhere = timer.status === 'running' && timer.taskId !== a.task_id;
  const queued = () => toast.success(t('yardPlanner:phone.saved'));
  const onStart = () => {
    const startedAt = new Date().toISOString();
    start.mutate(
      { taskId: a.task_id, title: a.task_title, startedAt },
      {
        onSuccess: (op) => {
          dispatch({
            type: 'started',
            taskId: a.task_id,
            title: a.task_title,
            startedAt,
            startOp: op,
          });
          queued();
        },
      },
    );
  };
  const onStop = () => {
    if (timer.status !== 'running') return;
    stop.mutate(
      {
        timesheetId: timer.timesheetId,
        startOp: timer.timesheetId ? undefined : timer.startOp,
        title: timer.title,
        endedAt: new Date().toISOString(),
      },
      {
        onSuccess: () => {
          dispatch({ type: 'stopped' });
          queued();
        },
      },
    );
  };
  return (
    <div className="flex flex-col gap-4" data-testid="yard-task">
      <Button variant="ghost" className="min-h-11 self-start" onClick={onBack}>
        <ArrowLeftIcon />
        {t('yardPlanner:phone.back')}
      </Button>
      <div>
        <h1 className="text-xl font-semibold">{a.task_title}</h1>
        <p className="text-muted-foreground text-sm">
          {a.work_order_number}
          {a.vessel_name ? ` · ${a.vessel_name}` : ''}
        </p>
      </div>

      <section
        className="flex flex-col gap-2 rounded-md border p-3"
        aria-label={t('yardPlanner:phone.timer')}
      >
        <h2 className="text-sm font-semibold">{t('yardPlanner:phone.timer')}</h2>
        {runningHere && timer.status === 'running' ? (
          <>
            <p className="text-2xl font-semibold" data-testid="timer-elapsed">
              {durationLabel(elapsedMinutes(timer.startedAt, now))}
            </p>
            <Button
              className="min-h-11"
              variant="destructive"
              onClick={onStop}
              disabled={stop.isPending}
            >
              <SquareIcon />
              {t('yardPlanner:phone.stop')}
            </Button>
          </>
        ) : (
          <Button
            className="min-h-11"
            onClick={onStart}
            disabled={runningElsewhere || start.isPending}
          >
            <PlayIcon />
            {t('yardPlanner:phone.start')}
          </Button>
        )}
        {runningElsewhere && timer.status === 'running' ? (
          <p className="text-muted-foreground text-xs">
            {t('yardPlanner:phone.runningOn', { title: timer.title })}
          </p>
        ) : null}
        <ProblemAlert error={start.error ?? stop.error} />
      </section>

      <section
        className="flex flex-col gap-2 rounded-md border p-3"
        aria-label={t('yardPlanner:phone.manual')}
      >
        <h2 className="text-sm font-semibold">{t('yardPlanner:phone.manual')}</h2>
        <p className="text-2xl font-semibold" data-testid="manual-hours">
          {t('yardPlanner:hoursShort', { hours: hoursLabel(hours) })}
        </p>
        <div className="grid grid-cols-4 gap-2">
          {[250, 500, 1000, 4000].map((s) => (
            <Button
              key={s}
              variant="outline"
              className="min-h-11"
              onClick={() => setHours((h) => addHours(h, s))}
            >
              +{hoursLabel(s)}
            </Button>
          ))}
        </div>
        <div className="grid grid-cols-2 gap-2">
          <Button variant="ghost" className="min-h-11" onClick={() => setHours(0)}>
            {t('yardPlanner:phone.reset')}
          </Button>
          <Button
            className="min-h-11"
            disabled={hours < 250 || manual.isPending}
            onClick={() =>
              manual.mutate(
                { taskId: a.task_id, title: a.task_title, workDate: today, hoursMilli: hours },
                {
                  onSuccess: () => {
                    setHours(0);
                    queued();
                  },
                },
              )
            }
          >
            {t('yardPlanner:phone.saveHours')}
          </Button>
        </div>
        <ProblemAlert error={manual.error} />
      </section>

      <section
        className="flex flex-col gap-3 rounded-md border p-3"
        aria-label={t('yardPlanner:phone.work')}
      >
        <h2 className="text-sm font-semibold">{t('yardPlanner:phone.work')}</h2>
        {(a.checklist ?? []).map((c) => (
          <div key={c.key} className="flex min-h-11 items-center gap-3">
            <Checkbox
              id={`yp-chk-${c.key}`}
              checked={!!checks[c.key]}
              onCheckedChange={(v) => setChecks((x) => ({ ...x, [c.key]: v === true }))}
            />
            <Label htmlFor={`yp-chk-${c.key}`}>{c.label}</Label>
          </div>
        ))}
        <Field>
          <FieldLabel htmlFor="yp-progress">
            {t('yardPlanner:phone.progress', { value: pct })}
          </FieldLabel>
          <Slider
            id="yp-progress"
            min={0}
            max={100}
            step={5}
            value={[pct]}
            onValueChange={(v) => setPct(v[0] ?? 0)}
            aria-label={t('yardPlanner:phone.progressLabel')}
          />
        </Field>
        <Button
          className="min-h-11"
          disabled={progress.isPending}
          onClick={() =>
            progress.mutate(
              {
                taskId: a.task_id,
                title: a.task_title,
                progressBp: pct * 100,
                checklist: Object.entries(checks).map(([key, done]) => ({ key, done })),
                recordedAt: new Date().toISOString(),
              },
              { onSuccess: queued },
            )
          }
        >
          {t('yardPlanner:phone.saveProgress')}
        </Button>
        <Field>
          <FieldLabel htmlFor="yp-photo" className="flex items-center gap-2">
            <CameraIcon className="size-4" />
            {t('yardPlanner:phone.photo')}
          </FieldLabel>
          <input
            id="yp-photo"
            type="file"
            accept="image/*"
            capture="environment"
            className="text-sm"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f)
                photo.mutate(
                  { taskId: a.task_id, title: a.task_title, photo: f },
                  { onSuccess: queued },
                );
              e.target.value = '';
            }}
          />
        </Field>
        <ProblemAlert error={progress.error ?? photo.error} />
      </section>
    </div>
  );
}

function LiftDay({ marinaId }: { marinaId: string }) {
  const { t } = useT();
  const day = useEquipmentBookings(marinaId);
  return (
    <div className="flex flex-col gap-3" data-testid="yard-lift-day">
      <h1 className="text-xl font-semibold">{t('yardPlanner:phone.liftTitle')}</h1>
      {day.data ? <DayGrid day={day.data} /> : <LoadingState rows={2} />}
    </div>
  );
}

function Approvals({ marinaId }: { marinaId: string }) {
  const { t } = useT();
  const list = useSubmittedTimesheets(marinaId, true);
  const approve = useApproveTimesheets(marinaId);
  const [picked, setPicked] = useState<Record<string, boolean>>({});
  const items = list.data?.items ?? [];
  const chosen = items.filter((x) => picked[x.id]);
  return (
    <div className="flex flex-col gap-3" data-testid="yard-approvals">
      <h1 className="text-xl font-semibold">{t('yardPlanner:phone.approveTitle')}</h1>
      {items.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('yardPlanner:phone.nothingToApprove')}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {items.map((x) => (
            <li key={x.id} className="flex min-h-11 items-center gap-3 rounded-md border p-2">
              <Checkbox
                id={`yp-ap-${x.id}`}
                checked={!!picked[x.id]}
                onCheckedChange={(v) => setPicked((p) => ({ ...p, [x.id]: v === true }))}
              />
              <Label htmlFor={`yp-ap-${x.id}`} className="flex flex-col items-start">
                <span>
                  {t('yardPlanner:phone.entry', {
                    order: x.work_order_number ?? '',
                    hours: hoursLabel(x.hours_milli),
                  })}
                </span>
                <span className="text-muted-foreground text-xs">{formatIsoDate(x.work_date)}</span>
              </Label>
            </li>
          ))}
        </ul>
      )}
      <ProblemAlert error={approve.error} />
      <Button
        className="min-h-11"
        disabled={chosen.length === 0 || approve.isPending}
        onClick={() =>
          approve.mutate(chosen, {
            onSuccess: () => {
              setPicked({});
              toast.success(t('yardPlanner:phone.approved', { count: chosen.length }));
            },
          })
        }
      >
        {t('yardPlanner:phone.approve', { count: chosen.length })}
      </Button>
    </div>
  );
}
