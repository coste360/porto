import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import {
  ListIcon,
  MapIcon,
  MaximizeIcon,
  PrinterIcon,
  ZoomInIcon,
  ZoomOutIcon,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';

import {
  mapBackgroundUrl,
  useBerthGroups,
  useDayView,
  useDeleteBlock,
  useMap,
  useMaps,
  useOccupancySettingsForOps,
  useUndoMove,
  type MoveResultData,
  type PlacementInputData,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  DEFAULT_LAYERS,
  DEFAULT_LENS_CONTEXT,
  DragGhost,
  HarborMap,
  applyLens,
  daySubjects,
  fitToView,
  focusBox,
  indexDayView,
  indexScene,
  isLensId,
  pxPerCm,
  schemaMinTileCm,
  useMapViewport,
  type DaySubject,
  type LensId,
  type OpsFilters,
  type VesselItem,
} from '@porto/shared/map';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { PageHeader } from '@porto/shared/ui/patterns';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { localName, mapAspectClass } from '../maps/lib';
import { useMarinaToday } from '../registry/lib/today';
import {
  BlockDialog,
  IncomingVesselPanel,
  NewQuoteDialog,
  NewTransitDialog,
  PresenceDialog,
  ReleaseDialog,
  TodayMovementsPanel,
  UnexpectedArrivalDialog,
  type DeskPrefill,
} from '../desk/public';
import { DateSelector } from './components/DateSelector';
import { ElementList, type ListGroup } from './components/ElementList';
import { LegendFilter } from './components/LegendFilter';
import { LensTabs } from './components/LensTabs';
import { MoveDialog, type MoveTarget } from './components/MoveDialog';
import { OpsFiltersPanel } from './components/OpsFiltersPanel';
import { QuickCard } from './components/QuickCard';
import { SwapDialog, type SwapCandidate } from './components/SwapDialog';
import { showUndoToast } from './components/undoToast';
import { isIsoDate } from './lib/dates';
import { legendTokens } from './lib/legendTokens';
import { quickActions, type QuickAction } from './lib/quickActions';
import { useProblemKey } from './lib/useProblemKey';
import { draftKey, queueAdd } from './lib/layoutSession';
import { opsScene, opsVessels, resourceMeta } from './lib/opsScene';
import {
  lastUndo,
  rememberLens,
  rememberMode,
  rememberUndo,
  rememberedLens,
  rememberedMode,
} from './lib/prefs';
import { useMapDrag, type DropResult } from './lib/useMapDrag';
import { useOpsLabels } from './lib/useOpsLabels';

const route = getRouteApi('/auth/m/$marinaId/ops');

type DeskDialog =
  | { kind: 'transit' | 'quote'; prefill: DeskPrefill }
  | { kind: 'presence'; vesselId: string; allocationId?: string; event: 'arrived' | 'departed' }
  | { kind: 'release'; allocationId: string }
  | { kind: 'block'; target: PlacementInputData }
  | { kind: 'unexpected'; target?: PlacementInputData }
  | null;

type MoveState =
  | { kind: 'move'; subject: DaySubject; target?: PlacementInputData }
  | { kind: 'swap'; subject: DaySubject; preset?: string }
  | null;

/**
 * Operational view (spec 9, m3-plan §8.1): map tabs, date, lens tabs, legend filter with counts, filters, the
 * map in scale or schema with vessels, quick card, incoming vessel search, today's movements, drag & drop
 * moves with undo, list alternative. SSE recolours it through query invalidation (AppShell).
 */
export function OpsPage() {
  const { t, i18n } = useT();
  const search = route.useSearch();
  const navigate = useNavigate();
  const { marinaId, can, me } = useCurrentMarina();
  const userId = me?.user.id ?? 'anon';
  const today = useMarinaToday();
  const date = isIsoDate(search.date) ? search.date : today;
  const maps = useMaps(marinaId);
  const mapList = maps.data ?? [];
  const mapId = search.map ?? mapList[0]?.id;
  const docQuery = useMap(marinaId, mapId ?? '');
  const doc = mapId ? docQuery.data : undefined;
  const day = useDayView(marinaId, mapId, date);
  const dv = day.data && day.data.map_id === mapId ? day.data : undefined;
  const groups = useBerthGroups(marinaId);
  const settings = useOccupancySettingsForOps(marinaId, can('harbor.configure') || can('map.view'));
  const undo = useUndoMove(marinaId);
  const deleteBlock = useDeleteBlock(marinaId);
  const pressRef = useRef<string | null>(null);
  const problemKey = useProblemKey();

  const lens: LensId = isLensId(search.lens)
    ? search.lens
    : (rememberedLens(userId) ?? 'occupancy');
  const mode = search.mode ?? rememberedMode(userId) ?? 'scale';
  const [filters, setFilters] = useState<OpsFilters>({});
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const [selectedRef, setSelectedRef] = useState<string | null>(search.focus ?? null);
  const [highlight, setHighlight] = useState<ReadonlySet<string>>(new Set());
  const [showList, setShowList] = useState(false);
  const [legendList, setLegendList] = useState<string | null>(null);
  const [deskDialog, setDeskDialog] = useState<DeskDialog>(null);
  const [moveState, setMoveState] = useState<MoveState>(null);
  const [undoState, setUndoState] = useState(() => lastUndo(marinaId));
  const pickResolver = useRef<((t: PlacementInputData | null) => void) | null>(null);
  const [picking, setPicking] = useState(false);

  const setSearch = useCallback(
    (patch: Partial<typeof search>) =>
      void navigate({
        to: '/m/$marinaId/ops',
        params: { marinaId },
        search: (prev: typeof search) => ({ ...prev, ...patch }),
        replace: true,
      }),
    [navigate, marinaId],
  );

  // ---- scene, day view, lens ----
  const content = useMemo(
    () => ({ minX: 0, minY: 0, maxX: doc?.width_cm ?? 10_000, maxY: doc?.height_cm ?? 10_000 }),
    [doc?.width_cm, doc?.height_cm],
  );
  const { viewport, setViewport, bindContainer, bindSvg, gestures, fit, zoomBy, mapPoint } =
    useMapViewport(content);
  const fitScale = pxPerCm(fitToView(content, viewport.screenW, viewport.screenH));
  const minTile = schemaMinTileCm(fitScale);
  const scene = useMemo(() => (doc ? opsScene(doc, mode, minTile) : []), [doc, mode, minTile]);
  const index = useMemo(() => indexScene(scene), [scene]);
  const dayIndex = useMemo(() => (dv ? indexDayView(dv) : null), [dv]);
  const subjects = useMemo(() => (dv ? daySubjects(dv) : []), [dv]);
  const subjectByRef = useMemo(() => new Map(subjects.map((s) => [s.ref, s])), [subjects]);
  const meta = useMemo(() => (doc ? resourceMeta(doc) : new Map()), [doc]);
  const ctx = useMemo(
    () => ({
      date,
      ...DEFAULT_LENS_CONTEXT,
      ...(settings.data ? { absentDaysThreshold: settings.data.absent_days_threshold } : {}),
    }),
    [date, settings.data],
  );
  const lensResult = useMemo(
    () => (dv ? applyLens(dv, lens, ctx, filters, meta, hidden, subjects) : null),
    [dv, lens, ctx, filters, meta, hidden, subjects],
  );
  const vessels = useMemo<VesselItem[]>(() => (doc && dv ? opsVessels(doc, dv) : []), [doc, dv]);
  const tileLabels = useMemo(() => {
    const m = new Map<string, string>();
    if (mode === 'schema')
      for (const s of subjects) if (s.subject.vessel) m.set(s.ref, s.subject.vessel.name);
    return m;
  }, [subjects, mode]);
  const tokens = useMemo(() => legendTokens(lensResult?.styles.values() ?? []), [lensResult]);
  const labelFor = useOpsLabels(dayIndex?.byResource);
  const vesselLabel = useCallback(
    (v: VesselItem) => {
      const s = subjectByRef.get(v.ref);
      return t('opsMap:element.aria', {
        code: s?.code ?? '',
        status: s ? t(`occupancyEnums:dayStatus.${s.subject.status}`) : '',
        vessel: v.name,
      });
    },
    [subjectByRef, t],
  );

  // ---- focus (?focus= and locate from the desk panels) ----
  const focusOn = useCallback(
    (ref: string) => {
      const item = scene.find((i) => i.ref === ref);
      if (item) setViewport((vp) => focusBox(vp, item.box));
      setSelectedRef(ref);
    },
    [scene, setViewport],
  );
  const focused = useRef<string | null>(null);
  useEffect(() => {
    if (search.focus && scene.length > 0 && focused.current !== search.focus) {
      focused.current = search.focus;
      focusOn(search.focus);
    }
  }, [search.focus, scene, focusOn]);

  // ---- undo ----
  const onMoved = useCallback(
    (r: MoveResultData, message: string) => {
      const u = { undoId: r.undo_id, until: r.undo_until, marinaId };
      rememberUndo(u);
      setUndoState(u);
      showUndoToast(t, message, () => {
        undo.mutate(
          { undoId: r.undo_id },
          {
            onSuccess: () => {
              rememberUndo(null);
              setUndoState(null);
              toast.success(t('opsMap:undo.done'));
            },
            onError: (e) => toast.error(t(problemKey(e))),
          },
        );
      });
    },
    [marinaId, t, undo, problemKey],
  );

  // ---- drag & drop ----
  const onDrop = useCallback(
    (r: DropResult) => {
      const s = subjectByRef.get(r.source.ref);
      if (!s) return;
      if (r.target.kind === 'area') {
        const c = r.target.candidate;
        const areaId = r.target.item.ref;
        const key = draftKey(userId, areaId, date);
        queueAdd(key, {
          allocationId: r.source.allocationId,
          version: r.source.version,
          vessel: {
            id: r.source.vessel.id,
            name: r.source.vessel.name,
            loa_cm: r.source.vessel.loa,
            beam_cm: r.source.vessel.beam,
            hull_type: r.source.vessel.hull,
          },
          placement: {
            x: c.x,
            ...(c.y !== undefined ? { y: c.y } : {}),
            ...(c.rotation !== undefined ? { rotation: c.rotation } : {}),
          },
        });
        void navigate({
          to: '/m/$marinaId/ops/area/$areaId',
          params: { marinaId, areaId },
          search: { date },
        });
        return;
      }
      const target: PlacementInputData = {
        resource_type: r.target.kind === 'berth' ? 'berth' : 'land_resource',
        resource_id: r.target.item.ref,
      };
      if (r.shift && r.target.occupiedBy?.allocationId) {
        setMoveState({ kind: 'swap', subject: s, preset: r.target.occupiedBy.allocationId });
      } else {
        setMoveState({ kind: 'move', subject: s, target });
      }
    },
    [subjectByRef, userId, date, navigate, marinaId],
  );
  const drag = useMapDrag({
    marinaId,
    mapId,
    doc,
    date,
    today,
    enabled: can('occupancy.move') && !picking,
    index,
    subjectByRef,
    mapPoint,
    onDrop,
  });

  // ---- incoming vessel panel ----
  const onSolutions = useCallback(
    (sol: { segments: { resource_id: string; map_id: string }[] }[]) => {
      const set = new Set<string>();
      for (const s of sol)
        for (const seg of s.segments) if (seg.map_id === mapId) set.add(seg.resource_id);
      setHighlight(set);
    },
    [mapId],
  );
  // Hovering or focusing a solution only pans the map to it: opening the quick card here would push the
  // incoming-vessel panel down under the pointer while the operator reaches for the solution's buttons.
  const onFocusSolution = useCallback(
    (sol: { segments: { resource_id: string; map_id: string }[] } | null) => {
      const seg = sol?.segments.find((s) => s.map_id === mapId);
      const item = seg ? scene.find((i) => i.ref === seg.resource_id) : undefined;
      if (item) setViewport((vp) => focusBox(vp, item.box));
    },
    [mapId, scene, setViewport],
  );
  const onPickOnMap = useCallback(
    () =>
      new Promise<PlacementInputData | null>((resolve) => {
        pickResolver.current = resolve;
        setPicking(true);
      }),
    [],
  );

  const pick = (ref: string) => {
    if (picking && pickResolver.current) {
      const s = subjectByRef.get(ref);
      const item = scene.find((i) => i.ref === ref);
      if (item && (item.kind === 'berth' || item.kind === 'land' || item.kind === 'area')) {
        pickResolver.current({
          resource_type:
            item.kind === 'berth'
              ? 'berth'
              : item.kind === 'land'
                ? 'land_resource'
                : 'dynamic_area',
          resource_id: s?.resourceId ?? ref,
        });
      } else pickResolver.current(null);
      pickResolver.current = null;
      setPicking(false);
      return;
    }
    setSelectedRef(ref);
  };

  // ---- quick card ----
  const selected = selectedRef ? subjectByRef.get(selectedRef) : undefined;
  const selectedItem = selectedRef ? scene.find((i) => i.ref === selectedRef) : undefined;
  const selectedArea: DaySubject | undefined =
    selectedItem?.kind === 'area' && dv
      ? ({
          ref: selectedItem.ref,
          resourceId: selectedItem.ref,
          resourceType: 'dynamic_area',
          code: selectedItem.el.code,
          subject: dayIndex?.byResource.get(selectedItem.ref) ?? {
            status: 'free',
            arrival_today: false,
            departure_today: false,
            anomalies: [],
          },
        } satisfies DaySubject)
      : undefined;
  const card = selected ?? selectedArea;
  const actions = card ? quickActions(card, can, { hasUndo: !!undoState }) : [];
  const targetOf = (s: DaySubject): PlacementInputData => ({
    resource_type: s.resourceType,
    resource_id: s.resourceId,
  });

  const onAction = (a: QuickAction) => {
    if (!card) return;
    const v = card.subject.vessel;
    switch (a) {
      case 'newTransit':
      case 'newQuote':
        setDeskDialog({
          kind: a === 'newTransit' ? 'transit' : 'quote',
          prefill: { target: targetOf(card), arrival: date },
        });
        return;
      case 'arrived':
      case 'departed':
        if (v)
          setDeskDialog({
            kind: 'presence',
            vesselId: v.id,
            ...(card.allocationId ? { allocationId: card.allocationId } : {}),
            event: a,
          });
        return;
      case 'move':
        setMoveState({ kind: 'move', subject: card });
        return;
      case 'swap':
        setMoveState({ kind: 'swap', subject: card });
        return;
      case 'release':
        if (card.allocationId) setDeskDialog({ kind: 'release', allocationId: card.allocationId });
        return;
      case 'block':
        setDeskDialog({ kind: 'block', target: targetOf(card) });
        return;
      case 'unblock':
        if (card.allocationId)
          deleteBlock.mutate(
            { allocationId: card.allocationId, version: card.allocationVersion ?? 0 },
            {
              onSuccess: () => toast.success(t('opsMap:quick.unblocked')),
              onError: (e) => toast.error(t(problemKey(e))),
            },
          );
        return;
      case 'openLayout':
        void navigate({
          to: '/m/$marinaId/ops/area/$areaId',
          params: { marinaId, areaId: card.resourceId },
          search: { date },
        });
        return;
      case 'undo':
        if (undoState)
          undo.mutate(
            { undoId: undoState.undoId },
            {
              onSuccess: () => {
                rememberUndo(null);
                setUndoState(null);
                toast.success(t('opsMap:undo.done'));
              },
              onError: (e) => toast.error(t(problemKey(e))),
            },
          );
        return;
    }
  };

  // ---- move/swap dialog data ----
  const moveTargets: MoveTarget[] = useMemo(
    () =>
      doc
        ? [
            ...doc.berths
              .filter((b) => b.id && b.active)
              .map((b) => ({ resourceType: 'berth' as const, resourceId: b.id!, label: b.code })),
            ...doc.dynamic_areas
              .filter((a) => a.id && a.active)
              .map((a) => ({
                resourceType: 'dynamic_area' as const,
                resourceId: a.id!,
                label: a.code,
                area: true,
              })),
            ...doc.land_resources
              .filter((l) => l.id && l.active)
              .map((l) => ({
                resourceType: 'land_resource' as const,
                resourceId: l.id!,
                label: l.code,
              })),
          ]
        : [],
    [doc],
  );
  const swapCandidates: SwapCandidate[] = useMemo(
    () =>
      subjects
        .filter(
          (s) =>
            s.allocationId &&
            s.subject.vessel &&
            s.resourceType !== 'dynamic_area' &&
            s.subject.status !== 'block',
        )
        .map((s) => ({
          allocationId: s.allocationId!,
          version: s.allocationVersion ?? 0,
          label: `${s.code} · ${s.subject.vessel!.name}`,
        })),
    [subjects],
  );

  // ---- list alternative ----
  const listGroups: ListGroup[] = useMemo(() => {
    if (!doc) return [];
    const byStructure = new Map<string, ListGroup>();
    const other: ListGroup = { id: 'other', label: t('opsMap:list.other'), items: [] };
    for (const st of doc.structures)
      if (st.id) byStructure.set(st.id, { id: st.id, label: st.code, items: [] });
    for (const s of subjects) {
      const g = byStructure.get(meta.get(s.resourceId)?.structureId ?? '') ?? other;
      g.items.push(s);
    }
    return [...byStructure.values(), other].filter((g) => g.items.length > 0);
  }, [doc, subjects, meta, t]);

  const legendItems =
    legendList && lensResult
      ? subjects.filter((s) => lensResult.styles.get(s.ref)?.legendKey === legendList)
      : [];

  const loading = maps.isPending || (!!mapId && docQuery.isPending);
  const error = maps.error ?? docQuery.error;

  return (
    <RequirePermission allowed={can('map.view')}>
      <PageHeader
        title={t('opsMap:title')}
        description={t('opsMap:description')}
        actions={
          <>
            {dv && dv.unplaced.length > 0 ? (
              <Badge variant="destructive" data-testid="ops-unplaced-arrivals">
                {t('opsMap:unplacedArrivals', { count: dv.unplaced.length })}
              </Badge>
            ) : null}
            {can('presence.write') ? (
              <Button variant="outline" onClick={() => setDeskDialog({ kind: 'unexpected' })}>
                {t('opsMap:actions.unexpectedArrival')}
              </Button>
            ) : null}
            {mapId ? (
              <Button variant="outline" asChild>
                <Link to="/m/$marinaId/maps/$mapId/print" params={{ marinaId, mapId }}>
                  <PrinterIcon />
                  {t('opsMap:actions.print')}
                </Link>
              </Button>
            ) : null}
          </>
        }
      />
      {loading ? (
        <LoadingState />
      ) : error ? (
        <QueryErrorState
          error={error}
          onRetry={() => void (maps.error ? maps.refetch() : docQuery.refetch())}
        />
      ) : !doc || !mapId ? (
        <p className="text-muted-foreground text-sm">{t('opsMap:noMaps')}</p>
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            {mapList.length > 1 ? (
              <ToggleGroup
                type="single"
                variant="outline"
                value={mapId}
                onValueChange={(v) => {
                  if (v) setSearch({ map: v, focus: undefined });
                }}
                aria-label={t('opsMap:maps')}
              >
                {mapList.map((m) => (
                  <ToggleGroupItem key={m.id} value={m.id}>
                    {localName(m.name, i18n.language)}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            ) : null}
            <DateSelector date={date} today={today} onChange={(d) => setSearch({ date: d })} />
            <ToggleGroup
              type="single"
              variant="outline"
              value={mode}
              onValueChange={(v) => {
                if (v !== 'scale' && v !== 'schema') return;
                rememberMode(userId, v);
                setSearch({ mode: v });
              }}
              aria-label={t('opsMap:mode.label')}
            >
              <ToggleGroupItem value="scale" data-testid="ops-mode-scale">
                {t('opsMap:mode.scale')}
              </ToggleGroupItem>
              <ToggleGroupItem value="schema" data-testid="ops-mode-schema">
                {t('opsMap:mode.schema')}
              </ToggleGroupItem>
            </ToggleGroup>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setShowList((v) => !v)}
              aria-pressed={showList}
              data-testid="ops-list-toggle"
            >
              {showList ? <MapIcon /> : <ListIcon />}
              {showList ? t('opsMap:actions.showMap') : t('opsMap:actions.showList')}
            </Button>
          </div>
          <LensTabs
            lens={lens}
            onChange={(l) => {
              rememberLens(userId, l);
              setHidden(new Set());
              setSearch({ lens: l });
            }}
          />
          <div className="grid gap-3 lg:grid-cols-12">
            <aside className="flex flex-col gap-4 lg:col-span-3">
              <Card>
                <CardHeader>
                  <CardTitle>{t('opsMap:legend.title')}</CardTitle>
                </CardHeader>
                <CardContent>
                  {lensResult ? (
                    <LegendFilter
                      lens={lens}
                      counts={lensResult.counts}
                      tokens={tokens}
                      hidden={hidden}
                      onHiddenChange={setHidden}
                      onShowList={setLegendList}
                    />
                  ) : (
                    <LoadingState />
                  )}
                </CardContent>
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle>{t('opsMap:filters.title')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <OpsFiltersPanel
                    doc={doc}
                    groups={groups.data ?? []}
                    filters={filters}
                    onChange={setFilters}
                  />
                </CardContent>
              </Card>
            </aside>
            <div className="flex flex-col gap-2 lg:col-span-6">
              {showList ? (
                <ElementList
                  groups={listGroups}
                  styles={lensResult?.styles ?? new Map()}
                  dimmed={lensResult?.dimmed ?? new Set()}
                  onPick={(ref) => setSelectedRef(ref)}
                />
              ) : (
                <>
                  <div
                    className="flex items-center gap-2"
                    role="toolbar"
                    aria-label={t('opsMap:toolbar')}
                  >
                    <Button variant="outline" size="sm" onClick={fit}>
                      <MaximizeIcon />
                      {t('opsMap:actions.fit')}
                    </Button>
                    <Button
                      variant="outline"
                      size="icon"
                      onClick={() => zoomBy(1.25)}
                      aria-label={t('opsMap:actions.zoomIn')}
                    >
                      <ZoomInIcon />
                    </Button>
                    <Button
                      variant="outline"
                      size="icon"
                      onClick={() => zoomBy(0.8)}
                      aria-label={t('opsMap:actions.zoomOut')}
                    >
                      <ZoomOutIcon />
                    </Button>
                    {picking ? (
                      <Badge variant="secondary" data-testid="ops-picking">
                        {t('opsMap:picking')}
                      </Badge>
                    ) : null}
                    {day.isFetching ? (
                      <span className="text-muted-foreground text-xs">{t('opsMap:updating')}</span>
                    ) : null}
                  </div>
                  <div
                    ref={bindContainer}
                    className={`bg-muted w-full overflow-hidden rounded-md border ${mapAspectClass(doc.width_cm, doc.height_cm)} max-h-svh lg:aspect-auto lg:h-160`}
                    data-testid="ops-map"
                    data-date={dv?.date}
                    data-lens={lens}
                    data-mode={mode}
                  >
                    <HarborMap
                      svgRef={bindSvg}
                      meta={doc}
                      scene={scene}
                      index={index}
                      viewport={viewport}
                      layers={DEFAULT_LAYERS}
                      selected={selectedRef ? new Set([selectedRef]) : undefined}
                      interactive
                      labelFor={labelFor}
                      vesselLabel={vesselLabel}
                      lensStyles={lensResult?.styles}
                      dimmed={lensResult?.dimmed}
                      vessels={vessels}
                      highlight={highlight}
                      mode={mode}
                      tileLabels={tileLabels}
                      backgroundUrl={
                        doc.background
                          ? mapBackgroundUrl(marinaId, mapId, doc.background.file_id)
                          : undefined
                      }
                      aria-label={t('opsMap:canvas')}
                      role="group"
                      onElementPointerDown={(ref, e) => {
                        if (drag.onElementPointerDown(ref, e)) pressRef.current = ref;
                        else pick(ref);
                      }}
                      onElementActivate={(ref) => pick(ref)}
                      onBackgroundPointerDown={(e) => {
                        gestures.onPointerDown(e, true);
                      }}
                      onPointerMove={(e) => {
                        if (!drag.onPointerMove(e)) gestures.onPointerMove(e);
                      }}
                      onPointerUp={(e) => {
                        const pressed = pressRef.current;
                        pressRef.current = null;
                        // A press on a drag source that did not move is a click: select it.
                        if (!drag.onPointerUp(e) && pressed) pick(pressed);
                        gestures.onPointerUp(e);
                      }}
                      onPointerCancel={(e) => {
                        pressRef.current = null;
                        drag.cancel();
                        gestures.onPointerUp(e);
                      }}
                    >
                      {drag.view ? (
                        <DragGhost
                          state={drag.view.state}
                          vessel={drag.view.source.vessel}
                          origin={drag.view.origin}
                          rotationDd={drag.view.rotationDd}
                          cells={drag.view.cells}
                          conflictCells={drag.view.conflictCells}
                          locale={i18n.language}
                          {...(drag.view.free
                            ? {
                                free: {
                                  ...drag.view.free,
                                  fontCm: Math.max(60, 14 / Math.max(pxPerCm(viewport), 1e-6)),
                                },
                              }
                            : {})}
                        />
                      ) : null}
                    </HarborMap>
                  </div>
                  {drag.view?.preview?.conflicts.length ? (
                    <p
                      className="text-destructive text-sm"
                      role="status"
                      data-testid="ops-drag-conflict"
                    >
                      {drag.view.preview.conflicts
                        .map((c) =>
                          t('opsMap:drag.conflict', {
                            vessel: c.vesselName ?? '',
                            from: c.from,
                            to: c.to,
                          }),
                        )
                        .join(' · ')}
                    </p>
                  ) : null}
                </>
              )}
            </div>
            <aside className="flex flex-col gap-3 lg:col-span-3">
              {card ? (
                <QuickCard
                  marinaId={marinaId}
                  subject={card}
                  element={dayIndex?.byResource.get(card.resourceId)}
                  actions={actions}
                  onAction={onAction}
                  onClose={() => setSelectedRef(null)}
                />
              ) : null}
              {can('transits.desk') ? (
                <IncomingVesselPanel
                  marinaId={marinaId}
                  date={date}
                  onSolutions={onSolutions}
                  onFocusSolution={onFocusSolution}
                  onPickOnMap={onPickOnMap}
                />
              ) : null}
            </aside>
          </div>
          <TodayMovementsPanel
            marinaId={marinaId}
            date={date}
            mapId={mapId}
            onLocate={(ref) => {
              if (ref.map_id && ref.map_id !== mapId)
                setSearch({ map: ref.map_id, focus: ref.resource_id });
              else focusOn(ref.resource_id);
            }}
          />
        </div>
      )}

      <Dialog open={legendList !== null} onOpenChange={(o) => !o && setLegendList(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {legendList ? t(`occupancyEnums:lens.legend.${legendList}`) : ''}
            </DialogTitle>
            <DialogDescription>
              {t('opsMap:legend.listDescription', { count: legendItems.length })}
            </DialogDescription>
          </DialogHeader>
          <ul
            className="flex max-h-96 flex-col gap-1 overflow-y-auto"
            data-testid="ops-legend-list"
          >
            {legendItems.map((s) => (
              <li key={s.ref}>
                <Button
                  variant="link"
                  className="h-auto p-0"
                  onClick={() => {
                    setLegendList(null);
                    focusOn(s.ref);
                  }}
                >
                  {s.subject.vessel ? `${s.code} · ${s.subject.vessel.name}` : s.code}
                </Button>
              </li>
            ))}
          </ul>
        </DialogContent>
      </Dialog>

      {moveState?.kind === 'move' && moveState.subject.allocationId ? (
        <MoveDialog
          marinaId={marinaId}
          open
          onOpenChange={(o) => !o && setMoveState(null)}
          allocationId={moveState.subject.allocationId}
          version={moveState.subject.allocationVersion ?? 0}
          vesselName={moveState.subject.subject.vessel?.name ?? moveState.subject.code}
          date={date}
          today={today}
          {...(moveState.target ? { target: moveState.target } : {})}
          targets={moveTargets}
          canOverride={can('occupancy.override_fit')}
          onMoved={(r) => onMoved(r, t('opsMap:undo.moved'))}
        />
      ) : null}
      {moveState?.kind === 'swap' && moveState.subject.allocationId ? (
        <SwapDialog
          marinaId={marinaId}
          open
          onOpenChange={(o) => !o && setMoveState(null)}
          a={{
            allocationId: moveState.subject.allocationId,
            version: moveState.subject.allocationVersion ?? 0,
            label: `${moveState.subject.code} · ${moveState.subject.subject.vessel?.name ?? ''}`,
          }}
          candidates={swapCandidates}
          {...(moveState.preset ? { preset: moveState.preset } : {})}
          fromNight={date > today ? date : today}
          onSwapped={(r) => onMoved(r, t('opsMap:undo.swapped'))}
        />
      ) : null}

      <NewTransitDialog
        marinaId={marinaId}
        open={deskDialog?.kind === 'transit'}
        onOpenChange={(o) => !o && setDeskDialog(null)}
        {...(deskDialog?.kind === 'transit' ? { prefill: deskDialog.prefill } : {})}
      />
      <NewQuoteDialog
        marinaId={marinaId}
        open={deskDialog?.kind === 'quote'}
        onOpenChange={(o) => !o && setDeskDialog(null)}
        {...(deskDialog?.kind === 'quote' ? { prefill: deskDialog.prefill } : {})}
      />
      {deskDialog?.kind === 'presence' ? (
        <PresenceDialog
          marinaId={marinaId}
          open
          onOpenChange={(o) => !o && setDeskDialog(null)}
          vesselId={deskDialog.vesselId}
          {...(deskDialog.allocationId ? { allocationId: deskDialog.allocationId } : {})}
          event={deskDialog.event}
        />
      ) : null}
      {deskDialog?.kind === 'release' ? (
        <ReleaseDialog
          marinaId={marinaId}
          open
          onOpenChange={(o) => !o && setDeskDialog(null)}
          allocationId={deskDialog.allocationId}
          defaultFrom={date > today ? date : today}
        />
      ) : null}
      {deskDialog?.kind === 'block' ? (
        <BlockDialog
          marinaId={marinaId}
          open
          onOpenChange={(o) => !o && setDeskDialog(null)}
          target={deskDialog.target}
          defaultFrom={date > today ? date : today}
        />
      ) : null}
      <UnexpectedArrivalDialog
        marinaId={marinaId}
        open={deskDialog?.kind === 'unexpected'}
        onOpenChange={(o) => !o && setDeskDialog(null)}
        {...(deskDialog?.kind === 'unexpected' && deskDialog.target
          ? { target: deskDialog.target }
          : {})}
      />
    </RequirePermission>
  );
}
