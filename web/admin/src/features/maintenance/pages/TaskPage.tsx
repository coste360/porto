import { getRouteApi } from '@tanstack/react-router';
import { CheckIcon, MinusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCancelMaintenanceTask,
  useCompleteMaintenanceTask,
  useMaintenanceFileLink,
  useMaintenanceTask,
  useReleaseMaintenanceTask,
  useScheduleMaintenanceTask,
  useStartMaintenanceTask,
  useUploadMaintenanceFile,
  type MaintenanceOutcome,
  type MaintenanceTask,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { isQueuedOffline } from '@porto/shared/offline';
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
import { DatePicker, MoneyInput, PageHeader } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import {
  Detail,
  DetailGrid,
  EnumSelect,
  IsoDate,
  M4ProblemAlert,
  Money,
  PageFrame,
  ReasonDialog,
  Section,
} from '../../contracts-m4/components/kit';
import {
  ConflictList,
  ElementLabel,
  MaintenanceNav,
  PriorityBadge,
  TaskStatusBadge,
} from '../components/kit';
import { conflictsOf } from '../lib';

const route = getRouteApi('/auth/m/$marinaId/maintenance/tasks/$taskId');

const OUTCOMES: readonly MaintenanceOutcome[] = ['ok', 'fixed', 'needs_followup', 'not_done'];

type Dialogs = 'schedule' | 'complete' | 'cancel' | 'release' | null;

/** Task detail (m9-plan §12.2): schedule nights (conflicts listed with alternatives), start, complete with the
 * checklist and costs, release and cancel (online), photos and documents. */
export function TaskPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { taskId } = route.useParams();
  const task = useMaintenanceTask(marinaId, taskId);
  const start = useStartMaintenanceTask(marinaId);
  const cancel = useCancelMaintenanceTask(marinaId);
  const upload = useUploadMaintenanceFile(marinaId);
  const link = useMaintenanceFileLink(marinaId);
  const [dialog, setDialog] = useState<Dialogs>(null);
  if (task.isPending) return <LoadingState rows={6} />;
  if (task.isError)
    return <QueryErrorState error={task.error} onRetry={() => void task.refetch()} />;
  const v = task.data;
  const live = v.status === 'open' || v.status === 'scheduled' || v.status === 'in_progress';
  return (
    <RequirePermission allowed={can('maintenance.manage')}>
      <PageFrame>
        <PageHeader
          title={v.title}
          description={v.description ?? undefined}
          actions={
            live ? (
              <>
                {v.status !== 'in_progress' ? (
                  <Button variant="outline" onClick={() => setDialog('schedule')}>
                    {t('maintenance:task.schedule')}
                  </Button>
                ) : null}
                {v.status !== 'in_progress' ? (
                  <Button
                    variant="outline"
                    disabled={start.isPending}
                    onClick={() => start.mutate({ id: v.id, version: v.version })}
                  >
                    {t('maintenance:task.start')}
                  </Button>
                ) : null}
                <Button onClick={() => setDialog('complete')}>
                  {t('maintenance:task.complete')}
                </Button>
                <Button variant="outline" onClick={() => setDialog('cancel')}>
                  {t('maintenance:task.cancel')}
                </Button>
              </>
            ) : v.allocation_ids.length > 0 && v.status === 'done' ? (
              <Button variant="outline" onClick={() => setDialog('release')}>
                {t('maintenance:task.release')}
              </Button>
            ) : null
          }
        />
        <MaintenanceNav marinaId={marinaId} current="tasks" />
        <M4ProblemAlert error={start.error} />
        <Section title={t('maintenance:task.details')}>
          <DetailGrid>
            <Detail label={t('maintenance:fields.element')}>
              <ElementLabel task={v} />
            </Detail>
            <Detail label={t('maintenance:fields.status')}>
              <TaskStatusBadge status={v.status} />
            </Detail>
            <Detail label={t('maintenance:fields.priority')}>
              <PriorityBadge priority={v.priority} />
            </Detail>
            <Detail label={t('maintenance:fields.dueOn')}>
              <IsoDate date={v.due_on} />
            </Detail>
            <Detail label={t('maintenance:fields.nights')}>
              {v.scheduled_from ? (
                <>
                  <IsoDate date={v.scheduled_from} /> – <IsoDate date={v.scheduled_to} />
                </>
              ) : (
                t('maintenance:none')
              )}
            </Detail>
            <Detail label={t('maintenance:fields.outOfService')}>
              {v.out_of_service ? t('maintenance:yes') : t('maintenance:no')}
            </Detail>
            <Detail label={t('maintenance:fields.source')}>
              {t(`maintenance:enums.source.${v.source}`)}
            </Detail>
            {v.intervention ? (
              <Detail label={t('maintenance:fields.intervention')}>
                {t(`maintenance:enums.intervention.${v.intervention}`)}
              </Detail>
            ) : null}
            {v.supplier_ref ? (
              <Detail label={t('maintenance:fields.supplierRef')}>{v.supplier_ref}</Detail>
            ) : null}
            {v.outcome ? (
              <Detail label={t('maintenance:fields.outcome')}>
                {t(`maintenance:enums.outcome.${v.outcome}`)}
              </Detail>
            ) : null}
            {v.internal_minutes !== undefined ? (
              <Detail label={t('maintenance:fields.minutes')}>{v.internal_minutes}</Detail>
            ) : null}
            {v.external_cost_cents !== undefined ? (
              <Detail label={t('maintenance:fields.externalCost')}>
                <Money cents={v.external_cost_cents} />
              </Detail>
            ) : null}
          </DetailGrid>
        </Section>
        {v.checklist.length > 0 ? (
          <Section title={t('maintenance:fields.checklist')}>
            <ul className="flex flex-col gap-1 text-sm">
              {v.checklist.map((c) => {
                const r = v.checklist_results.find((x) => x.key === c.key);
                return (
                  <li key={c.key} className="flex items-center gap-2">
                    {r?.done ? (
                      <CheckIcon className="size-4" aria-label={t('maintenance:task.checked')} />
                    ) : (
                      <MinusIcon className="size-4" aria-label={t('maintenance:task.unchecked')} />
                    )}
                    {c.label}
                    {r?.note ? <span className="text-muted-foreground"> — {r.note}</span> : null}
                  </li>
                );
              })}
            </ul>
          </Section>
        ) : null}
        <Section title={t('maintenance:task.files')}>
          <div className="flex flex-col gap-3">
            <ul className="flex flex-col gap-1 text-sm">
              {v.files.map((f) => (
                <li key={f.file_id} className="flex items-center gap-2">
                  <span>{t(`maintenance:enums.fileKind.${f.kind}`)}</span>
                  <Button
                    size="sm"
                    variant="link"
                    onClick={() =>
                      link.mutate(
                        { taskId: v.id, fileId: f.file_id },
                        { onSuccess: (l) => window.open(l.url, '_blank', 'noopener') },
                      )
                    }
                  >
                    {t('maintenance:task.openFile')}
                  </Button>
                </li>
              ))}
            </ul>
            <Field>
              <FieldLabel htmlFor="task-file">{t('maintenance:task.addFile')}</FieldLabel>
              <Input
                id="task-file"
                type="file"
                accept="image/jpeg,image/png,application/pdf"
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (file) upload.mutate({ taskId: v.id, title: v.title, file });
                  e.target.value = '';
                }}
              />
            </Field>
            {upload.error && !isQueuedOffline(upload.error) ? (
              <M4ProblemAlert error={upload.error} />
            ) : null}
            <M4ProblemAlert error={link.error} />
          </div>
        </Section>
        {dialog === 'schedule' ? (
          <ScheduleDialog marinaId={marinaId} task={v} onClose={() => setDialog(null)} />
        ) : null}
        {dialog === 'complete' ? (
          <CompleteDialog
            marinaId={marinaId}
            task={v}
            locale={i18n.language}
            onClose={() => setDialog(null)}
          />
        ) : null}
        {dialog === 'release' ? (
          <ReleaseDialog marinaId={marinaId} task={v} onClose={() => setDialog(null)} />
        ) : null}
        <ReasonDialog
          open={dialog === 'cancel'}
          title={t('maintenance:task.cancel')}
          description={t('maintenance:task.cancelHint')}
          confirmLabel={t('maintenance:task.cancel')}
          required
          destructive
          pending={cancel.isPending}
          error={cancel.error}
          onClose={() => setDialog(null)}
          onConfirm={(reason) =>
            cancel.mutate(
              { id: v.id, version: v.version, reason },
              { onSuccess: () => setDialog(null) },
            )
          }
        />
      </PageFrame>
    </RequirePermission>
  );
}

/** Nights to book; a refusal because vessels hold them lists each one with its alternatives. */
export function ScheduleDialog({
  marinaId,
  task,
  onClose,
}: {
  marinaId: string;
  task: MaintenanceTask;
  onClose: () => void;
}) {
  const { t } = useT();
  const schedule = useScheduleMaintenanceTask(marinaId);
  const [from, setFrom] = useState<string | null>(task.scheduled_from ?? task.due_on ?? null);
  const [to, setTo] = useState<string | null>(task.scheduled_to ?? task.due_on ?? null);
  const [oos, setOos] = useState(task.out_of_service);
  const conflicts = conflictsOf(schedule.error);
  const valid = !!from && !!to && from <= to;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('maintenance:task.schedule')}</DialogTitle>
          <DialogDescription>{t('maintenance:schedule.hint')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="sched-from">{t('maintenance:schedule.from')}</FieldLabel>
            <DatePicker id="sched-from" value={from} onChange={setFrom} />
          </Field>
          <Field>
            <FieldLabel htmlFor="sched-to">{t('maintenance:schedule.to')}</FieldLabel>
            <DatePicker id="sched-to" value={to} onChange={setTo} />
          </Field>
        </div>
        <div className="flex items-center gap-2">
          <Switch id="sched-oos" checked={oos} onCheckedChange={setOos} />
          <Label htmlFor="sched-oos">{t('maintenance:fields.outOfService')}</Label>
        </div>
        {conflicts ? (
          <ConflictList conflicts={conflicts} />
        ) : (
          <M4ProblemAlert error={schedule.error} />
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!valid || schedule.isPending}
            onClick={() =>
              schedule.mutate(
                {
                  id: task.id,
                  version: task.version,
                  body: { from_night: from!, to_night: to!, out_of_service: oos },
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('maintenance:task.schedule')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Outcome, checklist, minutes and costs (queued: works offline too). */
export function CompleteDialog({
  marinaId,
  task,
  locale,
  onClose,
}: {
  marinaId: string;
  task: MaintenanceTask;
  locale: string;
  onClose: () => void;
}) {
  const { t } = useT();
  const complete = useCompleteMaintenanceTask(marinaId);
  const [outcome, setOutcome] = useState<MaintenanceOutcome | ''>('ok');
  const [done, setDone] = useState<Record<string, boolean>>({});
  const [minutes, setMinutes] = useState('');
  const [cost, setCost] = useState<number | null>(null);
  const [notes, setNotes] = useState('');
  const queued = complete.error && isQueuedOffline(complete.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('maintenance:task.complete')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <EnumSelect
            id="complete-outcome"
            label={t('maintenance:fields.outcome')}
            values={OUTCOMES}
            value={outcome}
            onChange={setOutcome}
            labelOf={(o) => t(`maintenance:enums.outcome.${o}`)}
          />
          {task.checklist.map((c) => (
            <div key={c.key} className="flex items-center gap-2">
              <Checkbox
                id={`chk-${c.key}`}
                checked={!!done[c.key]}
                onCheckedChange={(v) => setDone((d) => ({ ...d, [c.key]: v === true }))}
              />
              <Label htmlFor={`chk-${c.key}`}>{c.label}</Label>
            </div>
          ))}
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="complete-minutes">{t('maintenance:fields.minutes')}</FieldLabel>
              <Input
                id="complete-minutes"
                inputMode="numeric"
                value={minutes}
                onChange={(e) => setMinutes(e.target.value.replace(/\D/g, ''))}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="complete-cost">
                {t('maintenance:fields.externalCost')}
              </FieldLabel>
              <MoneyInput id="complete-cost" value={cost} onChange={setCost} locale={locale} />
            </Field>
          </div>
          <Field>
            <FieldLabel htmlFor="complete-notes">{t('maintenance:fields.notes')}</FieldLabel>
            <Textarea
              id="complete-notes"
              value={notes}
              maxLength={2000}
              onChange={(e) => setNotes(e.target.value)}
            />
          </Field>
          {queued ? (
            <p className="text-muted-foreground text-sm" role="status">
              {t('maintenance:queued')}
            </p>
          ) : (
            <M4ProblemAlert error={complete.error} />
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!outcome || complete.isPending}
            onClick={() =>
              complete.mutate(
                {
                  id: task.id,
                  title: task.title,
                  body: {
                    outcome: outcome || 'ok',
                    checklist_results: task.checklist.map((c) => ({
                      key: c.key,
                      done: !!done[c.key],
                    })),
                    ...(minutes ? { internal_minutes: Number(minutes) } : {}),
                    ...(cost !== null ? { external_cost_cents: cost } : {}),
                    ...(notes.trim() ? { notes: notes.trim() } : {}),
                  },
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('maintenance:task.complete')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ReleaseDialog({
  marinaId,
  task,
  onClose,
}: {
  marinaId: string;
  task: MaintenanceTask;
  onClose: () => void;
}) {
  const { t } = useT();
  const release = useReleaseMaintenanceTask(marinaId);
  const [last, setLast] = useState<string | null>(null);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('maintenance:task.release')}</DialogTitle>
          <DialogDescription>{t('maintenance:task.releaseHint')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="release-last">{t('maintenance:task.keepUntil')}</FieldLabel>
          <DatePicker
            id="release-last"
            value={last}
            onChange={setLast}
            placeholder={t('maintenance:task.releaseAll')}
          />
        </Field>
        <M4ProblemAlert error={release.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={release.isPending}
            onClick={() =>
              release.mutate(
                { id: task.id, version: task.version, ...(last ? { lastNight: last } : {}) },
                { onSuccess: onClose },
              )
            }
          >
            {t('maintenance:task.release')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
