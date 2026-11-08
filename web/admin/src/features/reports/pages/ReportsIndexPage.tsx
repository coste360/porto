import { Link } from '@tanstack/react-router';
import { ChartColumnIcon, ChevronRightIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Card, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { REPORTS, UPCOMING_REPORTS, canOpen } from '../lib/catalog';

const cardClass =
  'hover:bg-accent focus-visible:ring-ring block rounded-xl focus-visible:ring-2 focus-visible:outline-none';

/**
 * Index of the reports (m7-plan §10.1): cards grouped Operativi / Economici, only those the role may open
 * (reports.operational sees the operational ones), plus the reports announced for M10/M11.
 */
export function ReportsIndexPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const allowed = REPORTS.filter((r) => canOpen(r, can));
  const groups = (['operational', 'economic'] as const)
    .map((g) => ({ group: g, items: allowed.filter((r) => r.group === g) }))
    .filter((g) => g.items.length > 0);
  return (
    <RequirePermission allowed={allowed.length > 0}>
      <PageHeader title={t('reports:title')} description={t('reports:description')} />
      <div className="flex flex-col gap-6" data-testid="reports-index">
        {groups.map(({ group, items }) => (
          <section key={group} className="flex flex-col gap-3">
            <h2 className="text-lg font-semibold">{t(`reports:groups.${group}`)}</h2>
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {items.map((r) => (
                <Link
                  key={r.key}
                  to="/m/$marinaId/reports/$report"
                  params={{ marinaId, report: r.key }}
                  className={cardClass}
                  data-testid={`report-card-${r.key}`}
                >
                  <ReportCard
                    title={t(`reports:keys.${r.key}`)}
                    description={t(`reports:descriptions.${r.key}`)}
                  />
                </Link>
              ))}
              {group === groups[groups.length - 1]?.group ? (
                <Link
                  to="/m/$marinaId/reports/comparison"
                  params={{ marinaId }}
                  className={cardClass}
                  data-testid="report-card-comparison"
                >
                  <ReportCard
                    title={t('reports:keys.comparison')}
                    description={t('reports:descriptions.comparison')}
                  />
                </Link>
              ) : null}
            </div>
          </section>
        ))}
        <section className="flex flex-col gap-3">
          <h2 className="text-lg font-semibold">{t('reports:groups.upcoming')}</h2>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {UPCOMING_REPORTS.map((r) => (
              <Card key={r.key} className="opacity-70" data-testid={`report-card-${r.key}`}>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <ChartColumnIcon className="size-4" />
                    {t(`reports:keys.${r.key}`)}
                  </CardTitle>
                  <CardDescription>
                    <Badge variant="outline">
                      {t('reports:unavailable', { milestone: r.milestone })}
                    </Badge>
                  </CardDescription>
                </CardHeader>
              </Card>
            ))}
          </div>
        </section>
      </div>
    </RequirePermission>
  );
}

function ReportCard({ title, description }: { title: string; description: string }) {
  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle className="flex items-center justify-between gap-2">
          <span className="flex items-center gap-2">
            <ChartColumnIcon className="size-4" />
            {title}
          </span>
          <ChevronRightIcon className="text-muted-foreground size-4" />
        </CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
    </Card>
  );
}
