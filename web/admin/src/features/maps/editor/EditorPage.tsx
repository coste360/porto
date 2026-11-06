import { getRouteApi, Link, useBlocker } from '@tanstack/react-router';
import {
  ArrowLeftIcon,
  ImageIcon,
  LayersIcon,
  ListIcon,
  ListChecksIcon,
  MaximizeIcon,
  SlidersHorizontalIcon,
  WandSparklesIcon,
  ZoomInIcon,
  ZoomOutIcon,
  type LucideIcon,
} from 'lucide-react';
import {
  useEffect,
  useEffectEvent,
  useMemo,
  useReducer,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
} from 'react';
import { toast } from 'sonner';

import {
  fetchMap,
  generateMapElements,
  isProblem,
  mapBackgroundUrl,
  toProblem,
  useApi,
  useBerthGroups,
  useHarborSettings,
  useMap,
  useSaveMap,
  type GenerateResult,
  type MapDocument,
  type MapValidationReport,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  DEFAULT_LAYERS,
  HarborMap,
  KIT_ASSETS,
  boxCentre,
  entriesOf,
  entryBounds,
  expandBox,
  focusBox,
  indexScene,
  pxPerCm,
  refOf,
  fitToView,
  useMapViewport,
  zoomPercent,
  type KitAssetId,
  type LayerVisibility,
  type Pt,
  type SceneEntry,
  type SceneItem,
} from '@porto/shared/map';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Label } from '@porto/shared/ui/label';
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@porto/shared/ui/resizable';
import { ScrollArea } from '@porto/shared/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';
import { Tooltip, TooltipContent, TooltipTrigger } from '@porto/shared/ui/tooltip';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';
import { LayerSwitches } from '../LayerToggles';
import { localName } from '../lib';
import { StructureList } from '../StructureList';
import { useElementLabel } from '../useElementLabel';
import { CanvasOverlay, CursorReadout } from './CanvasOverlay';
import { BackgroundPanel, type BackgroundPick } from './dialogs/BackgroundPanel';
import { ConflictDialog } from './dialogs/ConflictDialog';
import { GeneratorPanel, type PickRequest } from './dialogs/GeneratorPanel';
import {
  buildGenerator,
  generatorDefaults,
  type GeneratorType,
  type GeneratorValues,
} from './dialogs/generators';
import { NumericMoveDialog } from './dialogs/NumericMoveDialog';
import { RenumberDialog } from './dialogs/RenumberDialog';
import { DraftRestoreDialog, UnsavedChangesDialog } from './dialogs/UnsavedChangesDialog';
import {
  addCommand,
  apply,
  batch,
  metaCommand,
  removeCommand,
  type Command,
} from './model/commands';
import {
  boundsOfRefs,
  countsOf,
  createSceneBuilder,
  fromApi,
  toSaveRequest,
  type EditorDocument,
} from './model/document';
import { clearDrafts, loadDraft, saveDraft, type Draft } from './model/draft';
import { historyShortcut } from './model/history';
import { newGeneratorPrefix } from './model/keys';
import { createPointerStore } from './model/pointerStore';
import {
  allOfKind,
  byStructure,
  emptySelection,
  marquee,
  single,
  toggle,
  type Selection,
} from './model/selection';
import {
  DEFAULT_SNAP,
  snapDelta,
  snapPoint,
  type SnapOptions,
  type SnapResult,
} from './model/snap';
import { editorReducer, initialEditorState, isDirty } from './model/state';
import {
  NUDGE_BIG_CM,
  NUDGE_CM,
  ROTATE_BIG_DD,
  ROTATE_STEP_DD,
  alignCommand,
  distributeCommand,
  duplicateCommand,
  mirrorCommand,
  moveCommand,
  pierAxisOf,
  rotateCommand,
  selectionCentre,
} from './model/transform';
import { useLiveValidation } from './model/validation';
import { PropertiesPanel } from './panels/PropertiesPanel';
import { Toolbar } from './panels/Toolbar';
import { ValidationPanel } from './panels/ValidationPanel';
import { POLYGON_TARGETS, TOOLS, toolForKey, type PolygonTarget, type ToolId } from './tools';
import { createFromClicks, type ToolOptions } from './tools/create';

const route = getRouteApi('/auth/m/$marinaId/maps/$mapId/edit');

/** Snap tolerance in screen pixels. */
const SNAP_PX = 10;

type Tab = 'properties' | 'generator' | 'background' | 'validation' | 'layers' | 'list';

/** Side panel tabs: icons with a tooltip and a screen-reader label, so the six tabs fit on one row. */
const PANEL_TABS: readonly (readonly [Tab, LucideIcon])[] = [
  ['properties', SlidersHorizontalIcon],
  ['generator', WandSparklesIcon],
  ['background', ImageIcon],
  ['validation', ListChecksIcon],
  ['layers', LayersIcon],
  ['list', ListIcon],
];

type Drag =
  | {
      kind: 'move';
      start: Pt;
      refs: Selection;
      origin: EditorDocument;
      single: string | null;
      cmd: Command | null;
    }
  | { kind: 'marquee'; start: Pt; add: boolean; moved: boolean }
  | {
      kind: 'rotate';
      centre: Pt;
      startDd: number;
      refs: Selection;
      origin: EditorDocument;
      cmd: Command | null;
    };

type Pick =
  { target: 'generator'; req: PickRequest } | { target: 'background'; pick: BackgroundPick };

/** The map editor (spec 8, m1-plan §5.3), harbor.configure only. */
export function EditorPage() {
  const { mapId } = route.useParams();
  const { marinaId, can } = useCurrentMarina();
  const query = useMap(marinaId, mapId);
  return (
    <RequirePermission allowed={can('harbor.configure')}>
      {query.isPending ? (
        <LoadingState />
      ) : query.isError ? (
        <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <EditorWorkspace key={mapId} marinaId={marinaId} initial={query.data} />
      )}
    </RequirePermission>
  );
}

function angleDd(c: Pt, p: Pt): number {
  return Math.round((Math.atan2(p.y - c.y, p.x - c.x) * 1800) / Math.PI);
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.closest('[role="dialog"],[role="alertdialog"],[role="listbox"],[role="menu"]'))
    return true;
  return target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName);
}

function EditorWorkspace({ marinaId, initial }: { marinaId: string; initial: MapDocument }) {
  const { t, i18n } = useT();
  const api = useApi();
  const mapId = initial.id;
  const [state, dispatch] = useReducer(editorReducer, initial, (doc) => ({
    ...initialEditorState,
    doc: fromApi(doc),
  }));
  const doc = state.doc!;
  const shown = state.transient ?? doc;
  const dirty = isDirty(state);
  const groups = useBerthGroups(marinaId).data ?? [];
  const settings = useHarborSettings(marinaId).data;
  const save = useSaveMap(marinaId, mapId);
  const labelFor = useElementLabel();

  const [builder] = useState(createSceneBuilder);
  const items = useMemo(() => builder(shown), [builder, shown]);
  const index = useMemo(() => indexScene(items), [items]);
  const content = useMemo(
    () => ({ minX: 0, minY: 0, maxX: shown.meta.width_cm, maxY: shown.meta.height_cm }),
    [shown.meta.width_cm, shown.meta.height_cm],
  );
  const vp = useMapViewport(content);
  const s = pxPerCm(vp.viewport);
  const [store] = useState(createPointerStore);

  const [tool, setToolState] = useState<ToolId>('select');
  const [pending, setPending] = useState<SnapResult[]>([]);
  const [snapOptions, setSnapOptions] = useState<SnapOptions>(DEFAULT_SNAP);
  const [layers, setLayers] = useState<LayerVisibility>({ ...DEFAULT_LAYERS, grid: true });
  const [toolOptions, setToolOptions] = useState<Omit<ToolOptions, 'textLabel'>>({
    polygonTarget: 'water',
    landKind: 'parking',
    asset: 'harbour_office',
  });
  const [measure, setMeasure] = useState<[Pt, Pt] | null>(null);
  const [calibration, setCalibration] = useState<[Pt, Pt] | null>(null);
  const [tab, setTab] = useState<Tab>('properties');
  const [pick, setPick] = useState<Pick | null>(null);
  const [saveReport, setSaveReport] = useState<MapValidationReport | null>(null);
  const [conflict, setConflict] = useState<{ server: MapDocument | null } | null>(null);
  const [renumberOpen, setRenumberOpen] = useState(false);
  const [numericOpen, setNumericOpen] = useState(false);
  const [draft, setDraft] = useState<Draft | null>(() => loadDraft(mapId, initial.version));
  const [gen, setGen] = useState<{
    type: GeneratorType;
    values: GeneratorValues;
    preview: GenerateResult | null;
    prefix: string;
    invalid: string[];
    pending: boolean;
    error: unknown;
  }>({
    type: 'pier',
    values: generatorDefaults('pier'),
    preview: null,
    prefix: '',
    invalid: [],
    pending: false,
    error: null,
  });
  const drag = useRef<Drag | null>(null);

  const validation = useLiveValidation({
    marinaId,
    mapId,
    doc,
    enabled: true,
    override: saveReport,
  });
  const issueRefs = useMemo(() => {
    const out = new Set<string>();
    for (const i of validation.report?.errors ?? []) for (const r of i.refs) out.add(r);
    return out;
  }, [validation.report]);

  const previewItems = useMemo<SceneItem[]>(() => {
    if (!gen.preview) return [];
    const structures = new Map<string, Extract<SceneEntry, { kind: 'structure' }>['el']>();
    for (const [ref, e] of doc.elements) if (e.kind === 'structure') structures.set(ref, e.el);
    const entries = entriesOf(gen.preview.elements);
    for (const e of entries) if (e.kind === 'structure') structures.set(refOf(e.el), e.el);
    return entries.map(
      (e) => ({ ...e, ref: refOf(e.el), box: entryBounds(e, structures) }) as SceneItem,
    );
  }, [gen.preview, doc]);

  const counts = countsOf(doc);

  // ---- commands ----

  const run = (command: Command, select?: Selection) => {
    setSaveReport(null);
    dispatch({ type: 'execute', command, ...(select ? { select } : {}) });
  };
  const select = (selection: Selection) => dispatch({ type: 'select', selection });
  const history = (type: 'undo' | 'redo') => {
    setSaveReport(null);
    dispatch({ type });
  };

  const setTool = (next: ToolId) => {
    setToolState(next);
    setPending([]);
    setMeasure(null);
    store.set({ hover: null });
  };

  const snapCtx = (alt: boolean, exclude?: Selection) => ({
    items,
    gridCm: doc.meta.grid_cm,
    toleranceCm: SNAP_PX / s,
    options: snapOptions,
    disabled: alt,
    ...(exclude ? { exclude } : {}),
  });

  const focusRefs = (refs: Iterable<string>) => {
    const set = new Set(refs);
    const box = boundsOfRefs(items, set);
    if (box) vp.setViewport((v) => focusBox(v, expandBox(box, 500)));
  };

  const pickIssue = (refs: string[]) => {
    const present = refs.filter((r) => doc.elements.has(r));
    select(new Set(present));
    focusRefs(present);
  };

  const codeOf = (ref: string) => {
    const e = doc.elements.get(ref);
    return e && 'code' in e.el && typeof e.el.code === 'string' ? e.el.code : ref;
  };

  const deleteSelection = () => {
    if (state.selection.size === 0) return;
    run(removeCommand(doc, state.selection), emptySelection);
  };

  const duplicateSelection = () => {
    const { command, newRefs } = duplicateCommand(doc, items, state.selection);
    run(command, new Set(newRefs));
  };

  const rotateSelection = (dd: number) => {
    const c = selectionCentre(items, state.selection);
    if (c) run(rotateCommand(doc, state.selection, dd, c));
  };

  const mirrorSelection = () => {
    const axis = pierAxisOf(doc, state.selection);
    const c = selectionCentre(items, state.selection);
    if (axis) run(mirrorCommand(doc, state.selection, axis[0], axis[1]));
    else if (c) run(mirrorCommand(doc, state.selection, c, { x: c.x, y: c.y + 100 }));
  };

  const selectStructure = () => {
    for (const ref of state.selection) {
      const e = doc.elements.get(ref);
      const sref =
        e?.kind === 'structure' ? ref : e?.kind === 'berth' ? e.el.structure_id : undefined;
      if (sref) {
        select(byStructure(doc, sref));
        return;
      }
    }
  };

  // ---- save ----

  const doSave = async (version: number) => {
    try {
      const result = await save.mutateAsync({ version, body: toSaveRequest(doc) });
      dispatch({ type: 'load', doc: fromApi(result.document), idMap: result.id_map });
      clearDrafts(mapId);
      setConflict(null);
      setGen((g) => ({ ...g, preview: null }));
      toast.success(
        result.warnings.length > 0
          ? t('editor:save.doneWithWarnings', { count: result.warnings.length })
          : t('editor:save.done'),
      );
    } catch (err) {
      if (isProblem(err, 'version_conflict')) {
        setConflict({ server: null });
        try {
          setConflict({ server: await fetchMap(api, marinaId, mapId) });
        } catch (e2) {
          toastProblem(t, e2);
        }
      } else if (isProblem(err, 'map_invalid')) {
        const issues = toProblem(err).issues;
        setSaveReport({
          errors: issues.filter((i) => i.severity === 'error'),
          warnings: issues.filter((i) => i.severity === 'warning'),
        });
        setTab('validation');
        toast.error(t('errors:map_invalid'));
      } else {
        toastProblem(t, err);
      }
    }
  };

  // ---- generator ----

  const openGenerator = (type: GeneratorType) => {
    const values = generatorDefaults(type);
    setGen({ type, values, preview: null, prefix: '', invalid: [], pending: false, error: null });
    setTab('generator');
  };

  const previewGenerator = async () => {
    const prefix = newGeneratorPrefix(doc);
    const built = buildGenerator(gen.type, gen.values, prefix);
    if ('errors' in built) {
      setGen((g) => ({ ...g, invalid: built.errors }));
      return;
    }
    setGen((g) => ({ ...g, invalid: [], pending: true, error: null }));
    try {
      const preview = await generateMapElements(api, marinaId, mapId, {
        generator: built.params,
        document: toSaveRequest(doc),
      });
      setGen((g) => ({ ...g, preview, prefix, pending: false }));
    } catch (err) {
      setGen((g) => ({ ...g, preview: null, pending: false, error: err }));
    }
  };

  const insertGenerated = () => {
    if (!gen.preview) return;
    const entries = entriesOf(gen.preview.elements);
    run(addCommand('editor:commands.generate', entries), new Set(entries.map((e) => refOf(e.el))));
    setGen((g) => ({ ...g, preview: null }));
  };

  // ---- canvas ----

  const finishClicks = (clicks: SnapResult[]) => {
    if (pick?.target === 'generator' && pick.req.mode === 'polygon') {
      if (clicks.length >= 3)
        setGen((g) => ({
          ...g,
          values: { ...g.values, [pick.req.field]: clicks.map((c) => c.point) },
          preview: null,
        }));
      setPick(null);
      setPending([]);
      return;
    }
    const cmd = createFromClicks(tool, clicks, doc, {
      ...toolOptions,
      textLabel: t('editor:defaults.text'),
    });
    setPending([]);
    if (!cmd) {
      if (tool === 'finger' || tool === 'linear_area') toast.info(t(`editor:hints.${tool}`));
      return;
    }
    const added = cmd.changes.filter((c) => !c.before && c.after).map((c) => c.ref);
    run(cmd, added.length > 0 ? new Set(added) : undefined);
  };

  const canvasClick = (e: ReactPointerEvent<SVGSVGElement>) => {
    const p = vp.mapPoint(e);
    const snap = snapPoint(p, snapCtx(e.altKey));
    if (pick) {
      if (pick.target === 'generator') {
        if (pick.req.mode === 'polygon') {
          setPending((cur) => [...cur, snap]);
          return;
        }
        setGen((g) => {
          const values = { ...g.values, [pick.req.field]: snap.point };
          if (
            pick.req.field === 'origin' &&
            snap.kind === 'edge' &&
            snap.rotation_dd !== undefined
          ) {
            values.rotation_dd = snap.rotation_dd;
            if (g.type === 'pier' && snap.structureRef) values.root_id = snap.structureRef;
          }
          return { ...g, values, preview: null };
        });
        setPick(null);
        return;
      }
      if (pick.pick.kind === 'calibrate') {
        const clicks = [...pending, snap];
        if (clicks.length >= 2) {
          setCalibration([clicks[0]!.point, clicks[1]!.point]);
          setPending([]);
          setPick(null);
        } else setPending(clicks);
        return;
      }
      const i = pick.pick.index;
      const points =
        doc.meta.georef?.points ?? [0, 1, 2].map(() => ({ x: 0, y: 0, lat_e7: 0, lon_e7: 0 }));
      run(
        metaCommand(
          doc,
          { georef: { points: points.map((cp, j) => (j === i ? { ...cp, ...snap.point } : cp)) } },
          'editor:commands.georef',
        ),
      );
      setPick(null);
      return;
    }
    const def = TOOLS[tool];
    const clicks = [...pending, snap];
    if (tool === 'measure' || tool === 'calibrate') {
      if (clicks.length >= 2) {
        const seg: [Pt, Pt] = [clicks[0]!.point, clicks[1]!.point];
        if (tool === 'measure') setMeasure(seg);
        else {
          setCalibration(seg);
          setTab('background');
        }
        setPending([]);
      } else {
        setMeasure(null);
        setPending(clicks);
      }
      return;
    }
    if (def.clicks === 'one' || (def.clicks === 'two' && clicks.length >= 2)) finishClicks(clicks);
    else setPending(clicks);
  };

  const drawing = tool !== 'select' && tool !== 'pan';

  const onElementPointerDown = (ref: string, e: ReactPointerEvent<SVGSVGElement>) => {
    if (pick || drawing) {
      canvasClick(e);
      return;
    }
    if (tool === 'pan' || e.button === 1) {
      vp.gestures.onPointerDown(e, true);
      return;
    }
    if (e.button !== 0) return;
    if (e.shiftKey) {
      select(toggle(state.selection, ref));
      return;
    }
    const sel = state.selection.has(ref) ? state.selection : single(ref);
    if (sel !== state.selection) select(sel);
    const entry = doc.elements.get(ref);
    drag.current = {
      kind: 'move',
      start: vp.mapPoint(e),
      refs: sel,
      origin: doc,
      single:
        sel.size === 1 && entry?.kind === 'berth' && entry.el.mooring_type !== 'buoy' ? ref : null,
      cmd: null,
    };
    e.currentTarget.setPointerCapture?.(e.pointerId);
  };

  const onBackgroundPointerDown = (e: ReactPointerEvent<SVGSVGElement>) => {
    const target = e.target instanceof Element ? e.target : null;
    if (target?.closest('[data-handle="rotate"]')) {
      const c = selectionCentre(items, state.selection);
      if (!c) return;
      drag.current = {
        kind: 'rotate',
        centre: c,
        startDd: angleDd(c, vp.mapPoint(e)),
        refs: state.selection,
        origin: doc,
        cmd: null,
      };
      e.currentTarget.setPointerCapture?.(e.pointerId);
      return;
    }
    if (tool === 'pan' || e.button === 1) {
      vp.gestures.onPointerDown(e, true);
      return;
    }
    if (e.button !== 0) return;
    if (pick || drawing) {
      canvasClick(e);
      return;
    }
    drag.current = { kind: 'marquee', start: vp.mapPoint(e), add: e.shiftKey, moved: false };
    e.currentTarget.setPointerCapture?.(e.pointerId);
  };

  const onPointerMove = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (vp.gestures.onPointerMove(e)) return;
    const p = vp.mapPoint(e);
    const d = drag.current;
    if (!d) {
      store.set({ cursor: p, hover: drawing || pick ? snapPoint(p, snapCtx(e.altKey)) : null });
      return;
    }
    store.set({ cursor: p });
    if (d.kind === 'marquee') {
      const box = {
        minX: Math.min(d.start.x, p.x),
        minY: Math.min(d.start.y, p.y),
        maxX: Math.max(d.start.x, p.x),
        maxY: Math.max(d.start.y, p.y),
      };
      d.moved = d.moved || Math.hypot(p.x - d.start.x, p.y - d.start.y) * s > 4;
      store.set({ marquee: d.moved ? box : null });
      return;
    }
    if (d.kind === 'rotate') {
      let dd = angleDd(d.centre, p) - d.startDd;
      const step = e.shiftKey ? ROTATE_BIG_DD : ROTATE_STEP_DD;
      dd = Math.round(dd / step) * step;
      d.cmd = rotateCommand(d.origin, d.refs, dd, d.centre);
      dispatch({ type: 'transient', doc: apply(d.origin, d.cmd) });
      return;
    }
    const dx = p.x - d.start.x;
    const dy = p.y - d.start.y;
    if (d.single) {
      const entry = d.origin.elements.get(d.single);
      if (entry?.kind === 'berth') {
        const target = { x: entry.el.anchor.x + dx, y: entry.el.anchor.y + dy };
        const snap = snapPoint(target, snapCtx(e.altKey, d.refs));
        const el = { ...entry.el, anchor: snap.point };
        if (snap.kind === 'edge' && snap.rotation_dd !== undefined) {
          el.rotation_dd = snap.rotation_dd;
          if (snap.structureRef) el.structure_id = snap.structureRef;
          if (snap.side) el.side = snap.side;
        }
        d.cmd = {
          label: 'editor:commands.move',
          changes: [{ ref: d.single, before: entry, after: { kind: 'berth', el } }],
        };
        dispatch({ type: 'transient', doc: apply(d.origin, d.cmd) });
        return;
      }
    }
    const delta = snapDelta(dx, dy, d.origin.meta.grid_cm, e.altKey || !snapOptions.grid);
    d.cmd = moveCommand(d.origin, d.refs, delta.x, delta.y);
    dispatch({ type: 'transient', doc: apply(d.origin, d.cmd) });
  };

  const onPointerUp = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (vp.gestures.onPointerUp(e)) return;
    const d = drag.current;
    drag.current = null;
    if (!d) return;
    if (d.kind === 'marquee') {
      const box = store.get().marquee;
      store.set({ marquee: null });
      if (d.moved && box) select(marquee(items, box, state.selection, d.add));
      else if (!d.add) select(emptySelection);
      return;
    }
    if (d.cmd && d.cmd.changes.length > 0) dispatch({ type: 'execute', command: d.cmd });
    else dispatch({ type: 'transient', doc: null });
  };

  // ---- keyboard ----

  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (isTypingTarget(e.target)) return;
    const h = historyShortcut(e);
    if (h) {
      e.preventDefault();
      history(h);
      return;
    }
    const mod = e.ctrlKey || e.metaKey;
    const key = e.key;
    if (mod && key.toLowerCase() === 's') {
      e.preventDefault();
      if (dirty && !save.isPending) void doSave(doc.version);
      return;
    }
    if (mod && key.toLowerCase() === 'a') {
      e.preventDefault();
      select(new Set(doc.elements.keys()));
      return;
    }
    if (mod && key.toLowerCase() === 'd') {
      e.preventDefault();
      duplicateSelection();
      return;
    }
    if (mod || e.altKey) return;
    if (key === 'Escape') {
      if (pending.length > 0 || pick) {
        setPending([]);
        setPick(null);
      } else if (tool !== 'select') setTool('select');
      else select(emptySelection);
      return;
    }
    if (key === 'Enter') {
      const polygonPick = pick?.target === 'generator' && pick.req.mode === 'polygon';
      if (
        polygonPick ||
        (TOOLS[tool].clicks === 'many' && pending.length >= TOOLS[tool].minPoints)
      ) {
        e.preventDefault();
        finishClicks(pending);
      }
      return;
    }
    if (key === 'Delete' || key === 'Backspace') {
      e.preventDefault();
      if (pending.length > 0) setPending((p) => p.slice(0, -1));
      else deleteSelection();
      return;
    }
    if (key.startsWith('Arrow') && state.selection.size > 0) {
      e.preventDefault();
      const step = e.shiftKey ? NUDGE_BIG_CM : NUDGE_CM;
      const dx = key === 'ArrowLeft' ? -step : key === 'ArrowRight' ? step : 0;
      const dy = key === 'ArrowUp' ? -step : key === 'ArrowDown' ? step : 0;
      run(moveCommand(doc, state.selection, dx, dy));
      return;
    }
    if ((key === '[' || key === ']' || key === '{' || key === '}') && state.selection.size > 0) {
      const step = e.shiftKey ? ROTATE_BIG_DD : ROTATE_STEP_DD;
      rotateSelection(key === '[' || key === '{' ? -step : step);
      return;
    }
    const next = key.length === 1 ? toolForKey(key) : null;
    if (next) setTool(next);
  });

  useEffect(() => {
    const handler = (e: KeyboardEvent) => onKey(e);
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  // ---- local draft, unsaved-changes guard ----

  useEffect(() => {
    if (!dirty) return;
    const id = setTimeout(() => saveDraft(mapId, doc.version, toSaveRequest(doc)), 1000);
    return () => clearTimeout(id);
  }, [dirty, doc, mapId]);

  const blocker = useBlocker({
    shouldBlockFn: () => dirty,
    withResolver: true,
    enableBeforeUnload: () => dirty,
  });

  const selectionBox =
    state.selection.size > 0 && tool === 'select' ? boundsOfRefs(items, state.selection) : null;
  const fitted = fitToView(content, vp.viewport.screenW, vp.viewport.screenH);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" size="sm" asChild>
          <Link to="/m/$marinaId/maps/$mapId" params={{ marinaId, mapId }}>
            <ArrowLeftIcon />
            {t('editor:backToViewer')}
          </Link>
        </Button>
        <h1 className="text-lg font-semibold">
          {t('editor:titleFor', { name: localName(doc.meta.name, i18n.language) })}
        </h1>
      </div>
      <div className="rounded-md border">
        <Toolbar
          tool={tool}
          onTool={setTool}
          canUndo={state.history.past.length > 0}
          canRedo={state.history.future.length > 0}
          onUndo={() => history('undo')}
          onRedo={() => history('redo')}
          selectionCount={state.selection.size}
          onGenerator={openGenerator}
          onAlign={(m) => run(alignCommand(doc, items, state.selection, m))}
          onDistribute={(axis) => run(distributeCommand(doc, items, state.selection, axis))}
          onDuplicate={duplicateSelection}
          onMirror={mirrorSelection}
          onRotate={rotateSelection}
          onNumericMove={() => setNumericOpen(true)}
          onRenumber={() => setRenumberOpen(true)}
          onSelectStructure={selectStructure}
          onDelete={deleteSelection}
          snap={snapOptions}
          onSnap={setSnapOptions}
          dirty={dirty}
          saving={save.isPending}
          onSave={() => void doSave(doc.version)}
        />
        <ToolOptionsBar
          tool={tool}
          options={toolOptions}
          onChange={setToolOptions}
          pending={pending.length}
          pick={pick}
        />
        <ResizablePanelGroup orientation="horizontal" className="min-h-160">
          <ResizablePanel defaultSize="72%" minSize="40%">
            <div className="relative flex h-full flex-col">
              <div className="absolute top-2 right-2 z-10 flex gap-1">
                <Button
                  variant="secondary"
                  size="icon"
                  onClick={vp.fit}
                  aria-label={t('maps:viewer.fit')}
                >
                  <MaximizeIcon />
                </Button>
                <Button
                  variant="secondary"
                  size="icon"
                  onClick={() => vp.zoomBy(1.25)}
                  aria-label={t('maps:viewer.zoomIn')}
                >
                  <ZoomInIcon />
                </Button>
                <Button
                  variant="secondary"
                  size="icon"
                  onClick={() => vp.zoomBy(0.8)}
                  aria-label={t('maps:viewer.zoomOut')}
                >
                  <ZoomOutIcon />
                </Button>
              </div>
              <div
                ref={vp.bindContainer}
                className="bg-muted h-160 flex-1 overflow-hidden"
                data-tool={tool}
              >
                <HarborMap
                  svgRef={vp.bindSvg}
                  meta={shown.meta}
                  scene={items}
                  index={index}
                  viewport={vp.viewport}
                  layers={layers}
                  selected={state.selection}
                  issues={issueRefs}
                  preview={previewItems}
                  backgroundUrl={
                    shown.meta.background
                      ? mapBackgroundUrl(marinaId, mapId, shown.meta.background.file_id)
                      : undefined
                  }
                  interactive
                  labelFor={labelFor}
                  aria-label={t('editor:canvas')}
                  role="application"
                  data-testid="editor-canvas"
                  className={
                    drawing || pick
                      ? 'cursor-crosshair'
                      : tool === 'pan'
                        ? 'cursor-grab'
                        : undefined
                  }
                  onElementPointerDown={onElementPointerDown}
                  onBackgroundPointerDown={onBackgroundPointerDown}
                  onElementActivate={(ref, e) =>
                    select(e.shiftKey ? toggle(state.selection, ref) : single(ref))
                  }
                  onPointerMove={onPointerMove}
                  onPointerUp={onPointerUp}
                  onPointerCancel={onPointerUp}
                  onPointerLeave={() => store.set({ cursor: null, hover: null })}
                  onDoubleClick={() => {
                    if (
                      TOOLS[tool].clicks === 'many' ||
                      (pick?.target === 'generator' && pick.req.mode === 'polygon')
                    )
                      finishClicks(pending);
                  }}
                >
                  <CanvasOverlay
                    store={store}
                    pxPerCm={s}
                    pending={pending}
                    drawing={drawing || !!pick}
                    selectionBox={selectionBox}
                    rotateHandle={tool === 'select' && state.selection.size > 0}
                    measure={measure}
                    calibration={calibration}
                  />
                </HarborMap>
              </div>
              <div
                className="text-muted-foreground flex flex-wrap items-center gap-4 border-t px-3 py-1 text-xs"
                role="status"
                data-testid="editor-status"
              >
                <CursorReadout store={store} />
                <span>{t('editor:status.zoom', { value: zoomPercent(vp.viewport, fitted) })}</span>
                <span>{t('editor:status.selection', { count: state.selection.size })}</span>
                <span data-testid="berth-count">
                  {t('maps:counts.berths', { count: counts.berths })}
                </span>
                <span>{t('maps:counts.structures', { count: counts.structures })}</span>
                {dirty ? <Badge variant="secondary">{t('editor:status.unsaved')}</Badge> : null}
              </div>
            </div>
          </ResizablePanel>
          <ResizableHandle withHandle />
          <ResizablePanel defaultSize="28%" minSize="20%">
            <Tabs
              value={tab}
              onValueChange={(v) => setTab(v as Tab)}
              className="flex h-full flex-col"
            >
              <TabsList className="m-2 grid w-auto grid-cols-6">
                {PANEL_TABS.map(([id, Icon]) => {
                  const errors = id === 'validation' ? (validation.report?.errors.length ?? 0) : 0;
                  return (
                    <Tooltip key={id}>
                      <TooltipTrigger asChild>
                        <TabsTrigger value={id} className="min-w-0 px-1">
                          <Icon aria-hidden />
                          <span className="sr-only">{t(`editor:tabs.${id}`)}</span>
                          {errors > 0 ? (
                            <Badge variant="destructive" className="px-1">
                              {errors}
                            </Badge>
                          ) : null}
                        </TabsTrigger>
                      </TooltipTrigger>
                      <TooltipContent>{t(`editor:tabs.${id}`)}</TooltipContent>
                    </Tooltip>
                  );
                })}
              </TabsList>
              <ScrollArea className="h-160 px-3 pb-3">
                <TabsContent value="properties">
                  <PropertiesPanel
                    doc={doc}
                    selection={state.selection}
                    groups={groups}
                    settings={settings}
                    onCommand={(c) => run(c)}
                  />
                </TabsContent>
                <TabsContent value="generator">
                  <GeneratorPanel
                    type={gen.type}
                    values={gen.values}
                    onChange={(values) => setGen((g) => ({ ...g, values, preview: null }))}
                    invalid={gen.invalid}
                    doc={doc}
                    groups={groups}
                    picking={pick?.target === 'generator' ? pick.req : null}
                    onPick={(req) => {
                      setPending([]);
                      setPick(req ? { target: 'generator', req } : null);
                    }}
                    preview={gen.preview}
                    pending={gen.pending}
                    error={gen.error}
                    onPreview={() => void previewGenerator()}
                    onInsert={insertGenerated}
                    onDiscard={() => setGen((g) => ({ ...g, preview: null }))}
                  />
                </TabsContent>
                <TabsContent value="background">
                  <BackgroundPanel
                    marinaId={marinaId}
                    doc={doc}
                    onCommand={(c) => run(c)}
                    calibrationPoints={calibration}
                    onClearCalibration={() => setCalibration(null)}
                    picking={pick?.target === 'background' ? pick.pick : null}
                    onPick={(p) => {
                      setPending([]);
                      setPick(p ? { target: 'background', pick: p } : null);
                    }}
                  />
                </TabsContent>
                <TabsContent value="validation">
                  <ValidationPanel
                    report={validation.report}
                    status={validation.status}
                    codeOf={codeOf}
                    onPick={pickIssue}
                  />
                </TabsContent>
                <TabsContent value="layers">
                  <LayerSwitches layers={layers} onChange={setLayers} />
                </TabsContent>
                <TabsContent value="list">
                  <div className="mb-2 flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => select(allOfKind(doc, 'berth'))}
                    >
                      {t('editor:actions.selectAllBerths')}
                    </Button>
                    {groups.map((g) => (
                      <Button
                        key={g.id}
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          const sel = new Set<string>();
                          for (const [ref, e] of doc.elements) {
                            if (
                              (e.kind === 'berth' || e.kind === 'area' || e.kind === 'land') &&
                              e.el.group_ids.includes(g.id)
                            )
                              sel.add(ref);
                          }
                          select(sel);
                        }}
                      >
                        {t('editor:actions.selectGroup', { code: g.code })}
                      </Button>
                    ))}
                  </div>
                  <StructureList
                    items={items}
                    selected={state.selection}
                    onPick={(ref, additive) => {
                      const e = doc.elements.get(ref);
                      if (e?.kind === 'structure') select(byStructure(doc, ref));
                      else select(additive ? toggle(state.selection, ref) : single(ref));
                      const box = boundsOfRefs(items, new Set([ref]));
                      if (box)
                        vp.setViewport((v) => ({
                          ...v,
                          x: boxCentre(box).x - v.w / 2,
                          y: boxCentre(box).y - v.h / 2,
                        }));
                    }}
                  />
                </TabsContent>
              </ScrollArea>
            </Tabs>
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>

      <RenumberDialog
        open={renumberOpen}
        onOpenChange={setRenumberOpen}
        doc={doc}
        selection={state.selection}
        onCommand={(c) => run(c)}
      />
      <NumericMoveDialog
        open={numericOpen}
        onOpenChange={setNumericOpen}
        onApply={(dx, dy, dd) => {
          const c = selectionCentre(items, state.selection);
          const cmds = [moveCommand(doc, state.selection, dx, dy)];
          if (dd !== 0 && c)
            cmds.push(
              rotateCommand(apply(doc, cmds[0]!), state.selection, dd, {
                x: c.x + dx,
                y: c.y + dy,
              }),
            );
          run(batch('editor:commands.numericMove', cmds));
        }}
      />
      <ConflictDialog
        open={conflict !== null}
        local={doc}
        server={conflict?.server ?? null}
        onCancel={() => setConflict(null)}
        onReload={() => {
          if (conflict?.server) dispatch({ type: 'load', doc: fromApi(conflict.server) });
          clearDrafts(mapId);
          setConflict(null);
        }}
        onOverwrite={() => {
          if (conflict?.server) void doSave(conflict.server.version);
        }}
      />
      <UnsavedChangesDialog
        open={blocker.status === 'blocked'}
        onStay={() => blocker.reset?.()}
        onLeave={() => blocker.proceed?.()}
      />
      {draft ? (
        <DraftRestoreDialog
          open
          savedAt={draft.savedAt}
          onRestore={() => {
            const restored = fromApi({
              ...draft.body,
              id: mapId,
              version: initial.version,
              updated_at: initial.updated_at,
            });
            dispatch({ type: 'load', doc: restored, restored: true });
            setDraft(null);
          }}
          onDiscard={() => {
            clearDrafts(mapId);
            setDraft(null);
          }}
        />
      ) : null}
    </div>
  );
}

/** Options of the active tool (polygon target, land kind, kit asset) and the click hint. */
function ToolOptionsBar({
  tool,
  options,
  onChange,
  pending,
  pick,
}: {
  tool: ToolId;
  options: Omit<ToolOptions, 'textLabel'>;
  onChange: (o: Omit<ToolOptions, 'textLabel'>) => void;
  pending: number;
  pick: Pick | null;
}) {
  const { t } = useT();
  const hint = pick
    ? t(
        pick.target === 'generator' && pick.req.mode === 'polygon'
          ? 'editor:hints.pickPolygon'
          : 'editor:hints.pick',
      )
    : t(`editor:hints.tool.${tool}`);
  return (
    <div
      className="flex flex-wrap items-center gap-3 border-b px-3 py-1 text-sm"
      aria-live="polite"
    >
      <span className="text-muted-foreground" data-testid="tool-hint">
        {hint}
        {pending > 0 ? ` · ${t('editor:hints.points', { count: pending })}` : ''}
      </span>
      {tool === 'polygon' ? (
        <div className="flex items-center gap-2">
          <Label htmlFor="tool-polygon-target">{t('editor:toolOptions.polygonTarget')}</Label>
          <Select
            value={options.polygonTarget}
            onValueChange={(v) => onChange({ ...options, polygonTarget: v as PolygonTarget })}
          >
            <SelectTrigger id="tool-polygon-target" size="sm" className="w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {POLYGON_TARGETS.map((p) => (
                <SelectItem key={p} value={p}>
                  {t(`editor:polygonTargets.${p}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}
      {tool === 'land_resource' ? (
        <div className="flex items-center gap-2">
          <Label htmlFor="tool-land-kind">{t('editor:fields.kind')}</Label>
          <Select
            value={options.landKind}
            onValueChange={(v) => onChange({ ...options, landKind: v as ToolOptions['landKind'] })}
          >
            <SelectTrigger id="tool-land-kind" size="sm" className="w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(
                [
                  'parking',
                  'box',
                  'storeroom',
                  'locker',
                  'cradle',
                  'trailer_spot',
                  'custom',
                ] as const
              ).map((k) => (
                <SelectItem key={k} value={k}>
                  {t(`editor:landKinds.${k}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}
      {tool === 'decoration' ? (
        <div className="flex items-center gap-2">
          <Label htmlFor="tool-asset">{t('editor:fields.asset')}</Label>
          <Select
            value={options.asset}
            onValueChange={(v) => onChange({ ...options, asset: v as KitAssetId })}
          >
            <SelectTrigger id="tool-asset" size="sm" className="w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {KIT_ASSETS.map((a) => (
                <SelectItem key={a.id} value={a.id}>
                  {t(`kit:${a.labelKey}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}
    </div>
  );
}
