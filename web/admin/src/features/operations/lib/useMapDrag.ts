// Drag & drop on the operational map (m3-plan §8.5): drag start on a vessel or berth with an allocation, local
// busy index from getMapOccupancy for the remaining nights (cached per drag), footprint + fit at every hover,
// ghost green / yellow / red, then the caller opens the "Da quando" dialog (berths) or the layout session
// (dynamic areas). Advisory only: the server rechecks everything.
import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState, type PointerEvent } from 'react';

import {
  fetchMapOccupancy,
  queryKeys,
  useApi,
  type DayElementData,
  type MapDocument,
  type MapOccupancyData,
} from '@porto/shared/api';
import {
  berthVesselPose,
  checkFit,
  rectCorners,
  type DaySubject,
  type FitOutcome,
  type GhostState,
  type Pt,
  type SceneItem,
  type SpatialIndex,
} from '@porto/shared/map';

import { buildBusyIndex, overlap, previewDrop, type BusyIndex, type Preview } from './busy';
import { addDays, maxDate } from './dates';
import { areaPose, structuresById } from './opsScene';
import {
  areaCandidate,
  areaFitTarget,
  axisNeighbours,
  berthFitTarget,
  hitTest,
  inArea,
  landFitTarget,
  reachOf,
  type AreaCandidate,
} from './targets';

/** Movement (px) before a press becomes a drag. */
export const DRAG_THRESHOLD_PX = 6;
/** Nights fetched ahead for the busy index (getMapOccupancy allows 400). */
const LOOKAHEAD_NIGHTS = 366;

export type DragSource = {
  ref: string;
  allocationId: string;
  version: number;
  vessel: { id: string; name: string; loa: number; beam: number; hull: string };
  /** Resource of the source (berth/land/area id). */
  resourceId: string;
  resourceType: DayElementData['resource_type'];
  insuranceUntil?: string | null;
};

export type DropTarget =
  | { kind: 'berth' | 'land'; item: SceneItem; occupiedBy?: DaySubject }
  | { kind: 'area'; item: SceneItem; candidate: AreaCandidate };

export type DragView = {
  source: DragSource;
  target: DropTarget | null;
  preview: Preview | null;
  state: GhostState;
  origin: Pt;
  rotationDd: number;
  cells: Pt[][];
  conflictCells: Set<number>;
  free?: { left: number; right: number; at: Pt };
  from: string;
  to: string;
};

export type DropResult = {
  source: DragSource;
  target: DropTarget;
  shift: boolean;
  from: string;
  to: string;
};

type Pending = { source: DragSource; startX: number; startY: number; pointerId: number };

export function useMapDrag(opts: {
  marinaId: string;
  mapId: string | undefined;
  doc: MapDocument | undefined;
  date: string;
  today: string;
  enabled: boolean;
  index: SpatialIndex<SceneItem> | null;
  subjectByRef: ReadonlyMap<string, DaySubject>;
  mapPoint: (e: { clientX: number; clientY: number }) => Pt;
  onDrop: (r: DropResult) => void;
}) {
  const api = useApi();
  const qc = useQueryClient();
  const pending = useRef<Pending | null>(null);
  const busy = useRef<{ index: BusyIndex; last: string } | null>(null);
  const [view, setView] = useState<DragView | null>(null);
  const [rotation, setRotation] = useState(0);
  const viewRef = useRef<DragView | null>(null);
  const show = (v: DragView | null) => {
    viewRef.current = v;
    setView(v);
  };

  const { marinaId, mapId, doc, date, today, index, subjectByRef, mapPoint, onDrop } = opts;
  const from = maxDate(date, today);

  const loadBusy = useCallback(
    async (source: DragSource) => {
      if (!mapId) return;
      const to = addDays(from, LOOKAHEAD_NIGHTS);
      const occ = await qc.fetchQuery<MapOccupancyData>({
        queryKey: queryKeys.mapOccupancy(marinaId, mapId, from, to),
        queryFn: () => fetchMapOccupancy(api, marinaId, mapId, from, to),
        staleTime: 15_000,
      });
      const own = occ.allocations.find((a) => a.id === source.allocationId);
      busy.current = {
        index: buildBusyIndex(occ, new Set([source.allocationId])),
        last: own?.last_night ?? from,
      };
    },
    [api, qc, marinaId, mapId, from],
  );

  const compute = useCallback(
    (source: DragSource, p: Pt, rot: number): DragView => {
      const last = busy.current?.last ?? from;
      const base: DragView = {
        source,
        target: null,
        preview: null,
        state: 'invalid',
        origin: p,
        rotationDd: 0,
        cells: [],
        conflictCells: new Set(),
        from,
        to: last,
      };
      if (!doc || !index) return base;
      const hits = index.query({ minX: p.x - 1, minY: p.y - 1, maxX: p.x + 1, maxY: p.y + 1 });
      const structures = structuresById(doc);
      const hit = hitTest(hits, p);
      const vessel = { loa: source.vessel.loa, beam: source.vessel.beam };
      const fitVessel = {
        ...vessel,
        hull: source.vessel.hull,
        insuranceUntil: source.insuranceUntil ?? '9999-12-31',
      };
      const idx: BusyIndex = busy.current?.index ?? new Map();
      if (hit && (hit.kind === 'berth' || hit.kind === 'land')) {
        const el = hit.el;
        const fit: FitOutcome = checkFit(
          fitVessel,
          hit.kind === 'berth' ? berthFitTarget(hit.el) : landFitTarget(hit.el),
          last,
        );
        const preview = previewDrop({ index: idx, resourceId: hit.ref, from, to: last, fit });
        const pose = berthVesselPose(el, vessel.loa);
        const occupiedBy = subjectByRef.get(hit.ref);
        return {
          ...base,
          target: {
            kind: hit.kind,
            item: hit,
            ...(occupiedBy?.allocationId ? { occupiedBy } : {}),
          },
          preview,
          state: hit.ref === source.resourceId ? 'invalid' : preview.state,
          origin: pose.origin,
          rotationDd: pose.rotationDd,
          cells: [rectCorners(el)],
        };
      }
      if (hit && hit.kind === 'area') {
        const a = hit.el;
        const structure = a.structure_id ? structures.get(a.structure_id) : undefined;
        if (!inArea(a, structure, p, reachOf(a, vessel.loa))) return base;
        const entries = (idx.get(hit.ref) ?? []).filter((e) => overlap(e, from, last));
        const cand = areaCandidate(a, structure, vessel, p, axisNeighbours(entries, a), rot);
        const fit = checkFit(fitVessel, areaFitTarget(a), last);
        const preview = previewDrop({
          index: idx,
          resourceId: hit.ref,
          from,
          to: last,
          cells: cand.cells,
          fit,
          ...(cand.error ? { placementError: cand.error } : {}),
        });
        const pose = areaPose(a, structures, vessel, {
          x: cand.x,
          y: cand.y ?? 0,
          rotationDd: cand.rotation ?? 0,
        });
        return {
          ...base,
          target: { kind: 'area', item: hit, candidate: cand },
          preview,
          state: preview.state,
          origin: pose?.origin ?? p,
          rotationDd: pose?.rotationDd ?? 0,
          ...(cand.free ? { free: { ...cand.free, at: { x: p.x, y: p.y - vessel.loa / 2 } } } : {}),
        };
      }
      return base;
    },
    [doc, index, from, subjectByRef],
  );

  const cancel = useCallback(() => {
    pending.current = null;
    busy.current = null;
    viewRef.current = null;
    setView(null);
    setRotation(0);
  }, []);

  useEffect(() => {
    if (!view) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') cancel();
      if (e.key === 'r' || e.key === 'R') setRotation((r) => (r + 900) % 3600);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [view, cancel]);

  /** Element press: a drag source when it has an allocation and moving is allowed. Returns whether it is one. */
  const onElementPointerDown = (ref: string, e: PointerEvent<SVGSVGElement>): boolean => {
    if (!opts.enabled) return false;
    const s = subjectByRef.get(ref);
    if (!s || !s.allocationId || !s.subject.vessel) return false;
    if (s.subject.status === 'block' || s.subject.status === 'maintenance') return false;
    const v = s.subject.vessel;
    pending.current = {
      source: {
        ref,
        allocationId: s.allocationId,
        version: s.allocationVersion ?? 0,
        vessel: { id: v.id, name: v.name, loa: v.loa_cm, beam: v.beam_cm, hull: v.hull_type },
        resourceId: s.resourceId,
        resourceType: s.resourceType,
        insuranceUntil: s.subject.deadlines?.insurance === 'missing' ? null : undefined,
      },
      startX: e.clientX,
      startY: e.clientY,
      pointerId: e.pointerId,
    };
    return true;
  };

  /** Returns true while a drag consumes the move. */
  const onPointerMove = (e: PointerEvent<SVGSVGElement>): boolean => {
    const pend = pending.current;
    if (!pend) return false;
    if (!viewRef.current) {
      if (Math.hypot(e.clientX - pend.startX, e.clientY - pend.startY) < DRAG_THRESHOLD_PX)
        return true;
      e.currentTarget.setPointerCapture?.(pend.pointerId);
      void loadBusy(pend.source).then(() => {
        if (pending.current === pend && viewRef.current)
          show(compute(pend.source, viewRef.current.origin, rotation));
      });
    }
    show(compute(pend.source, mapPoint(e), rotation));
    return true;
  };

  /** Ends a press: a drop when a drag was active (returns true), a click otherwise (returns false). */
  const onPointerUp = (e: PointerEvent<SVGSVGElement>): boolean => {
    const pend = pending.current;
    const v = viewRef.current;
    pending.current = null;
    if (!pend) return false;
    if (!v) return false;
    const final = compute(pend.source, mapPoint(e), rotation);
    cancel();
    if (final.target && final.state !== 'invalid') {
      onDrop({
        source: pend.source,
        target: final.target,
        shift: e.shiftKey,
        from: final.from,
        to: final.to,
      });
    }
    return true;
  };

  return { view, onElementPointerDown, onPointerMove, onPointerUp, cancel, dragging: !!view };
}
