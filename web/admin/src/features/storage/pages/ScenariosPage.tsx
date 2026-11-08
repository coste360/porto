import { useState } from 'react';
import { Link, getRouteApi, useNavigate } from '@tanstack/react-router';
import { FlaskConicalIcon, PlusIcon } from 'lucide-react';

import { useCreateStorageScenario, useStoragePlan, useStorageScenarios } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, EmptyState, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { addDays } from '../lib/plan';

const route = getRouteApi('/auth/m/$marinaId/storage/$areaId/scenarios');

/** Scenarios of an area (spec 17.3): name, author, window, status and the stale badge; new scenario dialog. */
export function ScenariosPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { areaId } = route.useParams();
  const plan = useStoragePlan(marinaId, areaId);
  const list = useStorageScenarios(marinaId, areaId);
  const items = list.data?.items ?? [];
  const area = plan.data?.area;
  return (
    <RequirePermission allowed={can('storage.read')}>
      <PageHeader
        title={t('storage:scenario.listTitle', { area: area?.name || area?.code || '' })}
        description={t('storage:scenario.listDescription')}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/m/$marinaId/storage/$areaId" params={{ marinaId, areaId }}>
                {t('storage:planner.open')}
              </Link>
            </Button>
            {can('storage.manage') ? <NewScenarioDialog areaId={areaId} /> : null}
          </>
        }
      />
      {list.isPending ? (
        <LoadingState />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState icon={<FlaskConicalIcon />} title={t('storage:scenario.empty')} />
      ) : (
        <Table data-testid="storage-scenarios">
          <TableHeader>
            <TableRow>
              <TableHead>{t('storage:scenario.name')}</TableHead>
              <TableHead>{t('storage:scenario.window')}</TableHead>
              <TableHead>{t('storage:scenario.status')}</TableHead>
              <TableHead className="text-right">{t('storage:indicators.exitsBlocked')}</TableHead>
              <TableHead className="text-right">{t('storage:indicators.moves')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((s) => (
              <TableRow key={s.id} data-scenario={s.id}>
                <TableCell>
                  <Link
                    to="/m/$marinaId/storage/$areaId/scenarios/$scenarioId"
                    params={{ marinaId, areaId, scenarioId: s.id }}
                    className="font-medium underline-offset-4 hover:underline"
                  >
                    {s.name}
                  </Link>
                  {s.stale && s.status === 'draft' ? (
                    <Badge variant="destructive" className="ml-2">
                      {t('storage:scenario.stale')}
                    </Badge>
                  ) : null}
                </TableCell>
                <TableCell>
                  {formatIsoDate(s.from_on)} – {formatIsoDate(s.to_on)}
                </TableCell>
                <TableCell>
                  <Badge variant={s.status === 'draft' ? 'outline' : 'secondary'}>
                    {t(`storage:scenarioStatus.${s.status}`)}
                  </Badge>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {s.indicators?.exits_blocked ?? '—'}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {s.indicators?.moves ?? '—'}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </RequirePermission>
  );
}

function NewScenarioDialog({ areaId }: { areaId: string }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const navigate = useNavigate();
  const create = useCreateStorageScenario(marinaId);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [from, setFrom] = useState<string | null>(today);
  const [to, setTo] = useState<string | null>(addDays(today, 180));
  const valid = name.trim().length > 0 && name.length <= 80 && !!from && !!to && from <= to;
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <PlusIcon />
          {t('storage:scenario.new')}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('storage:scenario.new')}</DialogTitle>
          <DialogDescription>{t('storage:scenario.newDescription')}</DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!valid) return;
            create.mutate(
              { area_id: areaId, name: name.trim(), from_on: from!, to_on: to! },
              {
                onSuccess: (d) => {
                  setOpen(false);
                  void navigate({
                    to: '/m/$marinaId/storage/$areaId/scenarios/$scenarioId',
                    params: { marinaId, areaId, scenarioId: d.scenario.id },
                  });
                },
              },
            );
          }}
        >
          <Field>
            <FieldLabel htmlFor="scenario-name">{t('storage:scenario.name')}</FieldLabel>
            <Input
              id="scenario-name"
              value={name}
              maxLength={80}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="scenario-from">{t('storage:scenario.from')}</FieldLabel>
              <DatePicker id="scenario-from" value={from} onChange={setFrom} />
            </Field>
            <Field>
              <FieldLabel htmlFor="scenario-to">{t('storage:scenario.to')}</FieldLabel>
              <DatePicker id="scenario-to" value={to} onChange={setTo} />
            </Field>
          </div>
          <ProblemAlert error={create.error} />
          <DialogFooter>
            <Button type="submit" disabled={!valid || create.isPending}>
              {t('storage:scenario.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
