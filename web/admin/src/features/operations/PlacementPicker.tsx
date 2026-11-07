import { useMemo, useState, type PointerEvent } from 'react';

import { useMap, useMapOccupancy, useMaps, type PlacementInputData } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  DEFAULT_LAYERS,
  DragGhost,
  HarborMap,
  berthVesselPose,
  checkFit,
  indexScene,
  rectCorners,
  useMapViewport,
  type GhostState,
  type Pt,
} from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';

import { buildBusyIndex, overlap, previewDrop, type BusyIndex } from './lib/busy';
import { areaPose, opsScene, structuresById } from './lib/opsScene';
import {
  areaCandidate,
  areaFitTarget,
  axisNeighbours,
  berthFitTarget,
  hitTest,
  inArea,
  landFitTarget,
} from './lib/targets';
import { useOpsLabels } from './lib/useOpsLabels';

export type PlacementPickerProps = {
  marinaId: string;
  mapId?: string;
  date: string;
  vessel: { loa_cm: number; beam_cm: number; hull_type: string };
  from: string;
  to: string;
  onPick: (t: PlacementInputData) => void;
  onCancel: () => void;
};

type Hover = {
  target: PlacementInputData;
  state: GhostState;
  origin: Pt;
  rotationDd: number;
  cells: Pt[][];
  label: string;
};

/**
 * Map-in-dialog placement picker with the ghost preview (m3-plan §8.7): hovering a berth, land resource or
 * dynamic area shows the vessel footprint green / yellow / red for [from, to]; a click picks it.
 */
export function PlacementPicker(p: PlacementPickerProps) {
  const { t, i18n } = useT();
  const maps = useMaps(p.marinaId);
  const mapId = p.mapId ?? maps.data?.[0]?.id;
  const doc = useMap(p.marinaId, mapId ?? '').data;
  const occ = useMapOccupancy(p.marinaId, mapId, p.from, p.to);
  const content = useMemo(
    () => ({ minX: 0, minY: 0, maxX: doc?.width_cm ?? 10_000, maxY: doc?.height_cm ?? 10_000 }),
    [doc?.width_cm, doc?.height_cm],
  );
  const { viewport, bindContainer, bindSvg, gestures, mapPoint } = useMapViewport(content);
  const scene = useMemo(() => (doc ? opsScene(doc, 'scale', 0) : []), [doc]);
  const index = useMemo(() => indexScene(scene), [scene]);
  const busy = useMemo<BusyIndex>(
    () => (occ.data ? buildBusyIndex(occ.data) : new Map()),
    [occ.data],
  );
  const labelFor = useOpsLabels(undefined);
  const [hover, setHover] = useState<Hover | null>(null);

  const evaluate = (pt: Pt): Hover | null => {
    if (!doc) return null;
    const hit = hitTest(
      index.query({ minX: pt.x - 1, minY: pt.y - 1, maxX: pt.x + 1, maxY: pt.y + 1 }),
      pt,
    );
    if (!hit) return null;
    const v = { loa: p.vessel.loa_cm, beam: p.vessel.beam_cm };
    const fv = { ...v, hull: p.vessel.hull_type, insuranceUntil: '9999-12-31' };
    if (hit.kind === 'berth' || hit.kind === 'land') {
      const fit = checkFit(
        fv,
        hit.kind === 'berth' ? berthFitTarget(hit.el) : landFitTarget(hit.el),
        p.to,
      );
      const pr = previewDrop({ index: busy, resourceId: hit.ref, from: p.from, to: p.to, fit });
      const pose = berthVesselPose(hit.el, v.loa);
      return {
        target: {
          resource_type: hit.kind === 'berth' ? 'berth' : 'land_resource',
          resource_id: hit.ref,
        },
        state: pr.state,
        ...pose,
        cells: [rectCorners(hit.el)],
        label: hit.el.code,
      };
    }
    if (hit.kind === 'area') {
      const structures = structuresById(doc);
      const a = hit.el;
      const st = a.structure_id ? structures.get(a.structure_id) : undefined;
      if (!inArea(a, st, pt)) return null;
      const entries = (busy.get(hit.ref) ?? []).filter((e) => overlap(e, p.from, p.to));
      const cand = areaCandidate(a, st, v, pt, axisNeighbours(entries, a));
      const fit = checkFit(fv, areaFitTarget(a), p.to);
      const pr = previewDrop({
        index: busy,
        resourceId: hit.ref,
        from: p.from,
        to: p.to,
        cells: cand.cells,
        fit,
        ...(cand.error ? { placementError: cand.error } : {}),
      });
      const pose = areaPose(a, structures, v, {
        x: cand.x,
        y: cand.y ?? 0,
        rotationDd: cand.rotation ?? 0,
      });
      return {
        target: {
          resource_type: 'dynamic_area',
          resource_id: hit.ref,
          x_cm: cand.x,
          ...(cand.y !== undefined && a.kind === 'surface'
            ? { y_cm: cand.y, rotation_dd: cand.rotation ?? 0 }
            : {}),
        },
        state: pr.state,
        origin: pose?.origin ?? pt,
        rotationDd: pose?.rotationDd ?? 0,
        cells: [],
        label: a.code,
      };
    }
    return null;
  };

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    if (gestures.onPointerMove(e)) return;
    setHover(evaluate(mapPoint(e)));
  };

  return (
    <Dialog open onOpenChange={(o) => !o && p.onCancel()}>
      <DialogContent className="sm:max-w-4xl" data-testid="ops-placement-picker">
        <DialogHeader>
          <DialogTitle>{t('opsMap:picker.title')}</DialogTitle>
          <DialogDescription>
            {t('opsMap:picker.description', { from: p.from, to: p.to })}
          </DialogDescription>
        </DialogHeader>
        <div
          ref={bindContainer}
          className="bg-muted aspect-video w-full overflow-hidden rounded-md border"
        >
          {doc ? (
            <HarborMap
              svgRef={bindSvg}
              meta={doc}
              scene={scene}
              index={index}
              viewport={viewport}
              layers={DEFAULT_LAYERS}
              interactive
              labelFor={labelFor}
              aria-label={t('opsMap:canvas')}
              role="group"
              onElementPointerDown={(_ref, e) => {
                const h = evaluate(mapPoint(e));
                if (h && h.state !== 'invalid' && h.state !== 'conflict') p.onPick(h.target);
              }}
              onElementActivate={(ref) => {
                const item = scene.find((i) => i.ref === ref);
                if (item && (item.kind === 'berth' || item.kind === 'land')) {
                  p.onPick({
                    resource_type: item.kind === 'berth' ? 'berth' : 'land_resource',
                    resource_id: ref,
                  });
                }
              }}
              onBackgroundPointerDown={(e) => {
                gestures.onPointerDown(e, true);
              }}
              onPointerMove={onMove}
              onPointerUp={(e) => gestures.onPointerUp(e)}
              onPointerLeave={() => setHover(null)}
            >
              {hover ? (
                <DragGhost
                  state={hover.state}
                  vessel={{
                    loa: p.vessel.loa_cm,
                    beam: p.vessel.beam_cm,
                    hull: p.vessel.hull_type,
                  }}
                  origin={hover.origin}
                  rotationDd={hover.rotationDd}
                  cells={hover.cells}
                  locale={i18n.language}
                />
              ) : null}
            </HarborMap>
          ) : null}
        </div>
        <p className="text-sm" role="status" data-testid="ops-picker-status">
          {hover
            ? t(`opsMap:picker.state.${hover.state}`, { code: hover.label })
            : t('opsMap:picker.hint')}
        </p>
        <DialogFooter>
          <Button variant="outline" onClick={p.onCancel}>
            {t('common:actions.cancel')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
