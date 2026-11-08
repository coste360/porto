import { useEffect, useReducer, useState } from 'react';
import { Link, getRouteApi, useNavigate } from '@tanstack/react-router';
import {
  CheckCheckIcon,
  GitCompareIcon,
  SaveIcon,
  SparklesIcon,
  TrashIcon,
  Undo2Icon,
} from 'lucide-react';
import { toast } from 'sonner';

import {
  useAddStorageScenarioItem,
  useApplyStorageScenario,
  useDiscardStorageScenario,
  useEvaluateStorageLayout,
  useOptimizeStorageScenario,
  useRemoveStorageScenarioItem,
  useSaveStorageScenarioItems,
  useStoragePlan,
  useStorageScenario,
  type StorageEvaluation,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@porto/shared/ui/collapsible';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ConfirmDialog, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { IndicatorsCard } from '../components/IndicatorsCard';
import { PlannerWorkspace } from '../components/PlannerWorkspace';
import { TrayCandidates } from '../components/TrayCandidates';
import { EMPTY_DRAFT, draftReducer, isDirty, toLayoutInputs } from '../lib/draft';
import { conflictsOf } from '../lib/conflicts';
import { gridOf, scenarioDraftItems } from '../lib/plan';

const route = getRouteApi('/auth/m/$marinaId/storage/$areaId/scenarios/$scenarioId');

/**
 * Scenario editor (spec 17.3): the planner on the working copy. Saving writes the copy only (INV-14); Ottimizza
 * runs the optimizer with pinned boats kept; Applica moves everything in one transaction or nothing, and a
 * conflict list explains why.
 */
export function ScenarioEditorPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { areaId, scenarioId } = route.useParams();
  const search = route.useSearch();
  const navigate = useNavigate({ from: '/m/$marinaId/storage/$areaId/scenarios/$scenarioId' });
  const detail = useStorageScenario(marinaId, scenarioId);
  const sc = detail.data?.scenario;
  const plan = useStoragePlan(marinaId, areaId, sc?.from_on, sc?.to_on);
  const [draft, dispatch] = useReducer(draftReducer, EMPTY_DRAFT);
  const [lens, setLens] = useState(true);
  const [seed, setSeed] = useState('');
  const [lastEvaluation, setEvaluation] = useState<StorageEvaluation | null>(null);
  const saveItems = useSaveStorageScenarioItems(marinaId, scenarioId);
  const optimize = useOptimizeStorageScenario(marinaId, scenarioId);
  const apply = useApplyStorageScenario(marinaId, scenarioId);
  const discard = useDiscardStorageScenario(marinaId, scenarioId);
  const removeItem = useRemoveStorageScenarioItem(marinaId, scenarioId);
  const addItem = useAddStorageScenarioItem(marinaId, scenarioId);
  const evaluate = useEvaluateStorageLayout(marinaId, areaId);
  const d = detail.data;
  const area = plan.data?.area;

  useEffect(() => {
    if (!d || !area) return;
    dispatch({ type: 'load', grid: gridOf(area), items: scenarioDraftItems(d.items) });
  }, [d, area]);

  const dirty = isDirty(draft);
  // The last evaluation of the draft; a clean draft shows the server state.
  const evaluation = dirty ? lastEvaluation : null;
  const evaluateDraft = evaluate.mutate;
  useEffect(() => {
    if (!sc || !dirty) return;
    const h = setTimeout(() => {
      evaluateDraft(
        { from_on: sc.from_on, to_on: sc.to_on, items: toLayoutInputs(draft) },
        { onSuccess: setEvaluation },
      );
    }, 400);
    return () => clearTimeout(h);
  }, [draft, dirty, sc, evaluateDraft]);

  if (detail.isPending || plan.isPending) return <LoadingState />;
  if (detail.isError)
    return <QueryErrorState error={detail.error} onRetry={() => void detail.refetch()} />;
  if (plan.isError)
    return <QueryErrorState error={plan.error} onRetry={() => void plan.refetch()} />;
  const s = d!.scenario;
  const p = plan.data!;
  const editable = can('storage.manage') && s.status === 'draft';
  const date = search.date ?? s.from_on;
  const conflicts = conflictsOf(apply.error);
  const label = (id: string) => {
    const it = d!.items.find((i) => i.allocation_id === id);
    return it?.vessel?.name ?? it?.contract?.code ?? id.slice(0, 8);
  };
  const seedValue = /^\d+$/.test(seed) ? Number(seed) : undefined;

  return (
    <RequirePermission allowed={can('storage.read')}>
      <PageHeader
        title={s.name}
        description={t('storage:scenario.header', {
          area: p.area.name || p.area.code,
          from: formatIsoDate(s.from_on),
          to: formatIsoDate(s.to_on),
        })}
        actions={
          <>
            <Badge variant={s.status === 'draft' ? 'outline' : 'secondary'}>
              {t(`storage:scenarioStatus.${s.status}`)}
            </Badge>
            {s.stale && s.status === 'draft' ? (
              <Badge variant="destructive">{t('storage:scenario.stale')}</Badge>
            ) : null}
            <Button variant="outline" asChild>
              <Link
                to="/m/$marinaId/storage/$areaId/scenarios/$scenarioId/compare"
                params={{ marinaId, areaId, scenarioId }}
                search={{ date }}
              >
                <GitCompareIcon />
                {t('storage:scenario.compare')}
              </Link>
            </Button>
            {editable ? (
              <>
                <Button
                  variant="outline"
                  disabled={optimize.isPending || dirty}
                  onClick={() =>
                    optimize.mutate({
                      body: seedValue !== undefined ? { seed: seedValue } : {},
                      version: s.version,
                    })
                  }
                  data-testid="scenario-optimize"
                >
                  <SparklesIcon />
                  {t('storage:scenario.optimize')}
                </Button>
                <Button
                  variant="outline"
                  disabled={!dirty}
                  onClick={() => dispatch({ type: 'restore' })}
                >
                  <Undo2Icon />
                  {t('storage:planner.restore')}
                </Button>
                <Button
                  variant="outline"
                  disabled={!dirty || saveItems.isPending}
                  onClick={() =>
                    saveItems.mutate(
                      { items: toLayoutInputs(draft), version: s.version },
                      { onSuccess: () => toast.success(t('storage:scenario.saved')) },
                    )
                  }
                  data-testid="scenario-save"
                >
                  <SaveIcon />
                  {t('storage:scenario.save')}
                </Button>
                <ConfirmDialog
                  trigger={
                    <Button disabled={dirty || apply.isPending} data-testid="scenario-apply">
                      <CheckCheckIcon />
                      {t('storage:scenario.apply')}
                    </Button>
                  }
                  title={t('storage:scenario.applyTitle')}
                  description={t('storage:scenario.applyDescription', { count: d!.moves.length })}
                  confirmLabel={t('storage:scenario.apply')}
                  cancelLabel={t('common:actions.cancel')}
                  onConfirm={() =>
                    apply.mutate(
                      { version: s.version },
                      {
                        onSuccess: (r) => {
                          toast.success(
                            t('storage:scenario.applied', { count: r.movements.length }),
                          );
                          void navigate({
                            to: '/m/$marinaId/storage/movements',
                            params: { marinaId },
                            search: {},
                          });
                        },
                      },
                    )
                  }
                />
                <ConfirmDialog
                  trigger={
                    <Button variant="ghost" aria-label={t('storage:scenario.discard')}>
                      <TrashIcon />
                    </Button>
                  }
                  title={t('storage:scenario.discardTitle')}
                  confirmLabel={t('storage:scenario.discard')}
                  cancelLabel={t('common:actions.cancel')}
                  destructive
                  onConfirm={() =>
                    discard.mutate(s.version, {
                      onSuccess: () =>
                        void navigate({
                          to: '/m/$marinaId/storage/$areaId/scenarios',
                          params: { marinaId, areaId },
                        }),
                    })
                  }
                />
              </>
            ) : null}
          </>
        }
      />
      {conflicts.length > 0 ? (
        <Alert variant="destructive" className="mb-4" data-testid="scenario-conflicts">
          <AlertTitle>
            {t('storage:scenario.conflictsTitle', { count: conflicts.length })}
          </AlertTitle>
          <AlertDescription>
            <ul className="list-disc pl-4">
              {conflicts.map((c) => (
                <li key={`${c.allocation_id}-${c.reason}`}>
                  <button
                    type="button"
                    className="underline-offset-4 hover:underline"
                    onClick={() => dispatch({ type: 'select', id: c.allocation_id })}
                  >
                    {label(c.allocation_id)}
                  </button>
                  {': '}
                  {t(`storage:conflict.${c.reason}`)}
                  {c.with_allocation_ids?.length
                    ? ` (${c.with_allocation_ids.map(label).join(', ')})`
                    : ''}
                </li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      ) : (
        <ProblemAlert error={apply.error} className="mb-4" />
      )}
      <ProblemAlert
        error={
          saveItems.error ?? optimize.error ?? discard.error ?? removeItem.error ?? addItem.error
        }
        className="mb-4"
      />
      <div className="grid gap-4">
        <PlannerWorkspace
          idPrefix="scenario"
          from={s.from_on}
          to={s.to_on}
          date={date}
          onDate={(x) => void navigate({ search: { date: x }, replace: true })}
          grid={draft.grid}
          draft={draft}
          dispatch={dispatch}
          meta={d!.items}
          exits={p.gates.flatMap((g) => g.cells)}
          obstacles={p.obstacles}
          access={evaluation?.access ?? d!.access}
          issues={evaluation?.issues ?? d!.issues}
          lens={lens}
          onLens={setLens}
          readOnly={!editable}
          pinnable
        />
        <div className="grid gap-4 md:grid-cols-2">
          <IndicatorsCard ind={evaluation?.indicators ?? s.indicators} />
          <div className="flex flex-col gap-3">
            {editable ? (
              <Collapsible>
                <CollapsibleTrigger asChild>
                  <Button variant="ghost" size="sm">
                    {t('storage:scenario.advanced')}
                  </Button>
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <Field className="max-w-60">
                    <FieldLabel htmlFor="scenario-seed">{t('storage:scenario.seed')}</FieldLabel>
                    <Input
                      id="scenario-seed"
                      inputMode="numeric"
                      value={seed}
                      placeholder={String(s.seed)}
                      onChange={(e) => setSeed(e.target.value.replace(/\D/g, ''))}
                    />
                  </Field>
                  {d!.run ? (
                    <p className="text-muted-foreground mt-2 text-xs">
                      {t('storage:scenario.run', {
                        seed: d!.run.seed,
                        iterations: d!.run.iterations,
                        stopped: t(`storage:stoppedBy.${d!.run.stopped_by}`),
                      })}
                    </p>
                  ) : null}
                </CollapsibleContent>
              </Collapsible>
            ) : null}
            {editable ? (
              <TrayCandidates
                marinaId={marinaId}
                scenarioId={scenarioId}
                disabled={dirty}
                pending={addItem.isPending}
                onAdd={(allocationId) => addItem.mutate({ allocationId, version: s.version })}
              />
            ) : null}
            {editable && d!.items.some((i) => i.origin === 'tray') ? (
              <div className="flex flex-col gap-1 text-sm">
                <p className="font-medium">{t('storage:scenario.trayItems')}</p>
                {d!.items
                  .filter((i) => i.origin === 'tray')
                  .map((i) => (
                    <div key={i.allocation_id} className="flex items-center justify-between gap-2">
                      <span>{label(i.allocation_id)}</span>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() =>
                          removeItem.mutate({ allocationId: i.allocation_id, version: s.version })
                        }
                      >
                        {t('storage:scenario.removeTray')}
                      </Button>
                    </div>
                  ))}
              </div>
            ) : null}
            <p className="text-sm font-medium">
              {t('storage:scenario.moves', { count: d!.moves.length })}
            </p>
            <ol
              className="text-muted-foreground list-decimal pl-5 text-sm"
              data-testid="scenario-moves"
            >
              {d!.moves.map((m) => (
                <li key={m.seq}>
                  {t(
                    m.kind === 'place' ? 'storage:scenario.movePlace' : 'storage:scenario.moveMove',
                    {
                      name: label(m.allocation_id),
                      date: formatIsoDate(m.move_on),
                    },
                  )}
                </li>
              ))}
            </ol>
          </div>
        </div>
      </div>
    </RequirePermission>
  );
}
