import { TOKEN_CLASSES } from '@porto/shared/map';
import { cn } from '@porto/shared/lib/utils';

import { daysBetween, type TimelineRow } from '../lib/plan';

// Timeline of an area (spec 17.2): one bar per boat from arrival to exit over the window, sorted by exit, a red
// marker on blocked exits and the cursor on the selected date. SVG in day units (one day = 10 units).

const DAY = 10;
const ROW = 12;

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
  const days = Math.max(1, daysBetween(from, to) + 1);
  const width = days * DAY;
  const height = Math.max(1, rows.length) * ROW;
  const clamp = (d: string) => Math.min(days, Math.max(0, daysBetween(from, d)));
  const cursor = clamp(date) * DAY;
  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      className="bg-background h-auto w-full rounded-md border"
      role="list"
      aria-label={ariaLabel}
      data-testid="storage-timeline"
    >
      {rows.map((r, i) => {
        const x0 = clamp(r.arrival) * DAY;
        const x1 = clamp(r.exit) * DAY;
        const y = i * ROW + 2;
        const sel = selected === r.allocationId;
        return (
          <g
            key={r.allocationId}
            role="listitem"
            tabIndex={0}
            aria-label={rowLabel(r)}
            data-allocation={r.allocationId}
            data-exit-blocked={r.exitBlocked ? 'true' : undefined}
            className="cursor-pointer focus-visible:outline-none"
            onClick={() => onSelect?.(r.allocationId)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onSelect?.(r.allocationId);
              }
            }}
          >
            <rect
              x={x0}
              y={y}
              width={Math.max(DAY / 2, x1 - x0)}
              height={ROW - 4}
              rx={2}
              className={cn(sel ? 'fill-primary' : TOKEN_CLASSES.annual.fill)}
            />
            {r.exitBlocked ? (
              <rect
                x={Math.max(0, x1 - DAY / 2)}
                y={y - 1}
                width={DAY / 2}
                height={ROW - 2}
                className={TOKEN_CLASSES.bad.fill}
              />
            ) : null}
          </g>
        );
      })}
      <line
        x1={cursor}
        x2={cursor}
        y1={0}
        y2={height}
        className="stroke-foreground"
        strokeWidth={1}
      />
    </svg>
  );
}
