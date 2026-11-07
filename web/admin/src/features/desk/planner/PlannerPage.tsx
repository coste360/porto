import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { ChevronLeftIcon, ChevronRightIcon, PlusIcon } from 'lucide-react';
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';

import {
  useBerthGroups,
  useMap,
  useMaps,
  useMapOccupancy,
  type MapAllocation,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { ScrollArea, ScrollBar } from '@porto/shared/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { localName } from '../../maps/lib';
import { useMarinaToday } from '../../registry/lib/today';
import { addDays, daysBetween, nightsOf, weekdayOf } from '../lib/dates';
import {
  COL_PX,
  HEADER_PX,
  LABEL_PX,
  LANE_PX,
  PLANNER_SPANS,
  barFillClass,
  barRect,
  columnAt,
  dragColumns,
  isFree,
  layoutBars,
  moveForDrop,
  plannerRows,
  rowLayout,
  selectionStay,
  stayChangeForDrag,
  type Bar,
  type DragMode,
  type PlannerRow,
  type PlannerSpan,
} from '../lib/planner';
import { NewTransitDialog } from '../transits/NewTransitDialog';
import type { DeskPrefill } from '../public';
import { PlannerChangeDialog, type PlannerChange } from './PlannerChangeDialog';

const route = getRouteApi('/auth/m/$marinaId/planner');
const ALL = '__all__';
/** Pixels from the right edge of a bar that start a resize instead of a move. */
const RESIZE_HANDLE_PX = 8;

type Drag = {
  bar: Bar;
  rowIndex: number;
  mode: DragMode;
  x0: number;
  y0: number;
  dx: number;
  dy: number;
};

export function PlannerPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const search = route.useSearch();
  const navigate = useNavigate({ from: '/m/$marinaId/planner' });
  const maps = useMaps(marinaId);
  const mapId = search.map ?? maps.data?.[0]?.id ?? '';
  const from = search.from ?? today;
  const days: PlannerSpan = search.days ?? 14;
  const to = addDays(from, days - 1);
  const doc = useMap(marinaId, mapId);
  const groups = useBerthGroups(marinaId);
  const occupancy = useMapOccupancy(marinaId, mapId || undefined, from, to);
  const setSearch = (patch: Partial<typeof search>) =>
    void navigate({ search: (s) => ({ ...s, ...patch }), replace: true });

  return (
    <RequirePermission allowed={can('map.view')}>
      <PageHeader title={t('desk:planner.title')} description={t('desk:planner.description')} />
      <div className="mb-4 flex flex-col gap-3 lg:flex-row lg:flex-wrap lg:items-end">
        {(maps.data?.length ?? 0) > 1 ? (
          <Field className="lg:max-w-56">
            <FieldLabel htmlFor="planner-map">{t('desk:fields.map')}</FieldLabel>
            <Select value={mapId} onValueChange={(map) => setSearch({ map })}>
              <SelectTrigger id="planner-map" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(maps.data ?? []).map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {localName(m.name, i18n.language)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ) : null}
        <Field className="lg:max-w-56">
          <FieldLabel htmlFor="planner-from">{t('desk:planner.from')}</FieldLabel>
          <div className="flex gap-1">
            <Button
              variant="outline"
              size="icon"
              aria-label={t('desk:planner.previous')}
              onClick={() => setSearch({ from: addDays(from, -days) })}
            >
              <ChevronLeftIcon />
            </Button>
            <DatePicker
              id="planner-from"
              value={from}
              onChange={(d) => d && setSearch({ from: d })}
            />
            <Button
              variant="outline"
              size="icon"
              aria-label={t('desk:planner.next')}
              onClick={() => setSearch({ from: addDays(from, days) })}
            >
              <ChevronRightIcon />
            </Button>
          </div>
        </Field>
        <ToggleGroup
          type="single"
          variant="outline"
          value={String(days)}
          onValueChange={(v) => v && setSearch({ days: Number(v) as PlannerSpan })}
          aria-label={t('desk:planner.span')}
        >
          {PLANNER_SPANS.map((n) => (
            <ToggleGroupItem key={n} value={String(n)}>
              {t('desk:planner.days', { count: n })}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <Field className="lg:max-w-56">
          <FieldLabel htmlFor="planner-structure">{t('desk:planner.structure')}</FieldLabel>
          <Select
            value={search.structure ?? ALL}
            onValueChange={(v) => setSearch({ structure: v === ALL ? undefined : v })}
          >
            <SelectTrigger id="planner-structure" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('desk:filters.all')}</SelectItem>
              {(doc.data?.structures ?? []).flatMap((s) =>
                s.id
                  ? [
                      <SelectItem key={s.id} value={s.id}>
                        {s.name || s.code}
                      </SelectItem>,
                    ]
                  : [],
              )}
            </SelectContent>
          </Select>
        </Field>
        <Field className="lg:max-w-56">
          <FieldLabel htmlFor="planner-group">{t('desk:planner.group')}</FieldLabel>
          <Select
            value={search.group ?? ALL}
            onValueChange={(v) => setSearch({ group: v === ALL ? undefined : v })}
          >
            <SelectTrigger id="planner-group" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('desk:filters.all')}</SelectItem>
              {(groups.data ?? []).map((g) => (
                <SelectItem key={g.id} value={g.id}>
                  {localName(g.name, i18n.language)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>
      {maps.isPending || doc.isPending || occupancy.isPending ? (
        <LoadingState rows={8} />
      ) : doc.isError || occupancy.isError ? (
        <QueryErrorState
          error={doc.error ?? occupancy.error}
          onRetry={() => {
            void doc.refetch();
            void occupancy.refetch();
          }}
        />
      ) : (
        <PlannerGrid
          marinaId={marinaId}
          from={from}
          days={days}
          today={today}
          rows={plannerRows(doc.data, {
            ...(search.structure ? { structureId: search.structure } : {}),
            ...(search.group ? { groupId: search.group } : {}),
          })}
          allocations={occupancy.data.allocations}
          canMove={can('occupancy.move')}
          canStay={can('transits.desk')}
          canCreate={can('transits.desk')}
        />
      )}
    </RequirePermission>
  );
}

/** Rendered grid of the berth calendar: SVG with integer attributes only (no inline style). */
export function PlannerGrid({
  marinaId,
  from,
  days,
  today,
  rows,
  allocations,
  canMove,
  canStay,
  canCreate,
}: {
  marinaId: string;
  from: string;
  days: number;
  today: string;
  rows: PlannerRow[];
  allocations: readonly MapAllocation[];
  canMove: boolean;
  canStay: boolean;
  canCreate: boolean;
}) {
  const { t } = useT();
  const navigate = useNavigate();
  const { bars, lanes } = useMemo(
    () => layoutBars(allocations, from, days),
    [allocations, from, days],
  );
  const layout = useMemo(() => rowLayout(rows, lanes), [rows, lanes]);
  const height = (layout.at(-1)?.y ?? HEADER_PX) + (layout.at(-1)?.h ?? 0);
  const width = LABEL_PX + days * COL_PX;
  const nights = nightsOf(from, addDays(from, days - 1));
  const svgRef = useRef<SVGSVGElement>(null);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [select, setSelect] = useState<{ rowIndex: number; c1: number; c2: number } | null>(null);
  const [change, setChange] = useState<PlannerChange | null>(null);
  const [create, setCreate] = useState<DeskPrefill | null>(null);
  const weekdays = t('desk:planner.weekdays').split(',');

  // A pointer released outside the SVG ends the gesture without effect.
  useEffect(() => {
    const cancel = () => {
      setDrag(null);
      setSelect(null);
    };
    window.addEventListener('pointercancel', cancel);
    return () => window.removeEventListener('pointercancel', cancel);
  }, []);

  const localX = (e: PointerEvent) =>
    e.clientX - (svgRef.current?.getBoundingClientRect().left ?? 0);
  const rowAtY = (y: number) => layout.findIndex((l) => y >= l.y && y < l.y + l.h);

  const draggable = (a: MapAllocation) => (a.kind === 'transit' || a.kind === 'hold') && canStay;

  const finishDrag = (d: Drag) => {
    const a = d.bar.allocation;
    const rowY = layout[d.rowIndex]!.y + 3 + d.bar.lane * LANE_PX + LANE_PX / 2 + d.dy;
    const targetIndex = rowAtY(rowY);
    if (targetIndex >= 0 && targetIndex !== d.rowIndex && canMove) {
      const move = moveForDrop(a, layout[targetIndex]!.row, today);
      if (move)
        setChange({ kind: 'move', allocation: a, move, targetCode: layout[targetIndex]!.row.code });
      return;
    }
    const delta = dragColumns(d.dx);
    if (delta !== 0 && draggable(a) && a.contract) {
      const stay = stayChangeForDrag(a, delta, d.mode, {
        arrivalFrozen: a.contract.status === 'arrived',
      });
      if (stay) setChange({ kind: 'stay', allocation: a, contractId: a.contract.id, stay });
    }
  };

  const onBarKey = (e: KeyboardEvent, a: MapAllocation) => {
    if (e.key === 'Enter' && a.contract) {
      void navigate(
        a.contract.kind === 'transit'
          ? {
              to: '/m/$marinaId/transits/$contractId',
              params: { marinaId, contractId: a.contract.id },
            }
          : {
              to: '/m/$marinaId/contracts/$contractId',
              params: { marinaId, contractId: a.contract.id },
            },
      );
      return;
    }
    // Keyboard alternative of the resize drag: Alt+→ adds a night, Alt+← removes one.
    if (
      e.altKey &&
      (e.key === 'ArrowRight' || e.key === 'ArrowLeft') &&
      draggable(a) &&
      a.contract
    ) {
      e.preventDefault();
      const stay = stayChangeForDrag(a, e.key === 'ArrowRight' ? 1 : -1, 'resize-end', {
        arrivalFrozen: true,
      });
      if (stay) setChange({ kind: 'stay', allocation: a, contractId: a.contract.id, stay });
    }
  };

  return (
    <div className="flex flex-col gap-3">
      {canCreate ? (
        <div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setCreate({ arrival: today, departure: addDays(today, 1) })}
          >
            <PlusIcon />
            {t('desk:transits.new')}
          </Button>
        </div>
      ) : null}
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('desk:planner.empty')}</p>
      ) : (
        <ScrollArea className="border-border w-full rounded-md border">
          <svg
            ref={svgRef}
            width={width}
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            role="grid"
            aria-label={t('desk:planner.gridLabel', { from: formatIsoDate(from), count: days })}
            data-testid="desk-planner"
            className="touch-none select-none"
            onPointerMove={(e) => {
              if (drag) setDrag({ ...drag, dx: e.clientX - drag.x0, dy: e.clientY - drag.y0 });
              else if (select) {
                const c = columnAt(localX(e), days);
                if (c !== null && c !== select.c2) setSelect({ ...select, c2: c });
              }
            }}
            onPointerUp={() => {
              if (drag) {
                finishDrag(drag);
                setDrag(null);
              } else if (select) {
                const row = layout[select.rowIndex]!.row;
                const lo = Math.min(select.c1, select.c2);
                const hi = Math.max(select.c1, select.c2);
                if (
                  row.resource_type !== 'dynamic_area' &&
                  isFree(bars.get(row.resource_id), lo, hi)
                ) {
                  setCreate({
                    target: { resource_type: row.resource_type, resource_id: row.resource_id },
                    ...selectionStay(from, select.c1, select.c2),
                  });
                }
                setSelect(null);
              }
            }}
          >
            {nights.map((n, i) => (
              <g key={n}>
                <rect
                  x={LABEL_PX + i * COL_PX}
                  y={0}
                  width={COL_PX}
                  height={height}
                  className={
                    n === today
                      ? 'fill-accent'
                      : weekdayOf(n) % 6 === 0
                        ? 'fill-muted'
                        : 'fill-background'
                  }
                />
                <text
                  x={LABEL_PX + i * COL_PX + COL_PX / 2}
                  y={14}
                  textAnchor="middle"
                  className="fill-muted-foreground text-xs"
                >
                  {weekdays[weekdayOf(n)] ?? ''}
                </text>
                <text
                  x={LABEL_PX + i * COL_PX + COL_PX / 2}
                  y={30}
                  textAnchor="middle"
                  className="fill-foreground text-xs"
                >
                  {n.slice(8)}
                </text>
              </g>
            ))}
            {layout.map(({ row, y, h }, rowIndex) => (
              <g
                key={row.resource_id}
                role="row"
                aria-label={row.code}
                data-testid={`desk-planner-row-${row.code}`}
              >
                <rect
                  x={LABEL_PX}
                  y={y}
                  width={days * COL_PX}
                  height={h}
                  className="fill-transparent stroke-border"
                  onPointerDown={(e) => {
                    if (!canCreate || row.resource_type === 'dynamic_area') return;
                    const c = columnAt(localX(e), days);
                    if (c !== null) setSelect({ rowIndex, c1: c, c2: c });
                  }}
                />
                <text x={8} y={y + h / 2 + 4} className="fill-foreground text-xs font-medium">
                  {row.code}
                </text>
                {select && select.rowIndex === rowIndex ? (
                  <rect
                    x={LABEL_PX + Math.min(select.c1, select.c2) * COL_PX}
                    y={y + 2}
                    width={(Math.abs(select.c2 - select.c1) + 1) * COL_PX}
                    height={h - 4}
                    className="fill-primary opacity-30"
                    pointerEvents="none"
                  />
                ) : null}
                {(bars.get(row.resource_id) ?? []).map((bar) => {
                  const a = bar.allocation;
                  const r = barRect(bar, y);
                  const dragging = drag?.bar.allocation.id === a.id ? drag : null;
                  const dx = dragging
                    ? dragging.mode === 'move'
                      ? dragColumns(dragging.dx) * COL_PX
                      : 0
                    : 0;
                  const dw =
                    dragging && dragging.mode === 'resize-end'
                      ? dragColumns(dragging.dx) * COL_PX
                      : 0;
                  const dy = dragging ? dragging.dy : 0;
                  const label = t('desk:planner.barLabel', {
                    code: a.contract?.code ?? t(`occupancyEnums:allocationKind.${a.kind}`),
                    vessel: a.vessel?.name ?? '',
                    from: formatIsoDate(a.first_night),
                    to: formatIsoDate(a.last_night),
                  });
                  return (
                    <g
                      key={a.id}
                      role="button"
                      tabIndex={0}
                      aria-label={label}
                      data-testid="desk-planner-bar"
                      data-allocation-id={a.id}
                      onKeyDown={(e) => onBarKey(e, a)}
                      onPointerDown={(e) => {
                        if (!draggable(a) && !canMove) return;
                        e.stopPropagation();
                        const right = r.x + r.width - localX(e) < RESIZE_HANDLE_PX;
                        setDrag({
                          bar,
                          rowIndex,
                          mode: right ? 'resize-end' : 'move',
                          x0: e.clientX,
                          y0: e.clientY,
                          dx: 0,
                          dy: 0,
                        });
                      }}
                    >
                      <rect
                        x={r.x + dx}
                        y={r.y + dy}
                        width={Math.max(COL_PX / 2, r.width + dw)}
                        height={r.height}
                        rx={4}
                        className={`${barFillClass(a)} ${dragging ? 'opacity-70' : ''}`}
                      />
                      {a.released.map((rel) => {
                        const c = layoutReleased(rel, from, days);
                        return c ? (
                          <rect
                            key={`${rel.from}-${rel.to}`}
                            x={LABEL_PX + c.s * COL_PX + 1}
                            y={r.y + r.height - 5}
                            width={(c.e - c.s + 1) * COL_PX - 2}
                            height={4}
                            className="fill-harbor-released"
                          />
                        ) : null;
                      })}
                      <text
                        x={r.x + dx + 4}
                        y={r.y + dy + r.height / 2 + 4}
                        className="fill-background pointer-events-none text-xs"
                      >
                        {a.vessel?.name ?? t(`occupancyEnums:allocationKind.${a.kind}`)}
                      </text>
                    </g>
                  );
                })}
              </g>
            ))}
          </svg>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>
      )}
      <p className="text-muted-foreground text-xs">{t('desk:planner.hint')}</p>
      {change ? (
        <PlannerChangeDialog marinaId={marinaId} change={change} onClose={() => setChange(null)} />
      ) : null}
      <NewTransitDialog
        marinaId={marinaId}
        open={create !== null}
        onOpenChange={(o) => !o && setCreate(null)}
        {...(create ? { prefill: create } : {})}
      />
    </div>
  );
}

function layoutReleased(rel: { from: string; to: string }, from: string, days: number) {
  const s = Math.max(0, daysBetween(from, rel.from));
  const e = Math.min(days - 1, daysBetween(from, rel.to));
  return e < s ? null : { s, e };
}
