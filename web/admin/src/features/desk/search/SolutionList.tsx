import { TriangleAlertIcon } from 'lucide-react';
import type { ReactNode } from 'react';

import type { OccupancySolution } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp, formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { cn } from '@porto/shared/lib/utils';

import { solutionCodes } from '../lib/search';

/**
 * Ordered search results (10.7): codes, segments, price or price error, fit warnings, `requires_release` flag.
 * Hover and keyboard focus report the solution so the map can highlight it.
 */
export function SolutionList({
  solutions,
  selectedKey,
  onFocusSolution,
  actions,
}: {
  solutions: readonly OccupancySolution[];
  selectedKey?: string | null;
  onFocusSolution?: (s: OccupancySolution | null) => void;
  actions: (s: OccupancySolution) => ReactNode;
}) {
  const { t, i18n } = useT();
  return (
    <ul className="flex flex-col gap-2" aria-label={t('desk:search.results')}>
      {solutions.map((s) => (
        <li
          key={s.key}
          data-testid="desk-solution"
          className={cn(
            'border-border flex flex-col gap-2 rounded-md border p-3',
            selectedKey === s.key && 'border-primary',
          )}
          onMouseEnter={() => onFocusSolution?.(s)}
          onMouseLeave={() => onFocusSolution?.(null)}
          onFocus={() => onFocusSolution?.(s)}
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="font-medium">{solutionCodes(s)}</span>
            <span className="tabular-nums">
              {s.pricing
                ? formatCents(s.pricing.total_cents, 'EUR', i18n.language)
                : t('desk:search.noPrice')}
            </span>
          </div>
          {s.segments.length > 1 ? (
            <ol className="text-muted-foreground text-xs">
              {s.segments.map((seg) => (
                <li key={`${seg.resource_id}-${seg.first_night}`}>
                  {t('desk:search.segment', {
                    code: seg.code,
                    from: formatIsoDate(seg.first_night),
                    to: formatIsoDate(seg.last_night),
                  })}
                </li>
              ))}
            </ol>
          ) : null}
          <div className="flex flex-wrap gap-1">
            <Badge variant="secondary">
              {t('desk:search.fill', { value: formatBp(s.fit.fill_bp, i18n.language) })}
            </Badge>
            {s.requires_release ? (
              <Badge variant="outline">{t('desk:search.requiresRelease')}</Badge>
            ) : null}
            {s.fit.warnings.map((w) => (
              <Badge key={w.code} variant="outline">
                <TriangleAlertIcon />
                {t(`occupancyEnums:fit.${w.code}`)}
              </Badge>
            ))}
          </div>
          <div className="flex flex-wrap gap-2">{actions(s)}</div>
        </li>
      ))}
    </ul>
  );
}
