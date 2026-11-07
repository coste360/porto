import {
  useId,
  useMemo,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
  type Ref,
  type SVGProps,
} from 'react';

import { useTranslation } from 'react-i18next';

import type { MapMeta, Structure } from '../api/schema';
import { Background } from './elements/Background';
import { RectElement, rectLabelFontCm } from './elements/Berth';
import { lodFor, structureLabelFontCm, type Lod } from './elements/common';
import { DecorationElement } from './elements/Decoration';
import { DynamicAreaElement } from './elements/DynamicArea';
import { GlyphDefs } from './elements/Glyphs';
import { LensFill, lensLod } from './elements/LensFill';
import { Vessel } from './elements/Vessel';
import { Grid, NorthArrow, ScaleBar } from './elements/Overlays';
import { PedestalElement } from './elements/Pedestal';
import { ConcessionOutline, WaterLand } from './elements/Polygons';
import { StructureElement, structureNominalFontCm } from './elements/Structure';
import type { VesselItem } from './dayView';
import { boxesIntersect, expandBox, type Box } from './geometry';
import type { GlyphId, LensStyle } from './lenses/types';
import { KitDefs } from './kit/KitDefs';
import { DEFAULT_LAYERS, type LayerId, type LayerVisibility } from './layers';
import {
  DEFAULT_DECORATION_SIZE_CM,
  PEDESTAL_SIZE_CM,
  isBuoy,
  type ElementKind,
  type SceneItem,
} from './scene';
import { SpatialIndex } from './spatialIndex';
import { MAP_TOKENS, type ElementState } from './tokens';
import { pxPerCm, viewBoxAttr, viewBoxOf, type Viewport } from './viewport';

export type HarborMapMeta = Pick<
  MapMeta,
  | 'width_cm'
  | 'height_cm'
  | 'grid_cm'
  | 'water_polygons'
  | 'land_polygons'
  | 'concession_outline'
  | 'background'
>;

export const LAYER_OF_KIND: Record<ElementKind, LayerId> = {
  structure: 'structures',
  berth: 'berths',
  area: 'areas',
  land: 'land_resources',
  pedestal: 'pedestals',
  decoration: 'decorations',
};

export type HarborMapProps = Omit<SVGProps<SVGSVGElement>, 'ref' | 'viewBox' | 'children'> & {
  meta: HarborMapMeta;
  /** Elements with bounds (buildScene). Keep the array and its items stable between renders when unchanged. */
  scene: readonly SceneItem[];
  /** Optional prebuilt index of `scene` (built here otherwise). */
  index?: SpatialIndex<SceneItem>;
  viewport: Viewport;
  layers?: LayerVisibility;
  selected?: ReadonlySet<string>;
  /** Refs that have validation issues. */
  issues?: ReadonlySet<string>;
  /** Unsaved generator preview, drawn on top in preview tokens. */
  preview?: readonly SceneItem[];
  /** URL of the background image (getMapBackground). */
  backgroundUrl?: string;
  /** Elements are focusable buttons that report pointer and keyboard activation. */
  interactive?: boolean;
  /** Accessible name of an element. */
  labelFor?: (item: SceneItem) => string;
  onElementPointerDown?: (ref: string, event: PointerEvent<SVGSVGElement>) => void;
  onBackgroundPointerDown?: (event: PointerEvent<SVGSVGElement>) => void;
  onElementActivate?: (ref: string, event: KeyboardEvent<SVGSVGElement>) => void;
  /** Overlay drawn in map coordinates (handles, guides, marquee). */
  children?: ReactNode;
  svgRef?: Ref<SVGSVGElement>;
  /** Hide the screen-fixed scale bar and north arrow (print page draws its own). */
  hideChrome?: boolean;
  // ---- operational view (M3, m3-plan §8.2); absent = editor/viewer behaviour unchanged ----
  /** Lens style of berths, land resources (by ref) and area vessels (by placement ref). */
  lensStyles?: ReadonlyMap<string, LensStyle>;
  /** Refs greyed by the filters or the legend. */
  dimmed?: ReadonlySet<string>;
  /** Vessel silhouettes in scale (not drawn in schema mode on berths). */
  vessels?: readonly VesselItem[];
  /** Refs outlined (search solutions, focus). */
  highlight?: ReadonlySet<string>;
  /** Scale (geometry, silhouettes) or schema (tiles with code and vessel name; berths of the scene are tiles). */
  mode?: 'scale' | 'schema';
  /** Second line of a schema tile (vessel name) by ref. */
  tileLabels?: ReadonlyMap<string, string>;
  /** Accessible name of an interactive vessel silhouette. */
  vesselLabel?: (item: VesselItem) => string;
};

/** Bounds of a vessel silhouette (loa around its stern, any heading). */
function vesselBox(v: VesselItem): Box {
  const r = v.loa + v.beam;
  return { minX: v.origin.x - r, minY: v.origin.y - r, maxX: v.origin.x + r, maxY: v.origin.y + r };
}

/** Below this on-screen beam (px) silhouettes are not drawn (the berth is a filled rect). */
const VESSEL_MIN_PX = 5;

/** Cull margin around the view box, as a fraction of its size. */
const CULL_MARGIN = 0.1;

function itemLod(item: SceneItem, s: number, labels: boolean): Lod {
  switch (item.kind) {
    case 'berth': {
      const b = item.el;
      if (isBuoy(b)) {
        const r = b.swing_radius_cm ?? b.length_cm / 2;
        return lodFor(2 * r, r / 3, s, labels);
      }
      return lodFor(
        Math.min(b.width_cm, b.length_cm),
        rectLabelFontCm(b.width_cm, b.length_cm),
        s,
        labels,
      );
    }
    case 'land':
      return lodFor(
        Math.min(item.el.width_cm, item.el.length_cm),
        rectLabelFontCm(item.el.width_cm, item.el.length_cm),
        s,
        labels,
      );
    case 'structure':
      return lodFor(
        Math.max(item.el.width_cm ?? 100, 100),
        Math.max(80, (item.el.width_cm ?? 200) * 0.6),
        s,
        labels,
      );
    case 'area':
      return lodFor(200, 200, s, labels);
    case 'pedestal':
      return lodFor(PEDESTAL_SIZE_CM, PEDESTAL_SIZE_CM, s, labels);
    case 'decoration': {
      const size = item.el.size_cm ?? DEFAULT_DECORATION_SIZE_CM;
      // Free text is drawn at size/2, the other labels (depth, N, scale) at size/3 or smaller.
      return lodFor(size, item.el.kind === 'text' ? size / 2 : size / 3, s, labels);
    }
  }
}

/** Spatial index of scene items by their bounds. */
export function indexScene(scene: readonly SceneItem[]): SpatialIndex<SceneItem> {
  return new SpatialIndex(scene.map((item) => ({ box: item.box, item })));
}

function findRef(target: EventTarget | null): string | null {
  const el = target instanceof Element ? target.closest('[data-ref]') : null;
  return el?.getAttribute('data-ref') ?? null;
}

/**
 * The harbour map in scale (ADR 0004): one `<svg>` whose viewBox is in cm, layers, culling through a uniform
 * grid index, level of detail by on-screen size, and memoised elements (an element re-renders only when its
 * object, state or level of detail changes). Shared by the viewer, the editor, the print page and, from M3,
 * the operational view.
 */
export function HarborMap({
  meta,
  scene,
  index: givenIndex,
  viewport,
  layers = DEFAULT_LAYERS,
  selected,
  issues,
  preview,
  backgroundUrl,
  interactive = false,
  labelFor,
  onElementPointerDown,
  onBackgroundPointerDown,
  onElementActivate,
  children,
  svgRef,
  hideChrome = false,
  lensStyles,
  dimmed,
  vessels,
  highlight,
  mode = 'scale',
  tileLabels,
  vesselLabel,
  className,
  ...svgProps
}: HarborMapProps) {
  const uid = useId().replace(/[^A-Za-z0-9_-]/g, '');
  const { i18n } = useTranslation();
  const locale = i18n.language || 'it';
  const builtIndex = useMemo(() => (givenIndex ? null : indexScene(scene)), [givenIndex, scene]);
  const index = givenIndex ?? builtIndex!;
  const structures = useMemo(() => {
    const m = new Map<string, Structure>();
    for (const item of scene) if (item.kind === 'structure') m.set(item.ref, item.el);
    return m;
  }, [scene]);
  const kitAssets = useMemo(() => {
    const set = new Set<string>();
    for (const item of [...scene, ...(preview ?? [])]) {
      if (item.kind === 'decoration' && item.el.kind === 'symbol' && item.el.asset)
        set.add(item.el.asset);
    }
    return set;
  }, [scene, preview]);

  const glyphs = useMemo(() => {
    const set = new Set<GlyphId>();
    if (lensStyles) for (const st of lensStyles.values()) if (st.glyph) set.add(st.glyph);
    return set;
  }, [lensStyles]);

  const s = pxPerCm(viewport);
  const view = viewBoxOf(viewport);
  const cullBox = expandBox(view, Math.max(viewport.w, viewport.h) * CULL_MARGIN);
  const visible = index.query(cullBox);
  const visibleVessels =
    vessels && mode === 'scale'
      ? vessels.filter((v) => boxesIntersect(vesselBox(v), cullBox) && v.beam * s >= VESSEL_MIN_PX)
      : [];

  const render = (item: SceneItem, state: ElementState, isInteractive: boolean) => {
    if (!layers[LAYER_OF_KIND[item.kind]]) return null;
    const lod = itemLod(item, s, layers.labels);
    const label = isInteractive && labelFor ? labelFor(item) : '';
    const common = { refId: item.ref, state, lod, interactive: isInteractive, label };
    switch (item.kind) {
      case 'structure':
        return (
          <StructureElement
            key={item.ref}
            {...common}
            el={item.el}
            labelFontCm={
              layers.labels ? structureLabelFontCm(structureNominalFontCm(item.el), s) : undefined
            }
          />
        );
      case 'berth':
      case 'land': {
        const lens = lensStyles?.get(item.ref);
        if (lens && state !== 'preview') {
          const el = item.el;
          const buoy = item.kind === 'berth' && isBuoy(item.el);
          const size = buoy
            ? 2 * (item.el.swing_radius_cm ?? el.length_cm / 2)
            : Math.min(el.width_cm, el.length_cm);
          return (
            <LensFill
              key={item.ref}
              refId={item.ref}
              kind={item.kind}
              geom={el}
              buoyRadiusCm={buoy ? size / 2 : undefined}
              code={el.code}
              lensStyle={lens}
              lod={lensLod(size, s)}
              pxPerCm={s}
              dimmed={dimmed?.has(item.ref) ?? false}
              selected={state === 'selected'}
              highlighted={highlight?.has(item.ref) ?? false}
              schema={mode === 'schema'}
              secondary={tileLabels?.get(item.ref)}
              glyphPrefix={uid}
              interactive={isInteractive}
              label={label}
            />
          );
        }
        return item.kind === 'berth' ? (
          <RectElement key={item.ref} {...common} el={item.el} kind="berth" />
        ) : (
          <RectElement key={item.ref} {...common} el={item.el} kind="land" />
        );
      }
      case 'area':
        return (
          <DynamicAreaElement
            key={item.ref}
            {...common}
            el={item.el}
            structure={item.el.structure_id ? structures.get(item.el.structure_id) : undefined}
            showCells={(item.el.cell_cm ?? 100) * s >= 4}
          />
        );
      case 'pedestal':
        return (
          <PedestalElement
            key={item.ref}
            refId={item.ref}
            el={item.el}
            state={state}
            interactive={isInteractive}
            label={label}
          />
        );
      case 'decoration':
        return (
          <DecorationElement
            key={item.ref}
            {...common}
            el={item.el}
            kitPrefix={uid}
            locale={locale}
          />
        );
    }
  };

  const stateOf = (ref: string): ElementState =>
    selected?.has(ref) ? 'selected' : issues?.has(ref) ? 'issue' : 'normal';

  return (
    <svg
      ref={svgRef}
      viewBox={viewBoxAttr(viewport)}
      width="100%"
      height="100%"
      className={`touch-none select-none ${MAP_TOKENS.canvas} ${className ?? ''}`}
      data-px-per-cm={s}
      {...svgProps}
      onPointerDown={(e) => {
        const ref = findRef(e.target);
        if (ref && onElementPointerDown) onElementPointerDown(ref, e);
        else onBackgroundPointerDown?.(e);
        svgProps.onPointerDown?.(e);
      }}
      onKeyDown={(e) => {
        const ref = findRef(e.target);
        if (ref && onElementActivate && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault();
          onElementActivate(ref, e);
        }
        svgProps.onKeyDown?.(e);
      }}
    >
      <KitDefs prefix={uid} assets={kitAssets} />
      {lensStyles ? <GlyphDefs prefix={uid} glyphs={glyphs} /> : null}
      <rect
        x={0}
        y={0}
        width={meta.width_cm}
        height={meta.height_cm}
        className={MAP_TOKENS.mapFrame}
        strokeWidth={1}
        vectorEffect="non-scaling-stroke"
        data-testid="map-frame"
      />
      {layers.background && meta.background && backgroundUrl ? (
        <Background bg={meta.background} href={backgroundUrl} />
      ) : null}
      {layers.water_land ? (
        <WaterLand water={meta.water_polygons} land={meta.land_polygons} />
      ) : null}
      {layers.grid ? (
        <Grid
          id={`${uid}-grid`}
          grid={meta.grid_cm}
          width={meta.width_cm}
          height={meta.height_cm}
          vp={viewport}
        />
      ) : null}
      {layers.concession && meta.concession_outline ? (
        <ConcessionOutline outline={meta.concession_outline} />
      ) : null}
      <g data-layer="elements">
        {visible.map((item) => render(item, stateOf(item.ref), interactive))}
      </g>
      {visibleVessels.length > 0 ? (
        <g data-layer="vessels">
          {visibleVessels.map((v) => (
            <Vessel
              key={v.key}
              item={v}
              token={lensStyles?.get(v.ref)?.colorToken ?? 'muted'}
              dimmed={dimmed?.has(v.ref) ?? false}
              selected={selected?.has(v.ref) ?? false}
              showName={v.beam * s >= 14}
              pxPerCm={s}
              label={v.interactive && vesselLabel ? vesselLabel(v) : ''}
            />
          ))}
        </g>
      ) : null}
      {preview && preview.length > 0 ? (
        <g data-layer="preview">{preview.map((item) => render(item, 'preview', false))}</g>
      ) : null}
      {children}
      {hideChrome ? null : (
        <>
          <ScaleBar vp={viewport} locale={locale} />
          <NorthArrow vp={viewport} />
        </>
      )}
    </svg>
  );
}
