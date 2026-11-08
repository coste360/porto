import { Link } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateMaintenanceTask,
  useMaintenanceTasks,
  type MaintenanceElementType,
  type MaintenancePriority,
  type MaintenanceTaskStatus,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, IsoDate, M4ProblemAlert, PageFrame } from '../../contracts-m4/components/kit';
import {
  ElementLabel,
  ElementPicker,
  MaintenanceNav,
  PriorityBadge,
  TaskStatusBadge,
} from '../components/kit';
import { sortTasks } from '../lib';

const TASK_STATUSES: readonly MaintenanceTaskStatus[] = [
  'open',
  'scheduled',
  'in_progress',
  'done',
  'cancelled',
];
const PRIORITIES: readonly MaintenancePriority[] = ['low', 'normal', 'high', 'urgent'];

/** Maintenance tasks board (m9-plan §12.2): filters by status and priority, overdue first, new office task. */
export function MaintenancePage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<MaintenanceTaskStatus | ''>('');
  const [priority, setPriority] = useState<MaintenancePriority | ''>('');
  const [overdue, setOverdue] = useState(false);
  const [creating, setCreating] = useState(false);
  const list = useMaintenanceTasks(marinaId, {
    ...(status ? { status } : {}),
    ...(priority ? { priority } : {}),
    ...(overdue ? { overdue_only: true } : {}),
  });
  const items = sortTasks(list.data?.items ?? []);
  return (
    <RequirePermission allowed={can('maintenance.manage')}>
      <PageFrame>
        <PageHeader
          title={t('maintenance:tasks.title')}
          description={t('maintenance:tasks.description')}
          actions={
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              {t('maintenance:tasks.new')}
            </Button>
          }
        />
        <MaintenanceNav marinaId={marinaId} current="tasks" />
        <div className="flex flex-wrap items-end gap-4">
          <EnumSelect
            id="task-status"
            label={t('maintenance:fields.status')}
            values={TASK_STATUSES}
            value={status}
            onChange={setStatus}
            labelOf={(s) => t(`maintenance:enums.status.${s}`)}
            allLabel={t('maintenance:all')}
          />
          <EnumSelect
            id="task-priority"
            label={t('maintenance:fields.priority')}
            values={PRIORITIES}
            value={priority}
            onChange={setPriority}
            labelOf={(s) => t(`maintenance:enums.priority.${s}`)}
            allLabel={t('maintenance:all')}
          />
          <div className="flex items-center gap-2 pb-2">
            <Switch id="task-overdue" checked={overdue} onCheckedChange={setOverdue} />
            <Label htmlFor="task-overdue">{t('maintenance:tasks.overdueOnly')}</Label>
          </div>
        </div>
        {list.isPending ? (
          <LoadingState rows={5} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('maintenance:fields.title'),
              t('maintenance:fields.element'),
              t('maintenance:fields.priority'),
              t('maintenance:fields.status'),
              t('maintenance:fields.dueOn'),
              t('maintenance:fields.source'),
            ]}
            empty={t('maintenance:tasks.empty')}
            rows={items.length}
          >
            {items.map((task) => (
              <TableRow key={task.id} data-testid="maintenance-task-row">
                <TableCell>
                  <Link
                    to="/m/$marinaId/maintenance/tasks/$taskId"
                    params={{ marinaId, taskId: task.id }}
                    className="font-medium underline-offset-4 hover:underline"
                  >
                    {task.title}
                  </Link>
                </TableCell>
                <TableCell>
                  <ElementLabel task={task} />
                </TableCell>
                <TableCell>
                  <PriorityBadge priority={task.priority} />
                </TableCell>
                <TableCell>
                  <TaskStatusBadge status={task.status} />
                </TableCell>
                <TableCell className="flex items-center gap-2">
                  <IsoDate date={task.due_on} />
                  {task.overdue ? (
                    <Badge variant="destructive">{t('maintenance:tasks.overdue')}</Badge>
                  ) : null}
                </TableCell>
                <TableCell>{t(`maintenance:enums.source.${task.source}`)}</TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        {creating ? (
          <TaskCreateDialog marinaId={marinaId} onClose={() => setCreating(false)} />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

function TaskCreateDialog({ marinaId, onClose }: { marinaId: string; onClose: () => void }) {
  const { t } = useT();
  const create = useCreateMaintenanceTask(marinaId);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [priority, setPriority] = useState<MaintenancePriority | ''>('normal');
  const [due, setDue] = useState<string | null>(null);
  const [oos, setOos] = useState(false);
  const [el, setEl] = useState<{ mapId: string; type: MaintenanceElementType; id: string | null }>({
    mapId: '',
    type: 'berth',
    id: null,
  });
  const submit = () =>
    create.mutate(
      {
        title: title.trim(),
        ...(description.trim() ? { description: description.trim() } : {}),
        priority: priority || 'normal',
        ...(due ? { due_on: due } : {}),
        out_of_service: oos,
        ...(el.id
          ? { element: { element_type: el.type, element_id: el.id } }
          : { map_id: el.mapId }),
      },
      { onSuccess: onClose },
    );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('maintenance:tasks.new')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <Field>
            <FieldLabel htmlFor="task-title">{t('maintenance:fields.title')}</FieldLabel>
            <Input
              id="task-title"
              value={title}
              maxLength={200}
              onChange={(e) => setTitle(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="task-desc">{t('maintenance:fields.description')}</FieldLabel>
            <Textarea
              id="task-desc"
              value={description}
              maxLength={2000}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
          <ElementPicker marinaId={marinaId} idPrefix="task-el" value={el} onChange={setEl} />
          <div className="grid gap-3 sm:grid-cols-2">
            <EnumSelect
              id="task-new-priority"
              label={t('maintenance:fields.priority')}
              values={PRIORITIES}
              value={priority}
              onChange={setPriority}
              labelOf={(s) => t(`maintenance:enums.priority.${s}`)}
            />
            <Field>
              <FieldLabel htmlFor="task-due">{t('maintenance:fields.dueOn')}</FieldLabel>
              <DatePicker
                id="task-due"
                value={due}
                onChange={setDue}
                placeholder={t('maintenance:none')}
              />
            </Field>
          </div>
          <div className="flex items-center gap-2">
            <Switch id="task-oos" checked={oos} onCheckedChange={setOos} />
            <Label htmlFor="task-oos">{t('maintenance:fields.outOfService')}</Label>
          </div>
          <M4ProblemAlert error={create.error} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!title.trim() || create.isPending || (!el.id && !el.mapId)}
            onClick={submit}
          >
            {t('maintenance:tasks.create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
