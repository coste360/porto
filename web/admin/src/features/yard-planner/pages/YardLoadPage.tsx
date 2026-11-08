import { AlertTriangleIcon, ChevronLeftIcon, ChevronRightIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useAtRiskOrders,
  useYardLoad,
  type YardLoadGroupBy,
  type YardLoadPeriod,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { PageHeader } from '@porto/shared/ui/patterns';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { YardPlannerNav } from '../components/kit';
import { useQualificationIndex } from '../lib/qualifications';
import { heatClass, loadLabel } from '../lib/load';
import { hoursLabel, periodOf, shiftAnchor, type PlannerView } from '../lib/planner';

const GROUPS: readonly YardLoadGroupBy[] = ['employee', 'department', 'qualification', 'bay'];

/** Workload (m10-plan §6.3, §14.3): heatmap by employee, department, qualification or bay, and at-risk orders. */
export function YardLoadPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [groupBy, setGroupBy] = useState<YardLoadGroupBy>('employee');
  const [range, setRange] = useState<PlannerView>('week');
  const [anchor, setAnchor] = useState<string | null>(null);
  const a = anchor ?? today;
  const { from, to } = periodOf(range, a);
  const period: YardLoadPeriod = range === 'month' ? 'week' : 'day';
  const load = useYardLoad(marinaId, { group_by: groupBy, period, from, to });
  const risk = useAtRiskOrders(marinaId);
  const quals = useQualificationIndex(marinaId);
  const columns = load.data?.rows[0]?.cells.map((c) => c.period_start) ?? [];
  return (
    <RequirePermission allowed={can('yard.read') || can('yard.manage')}>
      <div className="flex w-full flex-col gap-4">
        <PageHeader
          title={t('yardPlanner:load.title')}
          description={t('yardPlanner:load.description')}
        />
        <YardPlannerNav marinaId={marinaId} current="load" />
        <div className="flex flex-wrap items-center gap-3">
          <ToggleGroup
            type="single"
            variant="outline"
            value={groupBy}
            onValueChange={(v) => v && setGroupBy(v as YardLoadGroupBy)}
            aria-label={t('yardPlanner:load.groupBy')}
          >
            {GROUPS.map((g) => (
              <ToggleGroupItem key={g} value={g}>
                {t(`yardPlanner:load.groups.${g}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <ToggleGroup
            type="single"
            variant="outline"
            value={range}
            onValueChange={(v) => v && setRange(v as PlannerView)}
            aria-label={t('yardPlanner:planner.view')}
          >
            {(['week', 'month'] as const).map((v) => (
              <ToggleGroupItem key={v} value={v}>
                {t(`yardPlanner:planner.views.${v}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('yardPlanner:planner.previous')}
              onClick={() => setAnchor(shiftAnchor(range, a, -1))}
            >
              <ChevronLeftIcon />
            </Button>
            <span className="text-sm font-medium">
              {formatIsoDate(from)} – {formatIsoDate(to)}
            </span>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('yardPlanner:planner.next')}
              onClick={() => setAnchor(shiftAnchor(range, a, 1))}
            >
              <ChevronRightIcon />
            </Button>
          </div>
        </div>
        {load.isPending ? (
          <LoadingState rows={5} />
        ) : load.isError ? (
          <QueryErrorState error={load.error} onRetry={() => void load.refetch()} />
        ) : load.data.rows.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('yardPlanner:load.empty')}</p>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full border-collapse text-sm" data-testid="load-table">
              <thead>
                <tr>
                  <th className="bg-muted p-2 text-left font-medium">
                    {t(`yardPlanner:load.groups.${groupBy}`)}
                  </th>
                  {columns.map((c) => (
                    <th key={c} className="bg-muted p-2 text-left font-medium">
                      {formatIsoDate(c)}
                    </th>
                  ))}
                  <th className="bg-muted p-2 text-left font-medium">
                    {t('yardPlanner:load.total')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {load.data.rows.map((r) => (
                  <tr key={r.key} className="border-t">
                    <th scope="row" className="p-2 text-left font-medium">
                      {r.label}
                    </th>
                    {r.cells.map((c) => (
                      <td key={c.period_start} className="p-1">
                        <span
                          data-testid="load-cell"
                          className={`block rounded px-1 text-xs ${heatClass(c.load_bp ?? null, c.overload, c.assigned_milli)}`}
                          title={t('yardPlanner:load.cellTitle', {
                            assigned: hoursLabel(c.assigned_milli),
                            capacity: hoursLabel(c.capacity_milli),
                            actual: hoursLabel(c.actual_milli),
                          })}
                        >
                          {groupBy === 'bay'
                            ? hoursLabel(c.assigned_milli)
                            : `${hoursLabel(c.assigned_milli)} / ${hoursLabel(c.capacity_milli)}`}
                        </span>
                      </td>
                    ))}
                    <td className="p-2 text-xs font-medium">
                      {groupBy === 'bay'
                        ? hoursLabel(r.assigned_milli)
                        : loadLabel(r.load_bp ?? null)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <AlertTriangleIcon className="size-4" />
              {t('yardPlanner:load.atRisk')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {(risk.data?.items.length ?? 0) === 0 ? (
              <p className="text-muted-foreground text-sm">{t('yardPlanner:load.noRisk')}</p>
            ) : (
              <ul className="flex flex-col gap-2" data-testid="at-risk-list">
                {risk.data!.items.map((o) => (
                  <li
                    key={o.work_order_id}
                    className="flex flex-col gap-1 rounded-md border p-2 text-sm"
                  >
                    <span className="font-medium">
                      {o.number}
                      {o.vessel_name ? ` · ${o.vessel_name}` : ''}
                      {o.planned_delivery_on
                        ? ` · ${t('yardPlanner:load.delivery', { date: formatIsoDate(o.planned_delivery_on) })}`
                        : ''}
                    </span>
                    <span className="flex flex-wrap gap-1">
                      {o.reasons.map((r, i) =>
                        r.code === 'late' ? (
                          <Badge key={i} variant="destructive">
                            {t('yardPlanner:load.late')}
                          </Badge>
                        ) : (
                          <Badge key={i} variant="secondary">
                            {t('yardPlanner:load.capacity', {
                              qualification: quals.byId.get(r.qualification_id ?? '')?.code ?? '—',
                              remaining: hoursLabel(r.remaining_milli ?? 0),
                              free: hoursLabel(r.free_milli ?? 0),
                            })}
                          </Badge>
                        ),
                      )}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>
    </RequirePermission>
  );
}
