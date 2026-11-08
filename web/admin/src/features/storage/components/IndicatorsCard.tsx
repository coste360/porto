import type { StorageIndicators } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm2, formatInteger } from '@porto/shared/lib/format';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';

// Indicators of a draft, a scenario or the real state (spec 17.3): surface used, boats blocked, moves, unplaced,
// overlaps, largest free rectangle.

export function IndicatorsList({ ind }: { ind: StorageIndicators | undefined }) {
  const { t, i18n } = useT();
  if (!ind) return null;
  const rows: [string, string][] = [
    [
      'usedFootprint',
      `${formatCm2(ind.used_footprint_cm2, i18n.language)} / ${formatCm2(ind.usable_cm2, i18n.language)}`,
    ],
    ['usedHull', formatCm2(ind.used_hull_cm2, i18n.language)],
    ['exitsBlocked', formatInteger(ind.exits_blocked, i18n.language)],
    ['arrivalsBlocked', formatInteger(ind.arrivals_blocked, i18n.language)],
    ['moves', formatInteger(ind.moves, i18n.language)],
    ['unplaced', formatInteger(ind.unplaced, i18n.language)],
    ['overlaps', formatInteger(ind.overlaps, i18n.language)],
    ['largestFree', formatCm2(ind.largest_free_rect_cm2, i18n.language)],
  ];
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm" data-testid="storage-indicators">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-muted-foreground">{t(`storage:indicators.${k}`)}</dt>
          <dd className="text-right tabular-nums" data-indicator={k}>
            {v}
          </dd>
        </div>
      ))}
    </dl>
  );
}

export function IndicatorsCard({ ind }: { ind: StorageIndicators | undefined }) {
  const { t } = useT();
  return (
    <Card className="gap-2 py-4">
      <CardHeader className="px-4">
        <CardTitle className="text-base">{t('storage:indicators.title')}</CardTitle>
      </CardHeader>
      <CardContent className="px-4">
        <IndicatorsList ind={ind} />
      </CardContent>
    </Card>
  );
}
