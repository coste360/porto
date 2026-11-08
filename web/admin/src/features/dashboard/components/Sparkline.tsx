import { Line, LineChart, YAxis } from 'recharts';

import type { TileSeriesPoint } from '@porto/shared/api';
import { ChartContainer, type ChartConfig } from '@porto/shared/ui/chart';

// Tile sparkline (m7-plan §4): current values as a solid line, aligned previous values dashed. Integer values are
// plotted as they are; the chart only needs relative heights.

const config = {
  value: { label: 'value', color: 'var(--chart-1)' },
  previous: { label: 'previous', color: 'var(--chart-2)' },
} satisfies ChartConfig;

export function Sparkline({ series, label }: { series: TileSeriesPoint[]; label: string }) {
  if (series.length < 2) return null;
  const data = series.map((p) => ({
    bucket: p.bucket,
    value: p.value ?? null,
    previous: p.previous ?? null,
  }));
  const hasPrevious = series.some((p) => p.previous !== undefined && p.previous !== null);
  return (
    <ChartContainer
      config={config}
      className="aspect-auto h-12 w-full"
      role="img"
      aria-label={label}
      data-testid="tile-sparkline"
    >
      <LineChart data={data} margin={{ top: 4, right: 0, bottom: 4, left: 0 }}>
        <YAxis hide domain={['dataMin', 'dataMax']} />
        {hasPrevious ? (
          <Line
            dataKey="previous"
            type="monotone"
            stroke="var(--color-previous)"
            strokeDasharray="3 3"
            strokeWidth={1}
            dot={false}
            isAnimationActive={false}
            connectNulls
          />
        ) : null}
        <Line
          dataKey="value"
          type="monotone"
          stroke="var(--color-value)"
          strokeWidth={2}
          dot={false}
          isAnimationActive={false}
          connectNulls
        />
      </LineChart>
    </ChartContainer>
  );
}
