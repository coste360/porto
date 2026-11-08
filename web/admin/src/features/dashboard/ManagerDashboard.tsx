import { Link } from '@tanstack/react-router';
import { ChartLineIcon } from 'lucide-react';
import { useState } from 'react';

import { useDashboard, type ReportAlign } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../components/PageStates';
import { useMarinaToday } from '../registry/lib/today';
import { DimensionFilters, type Dims } from './components/DimensionFilters';
import { PeriodSelector } from './components/PeriodSelector';
import { useCanSeeDashboard } from './lib/access';
import { TileCard } from './components/TileCard';
import {
  PERIOD_PRESETS,
  presetPeriod,
  validPeriod,
  type Period,
  type PeriodPreset,
} from './lib/period';

type Compare = 'previous_year' | 'none';

/**
 * Manager dashboard (spec 21.2, m7-plan §4): the tiles the role may see for the chosen period, each compared
 * with the same period of the previous year and opening its report with the same filter. Renders nothing for
 * roles without report permissions.
 */
export function ManagerDashboard() {
  const allowed = useCanSeeDashboard();
  return allowed ? <ManagerDashboardTiles /> : null;
}

function ManagerDashboardTiles() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const [preset, setPreset] = useState<PeriodPreset>('ytd');
  const [custom, setCustom] = useState<Partial<Period>>(() => presetPeriod('ytd', today));
  const [compare, setCompare] = useState<Compare>('previous_year');
  const [align, setAlign] = useState<ReportAlign>('date');
  const [dims, setDims] = useState<Dims>({});

  const period: Partial<Period> = preset === 'custom' ? custom : presetPeriod(preset, today);
  const ready = validPeriod(period);
  const dashboard = useDashboard(
    marinaId,
    {
      from: period.from,
      to: period.to,
      compare: compare === 'previous_year' ? undefined : compare,
      align: align === 'date' ? undefined : align,
      ...dims,
    },
    ready,
  );
  const shown = dashboard.data;
  const linkPeriod = shown ? { from: shown.period.from, to: shown.period.to } : null;
  const tiles = shown?.tiles ?? [];
  const available = tiles.filter((x) => x.available);
  const seams = tiles.filter((x) => !x.available);

  return (
    <section
      className="flex flex-col gap-4"
      aria-labelledby="manager-dashboard-title"
      data-testid="manager-dashboard"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="manager-dashboard-title" className="text-lg font-semibold">
          {t('dashboard:sectionTitle')}
        </h2>
        <Button variant="outline" size="sm" asChild>
          <Link
            to="/m/$marinaId/reports/comparison"
            params={{ marinaId }}
            search={ready ? { from: period.from, to: period.to } : {}}
          >
            <ChartLineIcon />
            {t('dashboard:comparison.open')}
          </Link>
        </Button>
      </div>
      <div className="flex flex-col gap-3 lg:flex-row lg:flex-wrap lg:items-end">
        <PeriodSelector
          idPrefix="dashboard"
          presets={PERIOD_PRESETS}
          preset={preset}
          period={period}
          onPreset={(p) => {
            if (p === 'custom') setCustom(period);
            setPreset(p);
          }}
          onPeriod={setCustom}
        />
        <Field className="lg:max-w-48">
          <FieldLabel htmlFor="dashboard-compare">{t('dashboard:compare.label')}</FieldLabel>
          <Select value={compare} onValueChange={(v) => setCompare(v as Compare)}>
            <SelectTrigger id="dashboard-compare" className="min-h-11 w-full sm:min-h-9">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="previous_year">{t('dashboard:compare.previous_year')}</SelectItem>
              <SelectItem value="none">{t('dashboard:compare.none')}</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        {compare === 'previous_year' ? (
          <Field className="lg:max-w-60">
            <FieldLabel htmlFor="dashboard-align">{t('dashboard:align.label')}</FieldLabel>
            <Select value={align} onValueChange={(v) => setAlign(v as ReportAlign)}>
              <SelectTrigger id="dashboard-align" className="min-h-11 w-full sm:min-h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="date">{t('dashboard:align.date')}</SelectItem>
                <SelectItem value="weekday">{t('dashboard:align.weekday')}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        ) : null}
        <DimensionFilters marinaId={marinaId} value={dims} onChange={setDims} />
      </div>
      {!ready ? null : dashboard.isPending ? (
        <LoadingState rows={4} />
      ) : dashboard.isError ? (
        <QueryErrorState error={dashboard.error} onRetry={() => void dashboard.refetch()} />
      ) : available.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('dashboard:empty')}</p>
      ) : (
        <div
          className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3"
          data-testid="dashboard-tiles"
          aria-busy={dashboard.isPlaceholderData || undefined}
        >
          {available.map((tile) => (
            <TileCard key={tile.key} tile={tile} marinaId={marinaId} period={linkPeriod!} />
          ))}
        </div>
      )}
      {ready && seams.length > 0 ? (
        <div className="flex flex-col gap-2" data-testid="dashboard-seams">
          <h3 className="text-muted-foreground text-sm font-medium">{t('dashboard:outOfScope')}</h3>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
            {seams.map((tile) => (
              <TileCard key={tile.key} tile={tile} marinaId={marinaId} period={linkPeriod!} />
            ))}
          </div>
        </div>
      ) : null}
    </section>
  );
}
