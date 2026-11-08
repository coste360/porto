import { useEffect, useReducer, useState } from 'react';
import { Link, getRouteApi, useNavigate } from '@tanstack/react-router';
import { FlaskConicalIcon, SaveIcon, SparklesIcon, Undo2Icon } from 'lucide-react';
import { toast } from 'sonner';

import {
  ifMatch,
  unwrap,
  useApi,
  useArrangeStorageArea,
  useCreateStorageScenario,
  useEvaluateStorageLayout,
  useSaveAreaLayout,
  useStoragePlan,
  type StorageEvaluation,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { IndicatorsCard } from '../components/IndicatorsCard';
import { PlannerWorkspace } from '../components/PlannerWorkspace';
import {
  EMPTY_DRAFT,
  asRotation,
  changedIds,
  draftReducer,
  isDirty,
  toLayoutInputs,
  type Position,
} from '../lib/draft';
import { gridOf, planDraftItems } from '../lib/plan';

const route = getRouteApi('/auth/m/$marinaId/storage/$areaId');

/**
 * Planner of a surface area (spec 17.2, m9-plan §12.1): the real state of the window as a layout session. Moves
 * stay in the draft (validated by the server, nothing saved) until Salva writes them through the M3 layout save;
 * Preset fills the draft with the optimizer proposal; "Apri come ipotesi" turns the draft into a scenario.
 */
export function StoragePlannerPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { areaId } = route.useParams();
  const search = route.useSearch();
  const navigate = useNavigate({ from: '/m/$marinaId/storage/$areaId' });
  const today = useMarinaToday();
  const plan = useStoragePlan(marinaId, areaId);
  const [draft, dispatch] = useReducer(draftReducer, EMPTY_DRAFT);
  const [lens, setLens] = useState(true);
  const [lastEvaluation, setEvaluation] = useState<StorageEvaluation | null>(null);
  const evaluate = useEvaluateStorageLayout(marinaId, areaId);
  const arrange = useArrangeStorageArea(marinaId, areaId);
  const save = useSaveAreaLayout(marinaId, areaId);
  const createScenario = useCreateStorageScenario(marinaId);
  const api = useApi();
  const manage = can('storage.manage');
  const data = plan.data;
  const date = search.date ?? (data && today < data.from ? data.from : today);

  useEffect(() => {
    if (!data) return;
    dispatch({ type: 'load', grid: gridOf(data.area), items: planDraftItems(data.items) });
  }, [data]);

  const dirty = isDirty(draft);
  // The last evaluation of the draft; a clean draft shows the server state.
  const evaluation = dirty ? lastEvaluation : null;
  const evaluateDraft = evaluate.mutate;
  useEffect(() => {
    if (!data || !dirty) return;
    const h = setTimeout(() => {
      evaluateDraft(
        { from_on: data.from, to_on: data.to, items: toLayoutInputs(draft) },
        { onSuccess: setEvaluation },
      );
    }, 400);
    return () => clearTimeout(h);
  }, [draft, dirty, data, evaluateDraft]);

  if (plan.isPending) return <LoadingState />;
  if (plan.isError)
    return <QueryErrorState error={plan.error} onRetry={() => void plan.refetch()} />;
  const p = data!;
  const changed = changedIds(draft);
  const unplacedChanged = changed.some((id) => !draft.items[id]!.at);

  const onSave = () => {
    const versions = new Map(p.items.map((i) => [i.allocation_id, i.version]));
    save.mutate(
      {
        body: {
          date,
          base: changed.map((id) => ({ allocation_id: id, version: versions.get(id) ?? 0 })),
          placements: changed.map((id) => {
            const at = draft.items[id]!.at!;
            return { allocation_id: id, x_cm: at.x, y_cm: at.y, rotation_dd: at.rotation };
          }),
        },
      },
      { onSuccess: () => toast.success(t('storage:planner.saved')) },
    );
  };

  const onPreset = () => {
    arrange.mutate(
      { from_on: p.from, to_on: p.to, items: toLayoutInputs(draft) },
      {
        onSuccess: (r) => {
          dispatch({
            type: 'fill',
            placements: r.items.map((i) => ({
              allocationId: i.allocation_id,
              at:
                i.x_cm !== undefined && i.y_cm !== undefined
                  ? ({
                      x: i.x_cm,
                      y: i.y_cm,
                      rotation: asRotation(i.rotation_dd),
                    } satisfies Position)
                  : null,
            })),
          });
          toast.success(
            t('storage:planner.presetDone', {
              moves: r.indicators.moves,
              unplaced: r.indicators.unplaced,
            }),
          );
        },
      },
    );
  };

  const onOpenScenario = () => {
    createScenario.mutate(
      {
        area_id: areaId,
        name: t('storage:scenario.defaultName', { date }),
        from_on: p.from,
        to_on: p.to,
      },
      {
        onSuccess: async (d) => {
          let scenario = d.scenario;
          if (dirty) {
            const saved = await unwrap(
              api.PUT('/api/admin/v1/marinas/{marinaId}/storage/scenarios/{scenarioId}/items', {
                params: {
                  path: { marinaId, scenarioId: scenario.id },
                  header: { 'If-Match': ifMatch(scenario.version) },
                },
                body: { items: toLayoutInputs(draft) },
              }),
            );
            scenario = saved.scenario;
          }
          void navigate({
            to: '/m/$marinaId/storage/$areaId/scenarios/$scenarioId',
            params: { marinaId, areaId, scenarioId: scenario.id },
          });
        },
      },
    );
  };

  const access = evaluation?.access ?? p.access;
  const error = save.error ?? arrange.error ?? createScenario.error ?? evaluate.error;
  return (
    <RequirePermission allowed={can('storage.read')}>
      <PageHeader
        title={t('storage:planner.title', { area: p.area.name || p.area.code })}
        description={t('storage:planner.description')}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/m/$marinaId/storage/$areaId/scenarios" params={{ marinaId, areaId }}>
                {t('storage:scenario.list')}
              </Link>
            </Button>
            {manage ? (
              <>
                <Button variant="outline" onClick={onPreset} disabled={arrange.isPending}>
                  <SparklesIcon />
                  {t('storage:planner.preset')}
                </Button>
                <Button
                  variant="outline"
                  onClick={onOpenScenario}
                  disabled={createScenario.isPending}
                >
                  <FlaskConicalIcon />
                  {t('storage:planner.openScenario')}
                </Button>
                <Button
                  variant="outline"
                  onClick={() => dispatch({ type: 'restore' })}
                  disabled={!dirty}
                >
                  <Undo2Icon />
                  {t('storage:planner.restore')}
                </Button>
                <Button
                  onClick={onSave}
                  disabled={
                    !dirty ||
                    unplacedChanged ||
                    save.isPending ||
                    (evaluation?.issues.length ?? 0) > 0
                  }
                  data-testid="storage-save"
                >
                  <SaveIcon />
                  {t('storage:planner.save', { count: changed.length })}
                </Button>
              </>
            ) : null}
          </>
        }
      />
      {unplacedChanged ? (
        <p className="text-muted-foreground text-sm">{t('storage:planner.unplacedHint')}</p>
      ) : null}
      <ProblemAlert error={error} className="mb-4" />
      <div className="grid gap-4">
        <PlannerWorkspace
          idPrefix="planner"
          from={p.from}
          to={p.to}
          date={date}
          onDate={(d) => void navigate({ search: { date: d }, replace: true })}
          grid={draft.grid}
          draft={draft}
          dispatch={dispatch}
          meta={p.items.filter((i) => i.kind !== 'block' && i.kind !== 'maintenance')}
          exits={p.gates.flatMap((g) => g.cells)}
          obstacles={p.obstacles}
          access={access}
          issues={evaluation?.issues ?? []}
          lens={lens}
          onLens={setLens}
          readOnly={!manage}
        />
        {evaluation ? <IndicatorsCard ind={evaluation.indicators} /> : null}
      </div>
    </RequirePermission>
  );
}
