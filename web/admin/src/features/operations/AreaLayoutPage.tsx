import { useQueries } from '@tanstack/react-query';
import { getRouteApi, Link } from '@tanstack/react-router';
import { ArrowLeftIcon, RotateCwIcon } from 'lucide-react';
import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react';
import { toast } from 'sonner';

import {
  fetchMap,
  isProblem,
  queryKeys,
  useApi,
  useDayView,
  useMaps,
  useProposeCompaction,
  useSaveAreaLayout,
  useUndoMove,
  type DynamicArea,
  type MapDocument,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  DEFAULT_LAYERS,
  DragGhost,
  HarborMap,
  allocationOfRef,
  checkFit,
  expandBox,
  focusBox,
  formatCm,
  indexScene,
  placementRef,
  useMapViewport,
  type GhostState,
  type LensStyle,
  type Pt,
  type VesselItem,
} from '@porto/shared/map';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { useMarinaToday } from '../registry/lib/today';
import { OpsProblem } from './components/OpsProblem';
import { useProblemKey } from './lib/useProblemKey';
import { showUndoToast } from './components/undoToast';
import { axisWidth, type AxisSpan } from './lib/axis';
import { isIsoDate } from './lib/dates';
import {
  draftItems,
  draftKey,
  initialLayout,
  isDirty,
  itemsFromPlacements,
  layoutReducer,
  loadDraft,
  saveBody,
  storeDraft,
  touched,
  unplaced,
  type LayoutItem,
} from './lib/layoutSession';
import { areaPose, opsScene, structuresById } from './lib/opsScene';
import {
  areaCandidate,
  areaFitTarget,
  footprintArea,
  inArea,
  reachOf,
  type AreaCandidate,
} from './lib/targets';

const route = getRouteApi('/auth/m/$marinaId/ops/area/$areaId');

type SessionDrag = { id: string; candidate: AreaCandidate | null; state: GhostState; pointer: Pt };

/** Cells of a placed draft item (linear: footprint interval; surface: indexes), for in-session conflicts. */
function itemCells(a: DynamicArea, it: LayoutItem): number[] {
  if (!it.placement) return [];
  const fa = footprintArea(a);
  const v = { loa: it.vessel.loa_cm, beam: it.vessel.beam_cm };
  try {
    if (a.kind !== 'surface') {
      const w = axisWidth(v, a.mooring_mode, fa.margin);
      const first = Math.floor(it.placement.x / fa.cell);
      const last = Math.ceil((it.placement.x + w) / fa.cell) - 1;
      return Array.from({ length: Math.max(0, last - first + 1) }, (_, i) => first + i);
    }
    const horizontal = !it.placement.rotation || it.placement.rotation === 1800;
    const wx = (horizontal ? v.loa : v.beam) + fa.margin;
    const wy = (horizontal ? v.beam : v.loa) + fa.margin;
    const out: number[] = [];
    for (
      let r = Math.floor((it.placement.y ?? 0) / fa.cell);
      r <= Math.ceil(((it.placement.y ?? 0) + wy) / fa.cell) - 1;
      r++
    )
      for (
        let c = Math.floor(it.placement.x / fa.cell);
        c <= Math.ceil((it.placement.x + wx) / fa.cell) - 1;
        c++
      )
        out.push(r * fa.cols + c);
    return out;
  } catch {
    return [];
  }
}

/**
 * Layout session of a dynamic area (spec 9.4, m3-plan §8.5): a draft of moves on the night's placements, the
 * "Da collocare" list, Save (one transaction) and Restore, compaction preview. A stale save (412) reloads the
 * base, re-applies the draft and names the vessels that changed.
 */
export function AreaLayoutPage() {
  const { t, i18n } = useT();
  const api = useApi();
  const { areaId } = route.useParams();
  const search = route.useSearch();
  const { marinaId, can, me } = useCurrentMarina();
  const today = useMarinaToday();
  const date = isIsoDate(search.date) ? search.date : today;
  const maps = useMaps(marinaId);
  // The map holding the area: every map document is loaded (a handful per marina) and the one with the area kept.
  const docs = useQueries({
    queries: (maps.data ?? []).map((m) => ({
      queryKey: queryKeys.map(marinaId, m.id),
      queryFn: () => fetchMap(api, marinaId, m.id),
    })),
    combine: (rs) => ({
      doc: rs.map((q) => q.data).find((d) => d?.dynamic_areas.some((a) => a.id === areaId)) as
        MapDocument | undefined,
      pending: rs.some((q) => q.isPending),
    }),
  });
  const doc = docs.doc;
  const area = doc?.dynamic_areas.find((a) => a.id === areaId);
  const day = useDayView(marinaId, doc?.id, date);
  const dv = day.data && day.data.map_id === doc?.id ? day.data : undefined;
  const save = useSaveAreaLayout(marinaId, areaId);
  const compact = useProposeCompaction(marinaId, areaId);
  const undo = useUndoMove(marinaId);
  const problemKey = useProblemKey();
  const key = draftKey(me?.user.id ?? 'anon', areaId, date);

  const [state, dispatch] = useReducer(layoutReducer, initialLayout(areaId, date));
  const loadedFor = useRef<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [drag, setDrag] = useState<SessionDrag | null>(null);
  const [rotation, setRotation] = useState(0);

  const serverItems = useMemo(
    () => itemsFromPlacements((dv?.area_placements ?? []).filter((p) => p.area_id === areaId)),
    [dv, areaId],
  );

  useEffect(() => {
    if (!dv) return;
    const v = `${key}|${dv.generated_at}`;
    if (loadedFor.current === v) return;
    if (loadedFor.current?.startsWith(key) && state.ops.length > 0) return; // keep the draft; the save revalidates
    loadedFor.current = v;
    dispatch({ type: 'load', base: serverItems, ops: loadDraft(key) });
  }, [dv, key, serverItems, state.ops.length]);

  useEffect(() => {
    if (loadedFor.current) storeDraft(key, state.ops);
  }, [key, state.ops]);

  // ---- map ----
  const content = useMemo(
    () => ({ minX: 0, minY: 0, maxX: doc?.width_cm ?? 10_000, maxY: doc?.height_cm ?? 10_000 }),
    [doc?.width_cm, doc?.height_cm],
  );
  const { viewport, setViewport, bindContainer, bindSvg, gestures, mapPoint } =
    useMapViewport(content);
  const scene = useMemo(() => (doc ? opsScene(doc, 'scale', 0) : []), [doc]);
  const index = useMemo(() => indexScene(scene), [scene]);
  const areaItem = scene.find((i) => i.ref === areaId);
  const focusedOnce = useRef(false);
  useEffect(() => {
    if (areaItem && viewport.screenW > 0 && !focusedOnce.current) {
      focusedOnce.current = true;
      setViewport((vp) => focusBox(vp, expandBox(areaItem.box, 500)));
    }
  }, [areaItem, viewport.screenW, setViewport]);
  const structures = useMemo(() => (doc ? structuresById(doc) : new Map()), [doc]);
  const structure = area?.structure_id ? structures.get(area.structure_id) : undefined;
  const draft = useMemo(() => draftItems(state), [state]);
  const vessels = useMemo<VesselItem[]>(() => {
    if (!area) return [];
    const out: VesselItem[] = [];
    for (const it of draft) {
      if (!it.placement || it.allocationId === drag?.id) continue;
      const pose = areaPose(
        area,
        structures,
        { loa: it.vessel.loa_cm, beam: it.vessel.beam_cm },
        {
          x: it.placement.x,
          y: it.placement.y ?? 0,
          rotationDd: it.placement.rotation ?? 0,
        },
      );
      if (!pose) continue;
      out.push({
        key: it.allocationId,
        version: it.version,
        ref: placementRef(it.allocationId),
        interactive: true,
        loa: it.vessel.loa_cm,
        beam: it.vessel.beam_cm,
        hull: it.vessel.hull_type,
        name: it.vessel.name,
        ...pose,
      });
    }
    return out;
  }, [area, draft, structures, drag?.id]);
  const touchedIds = useMemo(() => new Set(touched(state).map((d) => d.allocationId)), [state]);
  // Changed by someone else: warn; moved in the draft: override (violet); unchanged: transit colour.
  const lensStyles = useMemo(
    () =>
      new Map<string, LensStyle>(
        vessels.map((v) => [
          v.ref,
          {
            colorToken: state.changed.includes(v.key)
              ? 'warn'
              : touchedIds.has(v.key)
                ? 'override'
                : 'transit',
            legendKey: 'transit',
          },
        ]),
      ),
    [vessels, state.changed, touchedIds],
  );

  // ---- in-session drag (window listeners: works from the map and from the "Da collocare" list) ----
  const evaluate = useCallback(
    (id: string, p: Pt, rot: number): SessionDrag => {
      const it = draft.find((d) => d.allocationId === id);
      if (!area || !it || !inArea(area, structure, p, reachOf(area, it.vessel.loa_cm)))
        return { id, candidate: null, state: 'invalid', pointer: p };
      const others = draft.filter((d) => d.allocationId !== id && d.placement);
      const neighbours: AxisSpan[] = others.map((o) => ({
        x: o.placement!.x,
        w: axisWidth(
          { loa: o.vessel.loa_cm, beam: o.vessel.beam_cm },
          area.mooring_mode,
          area.margin_cm ?? 0,
        ),
      }));
      const cand = areaCandidate(
        area,
        structure,
        { loa: it.vessel.loa_cm, beam: it.vessel.beam_cm },
        p,
        neighbours,
        rot,
      );
      if (cand.error) return { id, candidate: cand, state: 'invalid', pointer: p };
      const taken = new Set(others.flatMap((o) => itemCells(area, o)));
      const clash = cand.cells.some((c) => taken.has(c));
      const fit = checkFit(
        {
          loa: it.vessel.loa_cm,
          beam: it.vessel.beam_cm,
          hull: it.vessel.hull_type,
          insuranceUntil: '9999-12-31',
        },
        areaFitTarget(area),
        date,
      );
      return {
        id,
        candidate: cand,
        state: clash || !fit.ok ? 'conflict' : fit.warnings.length > 0 ? 'warn' : 'ok',
        pointer: p,
      };
    },
    [area, structure, draft, date],
  );

  const startDrag = (id: string) => {
    if (!can('occupancy.move')) return;
    setSelected(id);
    setDrag({ id, candidate: null, state: 'invalid', pointer: { x: 0, y: 0 } });
  };

  useEffect(() => {
    if (!drag) return;
    const move = (e: PointerEvent) => setDrag(evaluate(drag.id, mapPoint(e), rotation));
    const up = (e: PointerEvent) => {
      const r = evaluate(drag.id, mapPoint(e), rotation);
      setDrag(null);
      if (r.candidate && (r.state === 'ok' || r.state === 'warn')) {
        dispatch({
          type: 'move',
          allocationId: r.id,
          placement: {
            x: r.candidate.x,
            ...(r.candidate.y !== undefined ? { y: r.candidate.y } : {}),
            ...(r.candidate.rotation !== undefined ? { rotation: r.candidate.rotation } : {}),
          },
        });
      }
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setDrag(null);
      if ((e.key === 'r' || e.key === 'R') && area?.kind === 'surface')
        setRotation((x) => (x + 900) % 3600);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('keydown', key);
    };
  }, [drag, evaluate, mapPoint, rotation, area?.kind]);

  const dragItem = drag ? draft.find((d) => d.allocationId === drag.id) : undefined;
  const ghostPose =
    drag?.candidate && area && dragItem
      ? areaPose(
          area,
          structures,
          { loa: dragItem.vessel.loa_cm, beam: dragItem.vessel.beam_cm },
          {
            x: drag.candidate.x,
            y: drag.candidate.y ?? 0,
            rotationDd: drag.candidate.rotation ?? 0,
          },
        )
      : null;

  // ---- actions ----
  const toPlace = unplaced(state);
  const dirty = isDirty(state);
  const doSave = () =>
    save.mutate(
      { body: saveBody(state) },
      {
        onSuccess: (r) => {
          storeDraft(key, []);
          loadedFor.current = null;
          dispatch({ type: 'restore' });
          void day.refetch();
          showUndoToast(t, t('opsMap:layout.saved'), () =>
            undo.mutate(
              { undoId: r.undo_id },
              {
                onSuccess: () => toast.success(t('opsMap:undo.done')),
                onError: (e) => toast.error(t(problemKey(e))),
              },
            ),
          );
        },
        onError: async (e) => {
          if (isProblem(e, 'version_conflict')) {
            const fresh = await day.refetch();
            const ps = (fresh.data?.area_placements ?? []).filter((p) => p.area_id === areaId);
            dispatch({ type: 'rebase', base: itemsFromPlacements(ps) });
          }
        },
      },
    );
  const doCompact = () =>
    compact.mutate(
      { from: date, to: date },
      {
        onSuccess: (r) => {
          dispatch({ type: 'compaction', placements: r.placements });
          toast.success(
            t('opsMap:layout.compacted', { freed: formatCm(r.freed_cm, i18n.language) }),
          );
        },
        onError: (e) => toast.error(t(problemKey(e))),
      },
    );

  const loading = maps.isPending || docs.pending;
  const vesselLabel = (v: VesselItem) => t('opsMap:layout.vesselAria', { vessel: v.name });
  const staleNames = state.changed
    .map(
      (id) =>
        serverItems.find((s) => s.allocationId === id)?.vessel.name ??
        draft.find((d) => d.allocationId === id)?.vessel.name ??
        id,
    )
    .join(', ');

  return (
    <RequirePermission allowed={can('map.view')}>
      <PageHeader
        title={t('opsMap:layout.title', { code: area?.code ?? '' })}
        description={t('opsMap:layout.description', { date })}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link
                to="/m/$marinaId/ops"
                params={{ marinaId }}
                search={{ date, ...(doc ? { map: doc.id } : {}), focus: areaId }}
              >
                <ArrowLeftIcon />
                {t('opsMap:layout.back')}
              </Link>
            </Button>
            {area?.kind === 'linear' && can('occupancy.move') ? (
              <Button
                variant="outline"
                onClick={doCompact}
                disabled={compact.isPending}
                data-testid="ops-layout-compact"
              >
                {t('opsMap:layout.compact')}
              </Button>
            ) : null}
            <Button
              variant="outline"
              onClick={() => dispatch({ type: 'restore' })}
              disabled={!dirty}
              data-testid="ops-layout-restore"
            >
              {t('opsMap:layout.restore')}
            </Button>
            <Button
              onClick={doSave}
              disabled={!dirty || toPlace.length > 0 || save.isPending || !can('occupancy.move')}
              data-testid="ops-layout-save"
            >
              {t('opsMap:layout.save')}
            </Button>
          </>
        }
      />
      {loading ? (
        <LoadingState />
      ) : maps.error ? (
        <QueryErrorState error={maps.error} onRetry={() => void maps.refetch()} />
      ) : !doc || !area ? (
        <QueryErrorState error={new Error('not_found')} />
      ) : (
        <div className="grid gap-3 lg:grid-cols-12">
          <div className="flex flex-col gap-2 lg:col-span-9">
            {state.changed.length > 0 ? (
              <Alert data-testid="ops-layout-stale">
                <AlertTitle>{t('opsMap:layout.staleTitle')}</AlertTitle>
                <AlertDescription>
                  {t('opsMap:layout.stale', { vessels: staleNames })}
                </AlertDescription>
              </Alert>
            ) : null}
            {save.error && !isProblem(save.error, 'version_conflict') ? (
              <OpsProblem error={save.error} />
            ) : null}
            {dirty ? (
              <p className="text-muted-foreground text-sm" data-testid="ops-layout-dirty">
                {t('opsMap:layout.draft', { count: state.ops.length })}
              </p>
            ) : null}
            <div
              ref={bindContainer}
              className="bg-muted aspect-video w-full overflow-hidden rounded-md border lg:aspect-auto lg:h-160"
              data-testid="ops-map"
            >
              <HarborMap
                svgRef={bindSvg}
                meta={doc}
                scene={scene}
                index={index}
                viewport={viewport}
                layers={DEFAULT_LAYERS}
                interactive
                lensStyles={lensStyles}
                vessels={vessels}
                vesselLabel={vesselLabel}
                selected={selected ? new Set([placementRef(selected)]) : undefined}
                aria-label={t('opsMap:canvas')}
                role="group"
                onElementPointerDown={(ref) => {
                  const id = allocationOfRef(ref);
                  if (id) startDrag(id);
                }}
                onElementActivate={(ref) => setSelected(allocationOfRef(ref))}
                onBackgroundPointerDown={(e) => {
                  gestures.onPointerDown(e, true);
                }}
                onPointerMove={(e) => gestures.onPointerMove(e)}
                onPointerUp={(e) => gestures.onPointerUp(e)}
                onPointerCancel={(e) => gestures.onPointerUp(e)}
              >
                {drag && dragItem && ghostPose ? (
                  <DragGhost
                    state={drag.state}
                    vessel={{
                      loa: dragItem.vessel.loa_cm,
                      beam: dragItem.vessel.beam_cm,
                      hull: dragItem.vessel.hull_type,
                    }}
                    origin={ghostPose.origin}
                    rotationDd={ghostPose.rotationDd}
                    locale={i18n.language}
                    {...(drag.candidate?.free
                      ? {
                          free: {
                            ...drag.candidate.free,
                            at: {
                              x: drag.pointer.x,
                              y: drag.pointer.y - dragItem.vessel.loa_cm / 2,
                            },
                            fontCm: Math.max(60, dragItem.vessel.beam_cm / 2),
                          },
                        }
                      : {})}
                  />
                ) : null}
              </HarborMap>
            </div>
          </div>
          <aside className="flex flex-col gap-3 lg:col-span-3">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  {t('opsMap:layout.toPlace')}
                  <Badge
                    variant={toPlace.length > 0 ? 'destructive' : 'secondary'}
                    data-testid="ops-unplaced-count"
                  >
                    {toPlace.length + (dv?.unplaced.length ?? 0)}
                  </Badge>
                </CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                {toPlace.length === 0 && (dv?.unplaced.length ?? 0) === 0 ? (
                  <p className="text-muted-foreground text-sm">
                    {t('opsMap:layout.nothingToPlace')}
                  </p>
                ) : null}
                {toPlace.map((it) => (
                  <Button
                    key={it.allocationId}
                    variant="outline"
                    className="touch-none justify-between"
                    data-testid={`ops-to-place-${it.allocationId}`}
                    onPointerDown={() => startDrag(it.allocationId)}
                  >
                    <span>{it.vessel.name}</span>
                    <span className="text-muted-foreground text-xs">
                      {formatCm(it.vessel.loa_cm, i18n.language)} ×{' '}
                      {formatCm(it.vessel.beam_cm, i18n.language)}
                    </span>
                  </Button>
                ))}
                {dv?.unplaced.map((u) => (
                  <p key={u.presence_id} className="text-sm">
                    {u.vessel.name} · {t('opsMap:layout.arrivedWithoutBooking')}
                  </p>
                ))}
                {toPlace.length > 0 ? (
                  <p className="text-muted-foreground text-xs">
                    {t('opsMap:layout.placeAllToSave')}
                  </p>
                ) : null}
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>{t('opsMap:layout.inArea')}</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1">
                {draft
                  .filter((d) => d.placement)
                  .map((d) => (
                    <div
                      key={d.allocationId}
                      className="flex items-center justify-between gap-2 text-sm"
                    >
                      <Button
                        variant="link"
                        className="h-auto p-0"
                        onClick={() => setSelected(d.allocationId)}
                      >
                        {d.vessel.name}
                      </Button>
                      <span className="text-muted-foreground text-xs tabular-nums">
                        {formatCm(d.placement!.x, i18n.language)}
                      </span>
                      {can('occupancy.move') ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          data-testid={`ops-layout-remove-${d.allocationId}`}
                          onClick={() => dispatch({ type: 'remove', allocationId: d.allocationId })}
                        >
                          {t('opsMap:layout.remove')}
                        </Button>
                      ) : null}
                    </div>
                  ))}
                {area.kind === 'surface' ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setRotation((x) => (x + 900) % 3600)}
                  >
                    <RotateCwIcon />
                    {t('opsMap:layout.rotate', { deg: rotation / 10 })}
                  </Button>
                ) : null}
              </CardContent>
            </Card>
          </aside>
        </div>
      )}
    </RequirePermission>
  );
}
