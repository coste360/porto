import { useT } from '@porto/shared/i18n';
import { TOKEN_CLASSES } from '@porto/shared/map';
import { cn } from '@porto/shared/lib/utils';

import { daysBetween, timelineTicks, type TimelineRow } from '../lib/plan';

// Timeline of an area (spec 17.2): one row per boat, sorted by exit, with the boat name on the left and a light bar
// from arrival to exit over the window, a date axis with ticks, a red marker on blocked exits and the cursor on the
// selected date. SVG in fixed units scaled to the width (uniform scaling: text keeps its proportions).

const WIDTH = 1000;
const LABEL_W = 200;
const PAD_R = 12;
const AXIS_H = 28;
const ROW = 30;
const BAR_H = 16;
const FONT = 15;
/** Longest name drawn in full (characters); longer names end with an ellipsis, the full one is in the tooltip. */
const NAME_CHARS = 22;

function shortName(s: string): string {
  return s.length > NAME_CHARS ? `${s.slice(0, NAME_CHARS - 1)}…` : s;
}

export function Timeline({
  rows,
  from,
  to,
  date,
  selected,
  onSelect,
  ariaLabel,
  rowLabel,
}: {
  rows: readonly TimelineRow[];
  from: string;
  to: string;
  date: string;
  selected?: string;
  onSelect?: (id: string) => void;
  ariaLabel: string;
  rowLabel: (r: TimelineRow) => string;
}) {
  const { i18n } = useT();
  const days = Math.max(1, daysBetween(from, to) + 1);
  const chartW = WIDTH - LABEL_W - PAD_R;
  const dayW = chartW / days;
  const height = AXIS_H + Math.max(1, rows.length) * ROW + 4;
  const clamp = (d: string) => Math.min(days, Math.max(0, daysBetween(from, d)));
  const xOf = (d: string) => LABEL_W + clamp(d) * dayW;
  const cursor = xOf(date);
  const tickFmt = new Intl.DateTimeFormat(i18n.language, {
    day: '2-digit',
    month: 'short',
    timeZone: 'UTC',
  });
  const ticks = timelineTicks(from, to);
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${height}`}
      className="bg-background h-auto w-full rounded-md border"
      role="list"
      aria-label={ariaLabel}
      data-testid="storage-timeline"
    >
      {ticks.map((d) => (
        <g key={d} aria-hidden>
          <line
            x1={xOf(d)}
            x2={xOf(d)}
            y1={AXIS_H - 4}
            y2={height}
            className="stroke-border"
            strokeWidth={1}
          />
          <text
            x={xOf(d) + 3}
            y={AXIS_H / 2}
            fontSize={FONT - 1}
            dominantBaseline="central"
            className="fill-muted-foreground"
          >
            {tickFmt.format(new Date(`${d}T00:00:00Z`))}
          </text>
        </g>
      ))}
      {rows.map((r, i) => {
        const x0 = xOf(r.arrival);
        const x1 = xOf(r.exit);
        const top = AXIS_H + i * ROW;
        const mid = top + ROW / 2;
        const sel = selected === r.allocationId;
        const label = rowLabel(r);
        return (
          <g
            key={r.allocationId}
            role="listitem"
            tabIndex={0}
            aria-label={label}
            data-allocation={r.allocationId}
            data-exit-blocked={r.exitBlocked ? 'true' : undefined}
            className="group cursor-pointer focus-visible:outline-none"
            onClick={() => onSelect?.(r.allocationId)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onSelect?.(r.allocationId);
              }
            }}
          >
            <title>{label}</title>
            <rect
              x={0}
              y={top}
              width={WIDTH}
              height={ROW}
              className={cn(
                sel ? 'fill-primary/10' : i % 2 === 1 ? 'fill-muted/50' : 'fill-transparent',
                'group-hover:fill-accent group-focus-visible:fill-accent',
              )}
            />
            <text
              x={8}
              y={mid}
              fontSize={FONT}
              dominantBaseline="central"
              className={cn('fill-foreground', sel ? 'font-semibold' : '')}
            >
              {shortName(r.label)}
            </text>
            <rect
              x={x0}
              y={mid - BAR_H / 2}
              width={Math.max(dayW / 2, x1 - x0)}
              height={BAR_H}
              rx={4}
              className={sel ? 'fill-primary stroke-primary' : 'fill-chart-2/45 stroke-chart-2'}
              strokeWidth={1}
            />
            {r.exitBlocked ? (
              <rect
                x={Math.max(LABEL_W, x1 - 6)}
                y={mid - BAR_H / 2 - 2}
                width={6}
                height={BAR_H + 4}
                rx={1}
                className={TOKEN_CLASSES.bad.fill}
              />
            ) : null}
          </g>
        );
      })}
      <line
        x1={cursor}
        x2={cursor}
        y1={AXIS_H - 4}
        y2={height}
        className="stroke-foreground"
        strokeWidth={1.5}
        strokeDasharray="4 3"
      />
    </svg>
  );
}
