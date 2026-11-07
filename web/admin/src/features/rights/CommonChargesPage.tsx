import { DownloadIcon, FileTextIcon, PlusIcon, TrashIcon, UploadIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  useActivateMillesimalTable,
  useAllBerths,
  useBudgetAction,
  useBudgetItems,
  useBudgets,
  useCommonChargeAllocations,
  useCommonChargeRunAction,
  useCommonChargeRuns,
  useCommonChargeStatements,
  useCreateBudget,
  useCreateBudgetItem,
  useCreateCommonChargeRun,
  useCreateMillesimalTable,
  useDeleteBudgetItem,
  useMillesimalEntries,
  useMillesimalTables,
  useReplaceMillesimalEntries,
  useSetBudgetActuals,
  type Budget,
  type CommonChargeRun,
  type MillesimalTable,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { MoneyInput, PageHeader, PercentInput } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  EnumSelect,
  M4ProblemAlert,
  Money,
  PageFrame,
  Section,
} from '../contracts-m4/components/kit';
import { openBlob, saveBlob } from '../contracts-m4/lib/files';
import { useMarinaToday } from '../registry/lib/today';
import {
  applyCsvToGrid,
  checkGrid,
  gridEntries,
  parseMillesimalCsv,
  type GridRow,
} from './lib/shares';

/** Common charges (spec 14.5): millesimal tables, budgets with items and actuals, runs and statements. */
export function CommonChargesPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        <PageHeader title={t('commonCharges:title')} description={t('commonCharges:description')} />
        <Tabs defaultValue="tables">
          <TabsList>
            <TabsTrigger value="tables">{t('commonCharges:tabs.tables')}</TabsTrigger>
            <TabsTrigger value="budgets">{t('commonCharges:tabs.budgets')}</TabsTrigger>
          </TabsList>
          <TabsContent value="tables">
            <TablesTab />
          </TabsContent>
          <TabsContent value="budgets">
            <BudgetsTab />
          </TabsContent>
        </Tabs>
      </PageFrame>
    </RequirePermission>
  );
}

// ---- millesimal tables ----

function TablesTab() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const tables = useMillesimalTables(marinaId);
  const create = useCreateMillesimalTable(marinaId);
  const [selected, setSelected] = useState('');
  const [name, setName] = useState('');
  const [code, setCode] = useState('');
  const current = tables.data?.find((x) => x.id === selected) ?? tables.data?.[0];
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-2">
        <Field className="w-56">
          <FieldLabel htmlFor="mt-name">{t('commonCharges:tables.name')}</FieldLabel>
          <Input
            id="mt-name"
            value={name}
            maxLength={120}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field className="w-32">
          <FieldLabel htmlFor="mt-code">{t('commonCharges:tables.code')}</FieldLabel>
          <Input
            id="mt-code"
            value={code}
            maxLength={20}
            onChange={(e) => setCode(e.target.value)}
          />
        </Field>
        <Button
          disabled={!name.trim() || !code.trim() || create.isPending}
          onClick={() =>
            create.mutate(
              { name: name.trim(), code: code.trim() },
              {
                onSuccess: (x) => {
                  setSelected(x.id);
                  setName('');
                  setCode('');
                },
              },
            )
          }
        >
          <PlusIcon />
          {t('commonCharges:tables.new')}
        </Button>
      </div>
      <M4ProblemAlert error={create.error} />
      {tables.isPending ? (
        <LoadingState rows={3} />
      ) : tables.isError ? (
        <QueryErrorState error={tables.error} onRetry={() => void tables.refetch()} />
      ) : tables.data.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('commonCharges:tables.empty')}</p>
      ) : (
        <>
          <div className="flex flex-wrap gap-2">
            {tables.data.map((x) => (
              <Button
                key={x.id}
                size="sm"
                variant={current?.id === x.id ? 'default' : 'outline'}
                onClick={() => setSelected(x.id)}
              >
                {`${x.code} · ${x.name}`}
                <Badge variant="secondary">
                  {t(`docsEnums:millesimalTableStatus.${x.status}`)}
                </Badge>
              </Button>
            ))}
          </div>
          {current ? <MillesimalGrid key={current.id} table={current} /> : null}
        </>
      )}
    </div>
  );
}

/** Grid editor of a table: one row per berth, live Σ, CSV import, save and activate at Σ = 1000 (INV-41). */
function MillesimalGrid({ table }: { table: MillesimalTable }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const berths = useAllBerths(marinaId);
  const entries = useMillesimalEntries(marinaId, table.id);
  const save = useReplaceMillesimalEntries(marinaId);
  const activate = useActivateMillesimalTable(marinaId);
  const [edited, setRows] = useState<GridRow[] | null>(null);
  const [csvReport, setCsvReport] = useState<{ rejected: number; unknown: string[] } | null>(null);
  const readOnly = table.status !== 'draft';
  const base = useMemo(() => {
    if (!berths.data || !entries.data) return null;
    const byBerth = new Map(entries.data.entries.map((e) => [e.berth_id, e.millis]));
    return berths.data.map((b) => ({
      berthId: b.id,
      berthCode: b.code,
      text: String(byBerth.get(b.id) ?? ''),
    }));
  }, [berths.data, entries.data]);
  const rows = edited ?? base;
  if (berths.isPending || entries.isPending || !rows) return <LoadingState rows={6} />;
  if (berths.isError || entries.isError)
    return <QueryErrorState error={berths.error ?? entries.error} />;
  const check = checkGrid(rows);
  return (
    <Section
      title={`${table.code} · ${table.name}`}
      description={readOnly ? t('commonCharges:tables.readOnly') : t('commonCharges:tables.hint')}
      action={
        <span
          className={
            check.complete ? 'text-sm font-semibold' : 'text-destructive text-sm font-semibold'
          }
          data-testid="millesimal-sum"
          aria-live="polite"
        >
          {t('commonCharges:tables.sum', { sum: check.sum, remaining: check.remaining })}
        </span>
      }
    >
      {!readOnly ? (
        <div className="flex flex-wrap items-end gap-2">
          <Field className="w-auto">
            <FieldLabel htmlFor="mt-csv">{t('commonCharges:tables.csv')}</FieldLabel>
            <Input
              id="mt-csv"
              type="file"
              accept=".csv,text/csv"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (!f) return;
                void f.text().then((text) => {
                  const parsed = parseMillesimalCsv(text);
                  const applied = applyCsvToGrid(rows, parsed.entries);
                  setRows(applied.rows);
                  setCsvReport({ rejected: parsed.rejected.length, unknown: applied.unknown });
                });
              }}
            />
          </Field>
          <Button
            disabled={save.isPending || check.invalid.length > 0}
            onClick={() =>
              save.mutate(
                { id: table.id, version: table.version, body: { entries: gridEntries(rows) } },
                { onSuccess: () => toast.success(t('commonCharges:tables.saved')) },
              )
            }
          >
            <UploadIcon />
            {t('common:actions.save')}
          </Button>
          <Button
            variant="outline"
            disabled={!check.complete || activate.isPending}
            onClick={() => activate.mutate({ id: table.id, version: table.version })}
          >
            {t('commonCharges:tables.activate')}
          </Button>
        </div>
      ) : null}
      {csvReport ? (
        <p className="text-sm" role="status">
          {t('commonCharges:csv.report', {
            rejected: csvReport.rejected,
            unknown: csvReport.unknown.length,
          })}
          {csvReport.unknown.length ? ` ${csvReport.unknown.slice(0, 10).join(', ')}` : ''}
        </p>
      ) : null}
      {check.invalid.length > 0 ? (
        <FieldError>{t('commonCharges:tables.invalidCells')}</FieldError>
      ) : null}
      <M4ProblemAlert error={save.error ?? activate.error} />
      <div className="grid gap-2 sm:grid-cols-3 lg:grid-cols-6">
        {rows.map((r, i) => (
          <Field key={r.berthId} data-invalid={check.invalid.includes(i) || undefined}>
            <FieldLabel htmlFor={`mt-cell-${r.berthId}`}>{r.berthCode}</FieldLabel>
            <Input
              id={`mt-cell-${r.berthId}`}
              inputMode="numeric"
              disabled={readOnly}
              value={r.text}
              onChange={(e) =>
                setRows(rows.map((x, j) => (j === i ? { ...x, text: e.target.value } : x)))
              }
            />
          </Field>
        ))}
      </div>
    </Section>
  );
}

// ---- budgets, items, actuals, runs ----

function BudgetsTab() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const budgets = useBudgets(marinaId);
  const create = useCreateBudget(marinaId);
  const [selected, setSelected] = useState('');
  const current = budgets.data?.find((b) => b.id === selected) ?? budgets.data?.[0];
  return (
    <div className="flex flex-col gap-4">
      <div>
        <Button
          disabled={create.isPending}
          onClick={() =>
            create.mutate(
              { year: Number(today.slice(0, 4)) },
              { onSuccess: (b) => setSelected(b.id) },
            )
          }
        >
          <PlusIcon />
          {t('commonCharges:budgets.new')}
        </Button>
      </div>
      <M4ProblemAlert error={create.error} />
      {budgets.isPending ? (
        <LoadingState rows={3} />
      ) : budgets.isError ? (
        <QueryErrorState error={budgets.error} onRetry={() => void budgets.refetch()} />
      ) : budgets.data.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('commonCharges:budgets.empty')}</p>
      ) : (
        <>
          <div className="flex flex-wrap gap-2">
            {budgets.data.map((b) => (
              <Button
                key={b.id}
                size="sm"
                variant={current?.id === b.id ? 'default' : 'outline'}
                onClick={() => setSelected(b.id)}
              >
                {`${b.year} · ${b.name}`}
              </Button>
            ))}
          </div>
          {current ? <BudgetDetail key={current.id} budget={current} /> : null}
        </>
      )}
    </div>
  );
}

function BudgetDetail({ budget }: { budget: Budget }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const items = useBudgetItems(marinaId, budget.id);
  const tables = useMillesimalTables(marinaId);
  const addItem = useCreateBudgetItem(marinaId);
  const delItem = useDeleteBudgetItem(marinaId);
  const action = useBudgetAction(marinaId);
  const actuals = useSetBudgetActuals(marinaId);
  const [name, setName] = useState('');
  const [tableId, setTableId] = useState('');
  const [cents, setCents] = useState<number | null>(null);
  const [actual, setActual] = useState<Record<string, number | null>>({});
  const activeTables = (tables.data ?? []).filter((x) => x.status === 'active');
  const total = (items.data ?? []).reduce((s, i) => s + i.budget_cents, 0);
  return (
    <div className="flex flex-col gap-4">
      <Section
        title={t('commonCharges:budgets.items')}
        description={t(`docsEnums:budgetStatus.${budget.status}`)}
        action={
          <div className="flex gap-2">
            {budget.status === 'draft' ? (
              <Button
                size="sm"
                disabled={action.isPending}
                onClick={() =>
                  action.mutate({ id: budget.id, version: budget.version, action: 'approve' })
                }
              >
                {t('commonCharges:budgets.approve')}
              </Button>
            ) : null}
            {budget.status === 'approved' ? (
              <Button
                size="sm"
                variant="outline"
                disabled={action.isPending}
                onClick={() =>
                  action.mutate({ id: budget.id, version: budget.version, action: 'close' })
                }
              >
                {t('commonCharges:budgets.close')}
              </Button>
            ) : null}
          </div>
        }
      >
        <M4ProblemAlert error={action.error ?? addItem.error ?? delItem.error ?? actuals.error} />
        {items.isPending ? (
          <LoadingState rows={3} />
        ) : items.isError ? (
          <QueryErrorState error={items.error} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('commonCharges:budgets.item')}</TableHead>
                  <TableHead>{t('commonCharges:budgets.table')}</TableHead>
                  <TableHead className="text-right">{t('commonCharges:budgets.budget')}</TableHead>
                  <TableHead className="text-right">{t('commonCharges:budgets.actual')}</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.data.map((it) => (
                  <TableRow key={it.id}>
                    <TableCell>{it.name}</TableCell>
                    <TableCell>
                      {tables.data?.find((x) => x.id === it.table_id)?.code ?? ''}
                    </TableCell>
                    <TableCell className="text-right">
                      <Money cents={it.budget_cents} />
                    </TableCell>
                    <TableCell className="text-right">
                      {budget.status === 'approved' ? (
                        <MoneyInput
                          aria-label={t('commonCharges:budgets.actualOf', { name: it.name })}
                          locale={i18n.language}
                          value={actual[it.id] ?? it.actual_cents ?? null}
                          onChange={(v) => setActual({ ...actual, [it.id]: v })}
                        />
                      ) : (
                        <Money cents={it.actual_cents} />
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {budget.status === 'draft' ? (
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={t('commonCharges:budgets.removeItem', { name: it.name })}
                          onClick={() =>
                            delItem.mutate({ id: it.id, version: it.version, budgetId: budget.id })
                          }
                        >
                          <TrashIcon />
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
              <TableFooter>
                <TableRow>
                  <TableCell colSpan={2}>{t('commonCharges:budgets.total')}</TableCell>
                  <TableCell className="text-right">
                    <Money cents={total} />
                  </TableCell>
                  <TableCell colSpan={2} />
                </TableRow>
              </TableFooter>
            </Table>
          </div>
        )}
        {budget.status === 'approved' && Object.keys(actual).length > 0 ? (
          <div>
            <Button
              size="sm"
              disabled={actuals.isPending}
              onClick={() =>
                actuals.mutate(
                  {
                    id: budget.id,
                    version: budget.version,
                    body: {
                      items: Object.entries(actual)
                        .filter(([, v]) => v !== null)
                        .map(([item_id, v]) => ({ item_id, actual_cents: v! })),
                    },
                  },
                  { onSuccess: () => setActual({}) },
                )
              }
            >
              {t('commonCharges:budgets.saveActuals')}
            </Button>
          </div>
        ) : null}
        {budget.status === 'draft' ? (
          <div className="flex flex-wrap items-end gap-2">
            <Field className="w-56">
              <FieldLabel htmlFor="item-name">{t('commonCharges:budgets.item')}</FieldLabel>
              <Input id="item-name" value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <EnumSelect
              id="item-table"
              label={t('commonCharges:budgets.table')}
              values={activeTables.map((x) => x.id)}
              labelOf={(id) => activeTables.find((x) => x.id === id)?.code ?? id}
              value={tableId}
              onChange={setTableId}
            />
            <Field className="w-44">
              <FieldLabel htmlFor="item-cents">{t('commonCharges:budgets.budget')}</FieldLabel>
              <MoneyInput
                id="item-cents"
                locale={i18n.language}
                value={cents}
                onChange={setCents}
              />
            </Field>
            <Button
              disabled={!name.trim() || !tableId || cents === null || addItem.isPending}
              onClick={() =>
                cents !== null &&
                addItem.mutate(
                  {
                    budgetId: budget.id,
                    body: { name: name.trim(), table_id: tableId, budget_cents: cents },
                  },
                  {
                    onSuccess: () => {
                      setName('');
                      setCents(null);
                    },
                  },
                )
              }
            >
              <PlusIcon />
              {t('commonCharges:budgets.addItem')}
            </Button>
          </div>
        ) : null}
      </Section>
      {budget.status !== 'draft' ? <RunsSection budget={budget} /> : null}
    </div>
  );
}

function RunsSection({ budget }: { budget: Budget }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const runs = useCommonChargeRuns(marinaId, budget.id);
  const create = useCreateCommonChargeRun(marinaId);
  const [kind, setKind] = useState<'advance' | 'balance' | ''>('advance');
  const [share, setShare] = useState<number | null>(5000);
  return (
    <Section title={t('commonCharges:runs.title')} description={t('commonCharges:runs.hint')}>
      <div className="flex flex-wrap items-end gap-2">
        <EnumSelect
          id="run-kind"
          label={t('commonCharges:runs.kind')}
          group="commonRunKind"
          values={['advance', 'balance'] as const}
          value={kind}
          onChange={setKind}
        />
        {kind === 'advance' ? (
          <Field className="w-36">
            <FieldLabel htmlFor="run-share">{t('commonCharges:runs.share')}</FieldLabel>
            <PercentInput id="run-share" locale={i18n.language} value={share} onChange={setShare} />
          </Field>
        ) : null}
        <Button
          disabled={!kind || (kind === 'advance' && share === null) || create.isPending}
          onClick={() =>
            kind &&
            create.mutate({
              budgetId: budget.id,
              body: { kind, ...(kind === 'advance' && share !== null ? { share_bp: share } : {}) },
            })
          }
        >
          {t('commonCharges:runs.new')}
        </Button>
      </div>
      <M4ProblemAlert error={create.error} />
      {runs.isPending ? (
        <LoadingState rows={2} />
      ) : (
        (runs.data ?? []).map((r) => <RunCard key={r.id} run={r} />)
      )}
    </Section>
  );
}

function RunCard({ run }: { run: CommonChargeRun }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const action = useCommonChargeRunAction(marinaId);
  const statements = useCommonChargeStatements(marinaId);
  const [open, setOpen] = useState(false);
  const allocations = useCommonChargeAllocations(marinaId, run.id, open);
  const allocated = run.totals_by_item.reduce((s, i) => s + i.allocated_cents, 0);
  return (
    <div className="flex flex-col gap-3 rounded-md border p-3" data-testid="common-run">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{`${t(`docsEnums:commonRunKind.${run.kind}`)} ${run.seq}`}</span>
        <Badge variant="secondary">{t(`docsEnums:commonRunStatus.${run.status}`)}</Badge>
        <span className="text-sm">{t('commonCharges:runs.holders', { count: run.holders })}</span>
        {run.status === 'draft' ? (
          <>
            <Button
              size="sm"
              disabled={action.isPending}
              onClick={() => action.mutate({ id: run.id, version: run.version, action: 'issue' })}
            >
              {t('commonCharges:runs.issue')}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={action.isPending}
              onClick={() => action.mutate({ id: run.id, version: run.version, action: 'discard' })}
            >
              {t('commonCharges:runs.discard')}
            </Button>
          </>
        ) : null}
        <Button
          size="sm"
          variant="outline"
          disabled={statements.isPending}
          onClick={() =>
            statements.mutate({ runId: run.id, format: 'pdf', by: 'item' }, { onSuccess: openBlob })
          }
        >
          <FileTextIcon />
          {t('commonCharges:runs.byItem')}
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={statements.isPending}
          onClick={() =>
            statements.mutate(
              { runId: run.id, format: 'csv' },
              { onSuccess: (b) => saveBlob(`ripartizione-${today}.csv`, b) },
            )
          }
        >
          <DownloadIcon />
          {t('commonCharges:runs.csv')}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setOpen(!open)}>
          {open ? t('commonCharges:runs.hideAllocations') : t('commonCharges:runs.showAllocations')}
        </Button>
      </div>
      <M4ProblemAlert error={action.error ?? statements.error} />
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('commonCharges:budgets.item')}</TableHead>
              <TableHead className="text-right">{t('commonCharges:runs.amount')}</TableHead>
              <TableHead className="text-right">{t('commonCharges:runs.allocated')}</TableHead>
              <TableHead className="text-right">{t('commonCharges:runs.marinaShare')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {run.totals_by_item.map((i) => (
              <TableRow key={i.item_id}>
                <TableCell>{i.name}</TableCell>
                <TableCell className="text-right">
                  <Money cents={i.amount_cents} />
                </TableCell>
                <TableCell className="text-right">
                  <Money cents={i.allocated_cents} />
                </TableCell>
                <TableCell className="text-right">
                  <Money cents={i.marina_share_cents} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
          <TableFooter>
            <TableRow>
              <TableCell>{t('commonCharges:runs.total')}</TableCell>
              <TableCell className="text-right" data-testid="run-total">
                <Money cents={run.total_cents} />
              </TableCell>
              <TableCell className="text-right" data-testid="run-allocated">
                <Money cents={allocated} />
              </TableCell>
              <TableCell />
            </TableRow>
          </TableFooter>
        </Table>
      </div>
      {open && allocations.data ? (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('commonCharges:runs.berth')}</TableHead>
                <TableHead>{t('commonCharges:runs.holder')}</TableHead>
                <TableHead className="text-right">{t('commonCharges:runs.berthShare')}</TableHead>
                <TableHead className="text-right">{t('commonCharges:runs.amount')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {allocations.data.items.map((a) => (
                <TableRow key={a.id}>
                  <TableCell>{a.berth.resource_id}</TableCell>
                  <TableCell>
                    {a.customer?.display_name ?? t('commonCharges:runs.marina')}
                  </TableCell>
                  <TableCell className="text-right">
                    <Money cents={a.berth_share_cents} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money cents={a.amount_cents} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}
    </div>
  );
}
