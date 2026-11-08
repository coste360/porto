import { Link, getRouteApi, useNavigate } from '@tanstack/react-router';

import {
  useCompareStorageScenario,
  useStoragePlan,
  useStorageScenario,
  useStorageScenarios,
  type StorageIndicators,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { PlanGrid, type PlanBoat } from '../components/PlanGrid';
import { draftReducer, EMPTY_DRAFT, type DraftItem } from '../lib/draft';
import {
  compareRows,
  gridOf,
  planDraftItems,
  positionText,
  presentOn,
  scenarioDraftItems,
} from '../lib/plan';

const route = getRouteApi('/auth/m/$marinaId/storage/$areaId/scenarios/$scenarioId/compare');

const INDICATORS: (keyof StorageIndicators)[] = [
  'used_footprint_cm2',
  'exits_blocked',
  'arrivals_blocked',
  'moves',
  'unplaced',
  'overlaps',
  'largest_free_rect_cm2',
];

/** Side-by-side comparison of a scenario with the real state or another scenario at the same date (spec 17.3). */
export function ScenarioComparePage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { areaId, scenarioId } = route.useParams();
  const search = route.useSearch();
  const navigate = useNavigate({
    from: '/m/$marinaId/storage/$areaId/scenarios/$scenarioId/compare',
  });
  const left = useStorageScenario(marinaId, scenarioId);
  const s = left.data?.scenario;
  const withId = search.with && search.with !== 'real' ? search.with : undefined;
  const right = useStorageScenario(marinaId, withId);
  const plan = useStoragePlan(marinaId, areaId, s?.from_on, s?.to_on);
  const others = useStorageScenarios(marinaId, areaId);
  const date = search.date ?? s?.from_on;
  const cmp = useCompareStorageScenario(marinaId, scenarioId, withId ?? 'real', date);

  if (left.isPending || plan.isPending) return <LoadingState />;
  if (left.isError) return <QueryErrorState error={left.error} />;
  if (plan.isError) return <QueryErrorState error={plan.error} />;
  const p = plan.data!;
  const grid = gridOf(p.area);
  const d = date!;
  const exits = p.gates.flatMap((g) => g.cells);
  const obstacles = p.obstacles.filter((o) => presentOn(o, d)).flatMap((o) => o.cells);
  const names = new Map<string, string>();
  for (const i of [...p.items, ...left.data!.items, ...(right.data?.items ?? [])])
    if (i.vessel?.name) names.set(i.allocation_id, i.vessel.name);
  const toBoats = (items: DraftItem[], present: (id: string) => boolean): PlanBoat[] => {
    const st = draftReducer(EMPTY_DRAFT, { type: 'load', grid, items });
    return st.order
      .map((id) => st.items[id]!)
      .filter((it) => it.at && present(it.allocationId))
      .map((it) => ({
        item: it,
        label: names.get(it.allocationId) ?? it.allocationId.slice(0, 8),
        token: 'annual',
      }));
  };
  const leftItems = left.data!.items;
  const leftBoats = toBoats(scenarioDraftItems(leftItems), (id) => {
    const m = leftItems.find((i) => i.allocation_id === id);
    return !m || presentOn(m, d);
  });
  const rightBoats = right.data
    ? toBoats(scenarioDraftItems(right.data.items), (id) => {
        const m = right.data!.items.find((i) => i.allocation_id === id);
        return !m || presentOn(m, d);
      })
    : toBoats(planDraftItems(p.items), (id) => {
        const m = p.items.find((i) => i.allocation_id === id);
        return !m || presentOn(m, d);
      });
  const rows = compareRows(cmp.data);
  const rightTitle = right.data ? right.data.scenario.name : t('storage:compare.real');
  const fmt = (k: keyof StorageIndicators, ind: StorageIndicators | undefined) => {
    const v = ind?.[k];
    if (typeof v !== 'number') return '—';
    return k.endsWith('_cm2')
      ? new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(v / 10_000)
      : String(v);
  };

  return (
    <RequirePermission allowed={can('storage.read')}>
      <PageHeader
        title={t('storage:compare.title', { name: s!.name })}
        actions={
          <Button variant="outline" asChild>
            <Link
              to="/m/$marinaId/storage/$areaId/scenarios/$scenarioId"
              params={{ marinaId, areaId, scenarioId }}
            >
              {t('storage:compare.back')}
            </Link>
          </Button>
        }
      />
      <div className="mb-4 flex flex-wrap gap-4">
        <Field className="max-w-72">
          <FieldLabel htmlFor="compare-with">{t('storage:compare.with')}</FieldLabel>
          <Select
            value={search.with ?? 'real'}
            onValueChange={(v) => void navigate({ search: { ...search, with: v }, replace: true })}
          >
            <SelectTrigger id="compare-with">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="real">{t('storage:compare.real')}</SelectItem>
              {(others.data?.items ?? [])
                .filter((o) => o.id !== scenarioId)
                .map((o) => (
                  <SelectItem key={o.id} value={o.id}>
                    {o.name}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        </Field>
        <Field className="max-w-48">
          <FieldLabel htmlFor="compare-date">{t('storage:compare.date')}</FieldLabel>
          <DatePicker
            id="compare-date"
            value={d}
            onChange={(x) =>
              void navigate({ search: { ...search, ...(x ? { date: x } : {}) }, replace: true })
            }
          />
        </Field>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        {[
          { title: s!.name, boats: leftBoats, key: 'left' },
          { title: rightTitle, boats: rightBoats, key: 'right' },
        ].map((side) => (
          <Card key={side.key} className="gap-2 py-4">
            <CardHeader className="px-4">
              <CardTitle className="text-base">{side.title}</CardTitle>
            </CardHeader>
            <CardContent className="px-4">
              <PlanGrid
                grid={grid}
                boats={side.boats}
                exits={exits}
                obstacles={obstacles}
                readOnly
                ariaLabel={t('storage:planner.planLabel', { date: formatIsoDate(d) })}
                boatLabel={(b) => b.label}
              />
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Table data-testid="compare-indicators">
          <TableHeader>
            <TableRow>
              <TableHead>{t('storage:compare.indicator')}</TableHead>
              <TableHead className="text-right">{s!.name}</TableHead>
              <TableHead className="text-right">{rightTitle}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {INDICATORS.map((k) => (
              <TableRow key={k}>
                <TableCell>{t(`storage:compare.ind.${k}`)}</TableCell>
                <TableCell className="text-right tabular-nums">{fmt(k, cmp.data?.left)}</TableCell>
                <TableCell className="text-right tabular-nums">{fmt(k, cmp.data?.right)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <Table data-testid="compare-moves">
          <TableHeader>
            <TableRow>
              <TableHead>{t('storage:compare.boat')}</TableHead>
              <TableHead>{t('storage:compare.change')}</TableHead>
              <TableHead>{s!.name}</TableHead>
              <TableHead>{rightTitle}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="text-muted-foreground">
                  {t('storage:compare.noDiff')}
                </TableCell>
              </TableRow>
            ) : (
              rows.map((r) => (
                <TableRow key={r.allocationId}>
                  <TableCell>{names.get(r.allocationId) ?? r.allocationId.slice(0, 8)}</TableCell>
                  <TableCell>{t(`storage:compare.changes.${r.change}`)}</TableCell>
                  <TableCell className="tabular-nums">
                    {positionText(r.left, i18n.language)}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {positionText(r.right, i18n.language)}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </RequirePermission>
  );
}
