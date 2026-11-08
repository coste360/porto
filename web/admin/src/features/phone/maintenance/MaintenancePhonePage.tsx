import { ArrowLeftIcon, CameraIcon, MegaphoneIcon, WrenchIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useCompleteMaintenanceTask,
  useMaintenanceWorklist,
  useReportMaintenance,
  type MaintenanceElementType,
  type MaintenanceOutcome,
  type MaintenancePriority,
  type MaintenanceTask,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { isQueuedOffline, useQueue } from '@porto/shared/offline';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { EmptyState } from '@porto/shared/ui/patterns';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { canUseMaintenancePhone } from '../../../lib/landing';
import { EnumSelect } from '../../contracts-m4/components/kit';
import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';
import { ElementLabel, ElementPicker, PriorityBadge } from '../../maintenance/components/kit';

const OUTCOMES: readonly MaintenanceOutcome[] = ['ok', 'fixed', 'needs_followup', 'not_done'];
const PRIORITIES: readonly MaintenancePriority[] = ['low', 'normal', 'high', 'urgent'];

type View = { kind: 'list' } | { kind: 'task'; task: MaintenanceTask } | { kind: 'report' };

/** Maintenance phone screen (m9-plan §12.2): the worklist from the offline cache, task detail with checklist and
 * outcome, and "Segnala" with a camera photo; outcomes and reports go through the offline queue. */
export function MaintenancePhonePage() {
  const { t } = useT();
  const { marina, marinaId } = useCurrentMarina();
  const worklist = useMaintenanceWorklist(marinaId);
  const queue = useQueue(marinaId);
  const [view, setView] = useState<View>({ kind: 'list' });
  const back = () => setView({ kind: 'list' });
  return (
    <RequirePermission allowed={canUseMaintenancePhone(marina?.permissions ?? [])}>
      <div data-testid="maintenance-phone" className="flex flex-col gap-4 p-4">
        {view.kind === 'task' ? (
          <OutcomeForm task={view.task} onDone={back} />
        ) : view.kind === 'report' ? (
          <ReportForm onDone={back} />
        ) : (
          <>
            <div className="flex items-center justify-between gap-2">
              <h1 className="text-xl font-semibold">{t('maintenance:phone.title')}</h1>
              <Button className="min-h-11" onClick={() => setView({ kind: 'report' })}>
                <MegaphoneIcon />
                {t('maintenance:phone.report')}
              </Button>
            </div>
            {queue.pending > 0 ? (
              <p role="status" className="text-muted-foreground text-sm">
                {t('maintenance:phone.queued', { count: queue.pending })}
              </p>
            ) : null}
            {(worklist.data?.items.length ?? 0) === 0 ? (
              <EmptyState
                icon={<WrenchIcon />}
                title={t('maintenance:phone.empty')}
                description={t('maintenance:phone.description')}
              />
            ) : (
              <ul className="flex flex-col gap-2">
                {worklist.data!.items.map((task) => (
                  <li key={task.id}>
                    <button
                      type="button"
                      data-testid="worklist-task"
                      className="hover:bg-muted flex min-h-11 w-full flex-col items-start gap-1 rounded-md border p-3 text-left"
                      onClick={() => setView({ kind: 'task', task })}
                    >
                      <span className="flex w-full items-center justify-between gap-2">
                        <span className="font-medium">{task.title}</span>
                        <PriorityBadge priority={task.priority} />
                      </span>
                      <span className="text-muted-foreground flex gap-2 text-sm">
                        <ElementLabel task={task} />
                        {task.due_on ? <span>{formatIsoDate(task.due_on)}</span> : null}
                        {task.overdue ? (
                          <Badge variant="destructive">{t('maintenance:tasks.overdue')}</Badge>
                        ) : null}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </div>
    </RequirePermission>
  );
}

function OutcomeForm({ task, onDone }: { task: MaintenanceTask; onDone: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const complete = useCompleteMaintenanceTask(marinaId);
  const [outcome, setOutcome] = useState<MaintenanceOutcome | ''>('ok');
  const [done, setDone] = useState<Record<string, boolean>>({});
  const [minutes, setMinutes] = useState('');
  const [notes, setNotes] = useState('');
  const finish = () => {
    toast.success(t('maintenance:phone.outcomeSaved'));
    onDone();
  };
  return (
    <div className="flex flex-col gap-4" data-testid="outcome-form">
      <Button variant="ghost" className="min-h-11 self-start" onClick={onDone}>
        <ArrowLeftIcon />
        {t('maintenance:phone.back')}
      </Button>
      <h1 className="text-xl font-semibold">{task.title}</h1>
      <p className="text-muted-foreground text-sm">
        <ElementLabel task={task} />
      </p>
      {task.description ? <p className="text-sm">{task.description}</p> : null}
      {task.checklist.map((c) => (
        <div key={c.key} className="flex min-h-11 items-center gap-3">
          <Checkbox
            id={`ph-chk-${c.key}`}
            checked={!!done[c.key]}
            onCheckedChange={(v) => setDone((d) => ({ ...d, [c.key]: v === true }))}
          />
          <Label htmlFor={`ph-chk-${c.key}`}>{c.label}</Label>
        </div>
      ))}
      <EnumSelect
        id="ph-outcome"
        label={t('maintenance:fields.outcome')}
        values={OUTCOMES}
        value={outcome}
        onChange={setOutcome}
        labelOf={(o) => t(`maintenance:enums.outcome.${o}`)}
      />
      <Field>
        <FieldLabel htmlFor="ph-minutes">{t('maintenance:fields.minutes')}</FieldLabel>
        <Input
          id="ph-minutes"
          inputMode="numeric"
          value={minutes}
          onChange={(e) => setMinutes(e.target.value.replace(/\D/g, ''))}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="ph-notes">{t('maintenance:fields.notes')}</FieldLabel>
        <Textarea
          id="ph-notes"
          value={notes}
          maxLength={2000}
          onChange={(e) => setNotes(e.target.value)}
        />
      </Field>
      {complete.error && !isQueuedOffline(complete.error) ? (
        <DeskProblemAlert error={complete.error} />
      ) : null}
      <Button
        className="min-h-11"
        disabled={!outcome || complete.isPending}
        onClick={() =>
          complete.mutate(
            {
              id: task.id,
              title: task.title,
              body: {
                outcome: outcome || 'ok',
                checklist_results: task.checklist.map((c) => ({ key: c.key, done: !!done[c.key] })),
                ...(minutes ? { internal_minutes: Number(minutes) } : {}),
                ...(notes.trim() ? { notes: notes.trim() } : {}),
              },
            },
            {
              onSuccess: finish,
              onError: (e) => {
                if (isQueuedOffline(e)) finish();
              },
            },
          )
        }
      >
        {t('maintenance:task.complete')}
      </Button>
    </div>
  );
}

function ReportForm({ onDone }: { onDone: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const report = useReportMaintenance(marinaId);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [priority, setPriority] = useState<MaintenancePriority | ''>('normal');
  const [photo, setPhoto] = useState<File | null>(null);
  const [el, setEl] = useState<{ mapId: string; type: MaintenanceElementType; id: string | null }>({
    mapId: '',
    type: 'berth',
    id: null,
  });
  return (
    <div className="flex flex-col gap-4" data-testid="report-form">
      <Button variant="ghost" className="min-h-11 self-start" onClick={onDone}>
        <ArrowLeftIcon />
        {t('maintenance:phone.back')}
      </Button>
      <h1 className="text-xl font-semibold">{t('maintenance:phone.report')}</h1>
      <Field>
        <FieldLabel htmlFor="rep-title">{t('maintenance:fields.title')}</FieldLabel>
        <Input
          id="rep-title"
          value={title}
          maxLength={200}
          onChange={(e) => setTitle(e.target.value)}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="rep-desc">{t('maintenance:fields.description')}</FieldLabel>
        <Textarea
          id="rep-desc"
          value={description}
          maxLength={2000}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Field>
      <ElementPicker marinaId={marinaId} idPrefix="rep-el" value={el} onChange={setEl} />
      <EnumSelect
        id="rep-priority"
        label={t('maintenance:fields.priority')}
        values={PRIORITIES}
        value={priority}
        onChange={setPriority}
        labelOf={(p) => t(`maintenance:enums.priority.${p}`)}
      />
      <Field>
        <FieldLabel htmlFor="rep-photo">
          <CameraIcon className="size-4" /> {t('maintenance:phone.photo')}
        </FieldLabel>
        <Input
          id="rep-photo"
          type="file"
          accept="image/jpeg,image/png"
          capture="environment"
          onChange={(e) => setPhoto(e.target.files?.[0] ?? null)}
        />
      </Field>
      <DeskProblemAlert error={report.error} />
      <Button
        className="min-h-11"
        disabled={!title.trim() || (!el.id && !el.mapId) || report.isPending}
        onClick={() =>
          report.mutate(
            {
              body: {
                title: title.trim(),
                ...(description.trim() ? { description: description.trim() } : {}),
                priority: priority || 'normal',
                ...(el.id
                  ? { element: { element_type: el.type, element_id: el.id } }
                  : { map_id: el.mapId }),
              },
              ...(photo ? { photo } : {}),
            },
            {
              onSuccess: () => {
                toast.success(t('maintenance:phone.reportSaved'));
                onDone();
              },
            },
          )
        }
      >
        {t('maintenance:phone.send')}
      </Button>
    </div>
  );
}
