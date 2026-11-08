import { Link } from '@tanstack/react-router';
import {
  ArrowRightIcon,
  ChartLineIcon,
  MinusIcon,
  TrendingDownIcon,
  TrendingUpIcon,
} from 'lucide-react';

import type { Tile } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp, formatReportValue } from '@porto/shared/lib/format';
import { reportLink, type ReportPeriodSearch } from '@porto/shared/lib/reportLinks';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@porto/shared/ui/card';

import { isComparableTile, milestoneOf } from '../lib/period';
import { Sparkline } from './Sparkline';

export type TileCardProps = {
  tile: Tile;
  marinaId: string;
  period: ReportPeriodSearch;
  /** Compact layout of the direction phone screen: no sparkline, no breakdown, no comparison link. */
  compact?: boolean;
};

const trendIcon = {
  up: <TrendingUpIcon />,
  down: <TrendingDownIcon />,
  flat: <MinusIcon />,
  none: null,
} as const;

/** Signed delta in the tile unit ("+1.234,00 €", "-2,5%"). */
function signed(text: string, value: number): string {
  return value > 0 ? `+${text}` : text;
}

/**
 * A manager dashboard tile (spec 21.2): value of the period, previous year, change and trend; the whole value
 * opens the report of the tile with the same period and the drill parameters from the server (m7-plan §6).
 */
export function TileCard({ tile, marinaId, period, compact = false }: TileCardProps) {
  const { t, i18n } = useT();
  const title = t(`dashboard:tiles.${tile.key}`);
  const fmt = (v: number | null | undefined) => formatReportValue(v, tile.unit, i18n.language);

  if (!tile.available) {
    return (
      <Card
        data-testid="dashboard-tile"
        data-tile={tile.key}
        data-available="false"
        className="bg-muted/40 gap-2 py-4"
      >
        <CardHeader className="px-4">
          <CardDescription>{title}</CardDescription>
          <CardTitle className="text-muted-foreground text-sm font-normal">
            {t('dashboard:unavailable', { milestone: milestoneOf(tile.reason) })}
          </CardTitle>
        </CardHeader>
      </Card>
    );
  }

  const link = tile.drill ? reportLink(marinaId, tile.drill, period) : null;
  const value = fmt(tile.value);
  const previous = tile.previous;
  const trend = tile.trend;
  const delta =
    tile.delta !== undefined && tile.delta !== null ? signed(fmt(tile.delta), tile.delta) : null;
  const deltaBp =
    tile.delta_bp !== undefined && tile.delta_bp !== null
      ? signed(formatBp(tile.delta_bp, i18n.language), tile.delta_bp)
      : null;

  return (
    <Card
      data-testid="dashboard-tile"
      data-tile={tile.key}
      data-available="true"
      className={compact ? 'gap-2 py-3' : 'gap-3 py-4'}
    >
      <CardHeader className={compact ? 'px-3' : 'px-4'}>
        <CardDescription>{title}</CardDescription>
        <CardTitle className="text-2xl font-semibold tabular-nums">
          {link ? (
            <Link
              {...link}
              className="hover:underline focus-visible:underline"
              aria-label={`${title}: ${value}. ${t('dashboard:openReport')}`}
              data-testid="tile-value"
            >
              {value}
            </Link>
          ) : (
            <span data-testid="tile-value">{value}</span>
          )}
        </CardTitle>
        {!compact && tile.dims_applied ? (
          <CardAction>
            <Badge variant="outline">{t('dashboard:dimsApplied')}</Badge>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className={`flex flex-col gap-2 ${compact ? 'px-3' : 'px-4'}`}>
        {previous ? (
          <div className="text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
            <span data-testid="tile-previous">
              {t('dashboard:previousValue', { value: fmt(previous.value) })}
            </span>
            {delta ? (
              <span
                className="text-foreground flex items-center gap-1 [&_svg]:size-3.5"
                data-testid="tile-delta"
                data-trend={trend}
                title={t(`dashboard:trend.${trend}`)}
              >
                {trendIcon[trend]}
                <span className="sr-only">{t(`dashboard:trend.${trend}`)}</span>
                {deltaBp ? `${delta} (${deltaBp})` : delta}
              </span>
            ) : null}
          </div>
        ) : null}
        {!compact ? (
          <Sparkline series={tile.series} label={t('dashboard:seriesLabel', { tile: title })} />
        ) : null}
        {!compact && tile.breakdown && tile.breakdown.length > 0 ? (
          <ul
            className="flex flex-wrap gap-1"
            aria-label={t('dashboard:breakdown')}
            data-testid="tile-breakdown"
          >
            {tile.breakdown.map((b) => (
              <li key={b.key}>
                <Badge variant="secondary" className="tabular-nums">
                  {b.label ?? b.key} · {formatReportValue(b.value, 'count', i18n.language)}
                </Badge>
              </li>
            ))}
          </ul>
        ) : null}
      </CardContent>
      {link && !compact ? (
        <CardFooter className="flex flex-wrap gap-2 px-4">
          <Button variant="ghost" size="sm" asChild>
            <Link {...link}>
              {t('dashboard:openReport')}
              <ArrowRightIcon />
            </Link>
          </Button>
          {isComparableTile(tile.key) ? (
            <Button variant="ghost" size="sm" asChild>
              <Link
                to="/m/$marinaId/reports/comparison"
                params={{ marinaId }}
                search={{ indicator: tile.key, from: period.from, to: period.to }}
              >
                <ChartLineIcon />
                {t('dashboard:compareIndicator')}
              </Link>
            </Button>
          ) : null}
        </CardFooter>
      ) : null}
    </Card>
  );
}
