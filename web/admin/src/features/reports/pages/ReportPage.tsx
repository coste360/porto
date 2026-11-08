import { Link, getRouteApi, useNavigate } from '@tanstack/react-router';
import { ChartColumnIcon, DownloadIcon } from 'lucide-react';

import { reportFileName, useReport, useReportExport } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { saveBlob } from '@porto/shared/lib/download';
import type { ReportSearch } from '@porto/shared/lib/reportLinks';
import { Button } from '@porto/shared/ui/button';
import { EmptyState, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { presetPeriod, validPeriod, type Period } from '../../dashboard/lib/period';
import { useMarinaToday } from '../../registry/lib/today';
import { ReportBody } from '../components/ReportBody';
import { ReportFilterBar } from '../components/ReportFilterBar';
import { canOpen, reportDef, sectionOf, type ReportDef } from '../lib/catalog';
import { patchSearch, reportQuery } from '../lib/reportSearch';

const route = getRouteApi('/auth/m/$marinaId/reports/$report');

/**
 * One report (spec 21.3, m7-plan §10.1): filter bar, summary of the totals, chart and groups, records with the
 * Totale row and pagination, CSV/Excel of the same request. All state lives in the URL search, so a dashboard
 * tile link reproduces the filter and the Totale equals the tile.
 */
export function ReportPage() {
  const { t } = useT();
  const { report } = route.useParams();
  const { can, marinaId } = useCurrentMarina();
  const def = reportDef(report);
  if (!def) {
    return (
      <>
        <PageHeader title={t('reports:title')} />
        <EmptyState
          icon={<ChartColumnIcon />}
          title={t('reports:unknown')}
          action={
            <Button asChild variant="outline">
              <Link to="/m/$marinaId/reports" params={{ marinaId }}>
                {t('reports:backToIndex')}
              </Link>
            </Button>
          }
        />
      </>
    );
  }
  return (
    <RequirePermission allowed={canOpen(def, can)}>
      <ReportView key={def.key} def={def} />
    </RequirePermission>
  );
}

function ReportView({ def }: { def: ReportDef }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const search = route.useSearch() as ReportSearch;
  const navigate = useNavigate({ from: '/m/$marinaId/reports/$report' });
  const ytd = presetPeriod('ytd', today);
  const period: Period = { from: search.from ?? ytd.from, to: search.to ?? ytd.to };
  const ready = validPeriod(period);
  const query = reportQuery(search, ytd);
  const section = sectionOf(def, search.section);
  const result = useReport(marinaId, def.key, query, ready);
  const file = useReportExport(marinaId);

  const onPatch = (patch: Record<string, string | undefined>) =>
    void navigate({
      search: (s: ReportSearch) => patchSearch(s, patch),
      replace: true,
    });
  const download = (format: 'csv' | 'xlsx') =>
    file.mutate(
      { key: def.key, query: { ...query, lang: i18n.language }, format },
      {
        onSuccess: (b) =>
          void saveBlob(b, reportFileName(def.key, query.from ?? '', query.to ?? '', format)),
      },
    );

  return (
    <div className="flex min-w-0 flex-col gap-4" data-testid="report-page">
      <PageHeader
        title={t(`reports:keys.${def.key}`)}
        description={t(`reports:descriptions.${def.key}`)}
        actions={
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              className="min-h-11 sm:min-h-9"
              disabled={!ready || file.isPending}
              onClick={() => download('csv')}
            >
              <DownloadIcon />
              {t('reports:export.csv')}
            </Button>
            <Button
              variant="outline"
              className="min-h-11 sm:min-h-9"
              disabled={!ready || file.isPending}
              onClick={() => download('xlsx')}
            >
              <DownloadIcon />
              {t('reports:export.xlsx')}
            </Button>
          </div>
        }
      />
      <ReportFilterBar
        def={def}
        marinaId={marinaId}
        search={search}
        period={period}
        today={today}
        onPatch={onPatch}
      />
      {def.sections ? (
        <ToggleGroup
          type="single"
          variant="outline"
          value={section}
          onValueChange={(v) => v && onPatch({ section: v === def.sections?.[0] ? undefined : v })}
          aria-label={t('reports:filters.section')}
          className="flex-wrap justify-start"
        >
          {def.sections.map((s) => (
            <ToggleGroupItem key={s} value={s} className="min-h-11 px-3 sm:min-h-9">
              {t(`reports:sections.${def.key}.${s}`)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      ) : null}
      {file.isError ? <ProblemAlert error={file.error} /> : null}
      {!ready ? (
        <p className="text-muted-foreground text-sm">{t('dashboard:invalidPeriod')}</p>
      ) : result.isPending ? (
        <LoadingState rows={6} />
      ) : result.isError ? (
        <QueryErrorState error={result.error} onRetry={() => void result.refetch()} />
      ) : (
        <ReportBody
          def={def}
          data={result.data}
          section={section}
          marinaId={marinaId}
          onPage={(p) => onPatch({ page: p <= 1 ? undefined : String(p) })}
        />
      )}
    </div>
  );
}
