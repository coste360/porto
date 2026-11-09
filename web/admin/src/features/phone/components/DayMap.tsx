import { MaximizeIcon, ZoomInIcon, ZoomOutIcon } from 'lucide-react';
import { useCallback, useMemo, useRef, useState } from 'react';

import {
  useDayView,
  useMap,
  useMapPaymentStatus,
  useMaps,
  type DayElementData,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  DEFAULT_LAYERS,
  HarborMap,
  TOKEN_CLASSES,
  daySubjects,
  fitToView,
  indexDayView,
  indexScene,
  pxPerCm,
  schemaMinTileCm,
  useMapViewport,
  type DaySubject,
} from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { useFillHeight } from '../../../lib/fillHeight';
import { mapAspectClass } from '../../maps/lib';
import { opsScene } from '../../operations/lib/opsScene';
import { useOpsLabels } from '../../operations/lib/useOpsLabels';
import { MAP_CONTEXTS, applyContext, legendLabelKey, type MapContext } from '../lib/mapContext';
import { StaleSince } from './StaleSince';

const TAP_SLOP_PX = 8;

export type MapSelection = {
  subject: DaySubject;
  element?: DayElementData;
  /** Payment state of the resource (lens enum), when known. */
  payment?: { state: string; suspended: boolean };
};

/**
 * Day map of the phone (m6b-plan §6.2 Mappa, §6.3): the main map in schema mode, a context switch with legend
 * counts, pinch and pan; a tap selects an element (the caller opens its bottom sheet). Read from the cached day
 * view when offline; map background images are not cached (D22) and not drawn here.
 */
export function DayMap({
  date,
  onSelect,
}: {
  date: string;
  onSelect: (selection: MapSelection) => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const maps = useMaps(marinaId);
  const mapId = maps.data?.[0]?.id;
  const docQuery = useMap(marinaId, mapId ?? '');
  const doc = mapId ? docQuery.data : undefined;
  const day = useDayView(marinaId, mapId, date);
  const dv = day.data && day.data.map_id === mapId ? day.data : undefined;
  const [ctx, setCtx] = useState<MapContext>('presence');
  // The payment states feed the payments context and the element sheet (enum states only, cached offline).
  const payments = useMapPaymentStatus(marinaId, mapId ?? '', date, !!mapId);
  const content = useMemo(
    () => ({ minX: 0, minY: 0, maxX: doc?.width_cm ?? 10_000, maxY: doc?.height_cm ?? 10_000 }),
    [doc?.width_cm, doc?.height_cm],
  );
  const { viewport, bindContainer, bindSvg, gestures, fit, zoomBy } = useMapViewport(content);
  // The canvas fills the screen below its top, leaving room for the legend and the tab bar (tall phones).
  const [fillRef, canvasHeight] = useFillHeight<HTMLDivElement>({
    reserve: 160,
    fallback: doc ? mapAspectClass(doc.width_cm, doc.height_cm) : 'aspect-4/3',
  });
  const canvasRef = useCallback(
    (el: HTMLDivElement | null) => {
      bindContainer(el);
      fillRef(el);
    },
    [bindContainer, fillRef],
  );
  const minTile = schemaMinTileCm(pxPerCm(fitToView(content, viewport.screenW, viewport.screenH)));
  const scene = useMemo(() => (doc ? opsScene(doc, 'schema', minTile) : []), [doc, minTile]);
  const index = useMemo(() => indexScene(scene), [scene]);
  const dayIndex = useMemo(() => (dv ? indexDayView(dv) : null), [dv]);
  const subjects = useMemo(() => (dv ? daySubjects(dv) : []), [dv]);
  const byRef = useMemo(() => new Map(subjects.map((s) => [s.ref, s])), [subjects]);
  const result = useMemo(
    () => (dv ? applyContext(ctx, dv, subjects, payments.data) : null),
    [ctx, dv, subjects, payments.data],
  );
  const tileLabels = useMemo(() => {
    const m = new Map<string, string>();
    for (const s of subjects) if (s.subject.vessel) m.set(s.ref, s.subject.vessel.name);
    return m;
  }, [subjects]);
  const labelFor = useOpsLabels(dayIndex?.byResource);
  // A tap selects; a press that moves more than TAP_SLOP_PX pans (berths cover most of a phone screen).
  const pressRef = useRef<{ ref: string; x: number; y: number } | null>(null);
  const [selected, setSelected] = useState<string | null>(null);

  const pick = (ref: string) => {
    const s = byRef.get(ref);
    if (!s) return;
    setSelected(ref);
    const element = dayIndex?.byResource.get(s.resourceId);
    const pay = payments.data?.resources.find(
      (r) => r.resource_id === s.resourceId && r.resource_type === s.resourceType,
    );
    onSelect({
      subject: s,
      ...(element ? { element } : {}),
      ...(pay ? { payment: { state: pay.state, suspended: pay.suspended } } : {}),
    });
  };

  if (maps.isPending || (!!mapId && docQuery.isPending)) return <LoadingState />;
  if (maps.isError || docQuery.isError)
    return (
      <QueryErrorState
        error={maps.error ?? docQuery.error}
        onRetry={() => void (maps.isError ? maps.refetch() : docQuery.refetch())}
      />
    );
  if (!doc || !mapId)
    return <p className="text-muted-foreground text-sm">{t('phone:dock.map.noMaps')}</p>;

  return (
    <div className="flex flex-col gap-3" data-testid="phone-map">
      <StaleSince updatedAt={day.dataUpdatedAt} />
      <ToggleGroup
        type="single"
        variant="outline"
        value={ctx}
        onValueChange={(v) => {
          if ((MAP_CONTEXTS as readonly string[]).includes(v)) setCtx(v as MapContext);
        }}
        aria-label={t('phone:dock.map.context')}
        className="flex w-full"
      >
        {MAP_CONTEXTS.map((c) => (
          <ToggleGroupItem
            key={c}
            value={c}
            className="h-auto min-h-11 flex-1 px-1 text-xs leading-tight whitespace-normal"
            data-testid={`phone-map-context-${c}`}
          >
            {t(`phone:dock.map.contexts.${c}`)}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <div className="flex items-center gap-2">
        <Button variant="outline" className="min-h-11" onClick={fit}>
          <MaximizeIcon />
          {t('phone:dock.map.fit')}
        </Button>
        <Button
          variant="outline"
          size="icon"
          className="size-11"
          onClick={() => zoomBy(1.25)}
          aria-label={t('phone:dock.map.zoomIn')}
        >
          <ZoomInIcon />
        </Button>
        <Button
          variant="outline"
          size="icon"
          className="size-11"
          onClick={() => zoomBy(0.8)}
          aria-label={t('phone:dock.map.zoomOut')}
        >
          <ZoomOutIcon />
        </Button>
      </div>
      <div
        ref={canvasRef}
        className={`bg-muted w-full touch-none overflow-hidden rounded-md border ${canvasHeight} max-h-svh`}
        data-testid="phone-map-canvas"
        data-context={ctx}
        data-date={dv?.date}
      >
        <HarborMap
          svgRef={bindSvg}
          meta={doc}
          scene={scene}
          index={index}
          viewport={viewport}
          layers={DEFAULT_LAYERS}
          selected={selected ? new Set([selected]) : undefined}
          interactive
          labelFor={labelFor}
          lensStyles={result?.styles}
          mode="schema"
          tileLabels={tileLabels}
          aria-label={t('phone:dock.map.canvas')}
          role="group"
          onElementPointerDown={(ref, e) => {
            pressRef.current = { ref, x: e.clientX, y: e.clientY };
            gestures.onPointerDown(e, true);
          }}
          onElementActivate={(ref) => pick(ref)}
          onBackgroundPointerDown={(e) => {
            gestures.onPointerDown(e, true);
          }}
          onPointerMove={(e) => {
            const p = pressRef.current;
            if (p && Math.hypot(e.clientX - p.x, e.clientY - p.y) > TAP_SLOP_PX)
              pressRef.current = null;
            gestures.onPointerMove(e);
          }}
          onPointerUp={(e) => {
            const pressed = pressRef.current;
            pressRef.current = null;
            if (pressed) pick(pressed.ref);
            gestures.onPointerUp(e);
          }}
          onPointerCancel={(e) => {
            pressRef.current = null;
            gestures.onPointerUp(e);
          }}
        />
      </div>
      {result ? (
        <ul
          className="flex flex-wrap gap-x-4 gap-y-1"
          aria-label={t('phone:dock.map.legend')}
          data-testid="phone-map-legend"
        >
          {result.legend.map((l) => (
            <li
              key={l.key}
              className="flex items-center gap-2 text-sm"
              data-testid={`phone-map-legend-${l.key}`}
            >
              <span
                className={`size-3 shrink-0 rounded-sm ${TOKEN_CLASSES[l.token].bg}`}
                aria-hidden
              />
              {t(legendLabelKey(ctx, l.key))}
              <span className="tabular-nums">{l.count}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
