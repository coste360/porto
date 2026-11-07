import { PlusIcon, TrashIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useActivateFeeTable,
  useCreateFeeRun,
  useCreateFeeTable,
  useFeeRunLines,
  useFeeRuns,
  useFeeTables,
  useIssueFeeRun,
  useSimulateFeeRun,
  useUpdateFeeTable,
  type FeeRun,
  type FeeTable,
  type FeeTableRow,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { LengthInput, MoneyInput, PageHeader, PercentInput } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  EnumSelect,
  M4ProblemAlert,
  Money,
  PageFrame,
  Section,
} from '../contracts-m4/components/kit';
import { useMarinaToday } from '../registry/lib/today';

const BASES: readonly Schemas['FeeBasis'][] = ['per_category', 'per_m2_berth', 'per_length_band'];

/** Management fees (spec 14.4): fee tables of a year and the run wizard simulate → totals/diff/excluded → issue. */
export function FeesPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [year, setYear] = useState(Number(today.slice(0, 4)));
  const tables = useFeeTables(marinaId, year);
  const runs = useFeeRuns(marinaId, year);
  const create = useCreateFeeTable(marinaId);
  const [editing, setEditing] = useState<string | null>(null);
  const [basis, setBasis] = useState<Schemas['FeeBasis'] | ''>('per_category');
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        <PageHeader
          title={t('rights:fees.title')}
          description={t('rights:fees.description')}
          actions={
            <Button
              disabled={create.isPending}
              onClick={() =>
                create.mutate(
                  {
                    year,
                    name: t('rights:fees.defaultName', { year }),
                    basis: basis || 'per_category',
                    rows: [],
                  },
                  { onSuccess: (tb) => setEditing(tb.id) },
                )
              }
            >
              <PlusIcon />
              {t('rights:fees.newTable')}
            </Button>
          }
        />
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="fees-basis"
            label={t('rights:fees.basis')}
            group="feeBasis"
            values={BASES}
            value={basis}
            onChange={setBasis}
          />
          <Field className="w-32">
            <FieldLabel htmlFor="fees-year">{t('rights:fees.year')}</FieldLabel>
            <Input
              id="fees-year"
              inputMode="numeric"
              value={String(year)}
              onChange={(e) => /^\d{4}$/.test(e.target.value) && setYear(Number(e.target.value))}
            />
          </Field>
        </div>
        <M4ProblemAlert error={create.error} />
        <Section title={t('rights:fees.tables')}>
          {tables.isPending ? (
            <LoadingState rows={2} />
          ) : tables.isError ? (
            <QueryErrorState error={tables.error} onRetry={() => void tables.refetch()} />
          ) : tables.data.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('rights:fees.noTables')}</p>
          ) : (
            tables.data.map((tb) =>
              editing === tb.id && tb.status === 'draft' ? (
                <FeeTableEditor key={tb.id} table={tb} onDone={() => setEditing(null)} />
              ) : (
                <FeeTableSummary key={tb.id} table={tb} onEdit={() => setEditing(tb.id)} />
              ),
            )
          )}
        </Section>
        <RunsSection year={year} tables={tables.data ?? []} runs={runs.data ?? []} />
      </PageFrame>
    </RequirePermission>
  );
}

function FeeTableSummary({ table, onEdit }: { table: FeeTable; onEdit: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const activate = useActivateFeeTable(marinaId);
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md border p-2">
      <span className="font-medium">{table.name}</span>
      <Badge variant="outline">{t(`docsEnums:feeBasis.${table.basis}`)}</Badge>
      <Badge variant={table.status === 'active' ? 'default' : 'secondary'}>
        {t(`rights:fees.status.${table.status}`)}
      </Badge>
      <span className="text-muted-foreground text-sm">
        {t('rights:fees.rows', { count: table.rows.length })}
      </span>
      {table.status === 'draft' ? (
        <>
          <Button size="sm" variant="ghost" onClick={onEdit}>
            {t('common:actions.edit')}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={activate.isPending}
            onClick={() => activate.mutate({ id: table.id, version: table.version })}
          >
            {t('rights:fees.activate')}
          </Button>
        </>
      ) : null}
      <M4ProblemAlert error={activate.error} />
    </div>
  );
}

function FeeTableEditor({ table, onDone }: { table: FeeTable; onDone: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateFeeTable(marinaId);
  const [name, setName] = useState(table.name);
  const [istat, setIstat] = useState<number | null>(table.istat_bp);
  const [rows, setRows] = useState<FeeTableRow[]>(table.rows);
  const set = (i: number, r: Partial<FeeTableRow>) =>
    setRows(rows.map((x, j) => (j === i ? { ...x, ...r } : x)));
  return (
    <div className="flex flex-col gap-3 rounded-md border p-3">
      <div className="flex flex-wrap gap-3">
        <Field className="flex-1">
          <FieldLabel htmlFor={`fee-name-${table.id}`}>{t('rights:fees.name')}</FieldLabel>
          <Input
            id={`fee-name-${table.id}`}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field className="w-36">
          <FieldLabel htmlFor={`fee-istat-${table.id}`}>{t('rights:fees.istat')}</FieldLabel>
          <PercentInput
            id={`fee-istat-${table.id}`}
            locale={i18n.language}
            value={istat}
            onChange={setIstat}
          />
        </Field>
      </div>
      {rows.map((r, i) => (
        <div key={i} className="flex flex-wrap items-end gap-2">
          {table.basis === 'per_category' ? (
            <Field className="w-56">
              <FieldLabel htmlFor={`fee-cat-${i}`}>{t('rights:fees.category')}</FieldLabel>
              <Input
                id={`fee-cat-${i}`}
                value={r.category_id ?? ''}
                onChange={(e) => set(i, { category_id: e.target.value })}
              />
            </Field>
          ) : null}
          {table.basis === 'per_length_band' ? (
            <>
              <Field className="w-32">
                <FieldLabel htmlFor={`fee-from-${i}`}>{t('rights:fees.fromCm')}</FieldLabel>
                <LengthInput
                  id={`fee-from-${i}`}
                  locale={i18n.language}
                  value={r.from_cm ?? null}
                  onChange={(v) => set(i, { from_cm: v ?? 0 })}
                />
              </Field>
              <Field className="w-32">
                <FieldLabel htmlFor={`fee-to-${i}`}>{t('rights:fees.toCm')}</FieldLabel>
                <LengthInput
                  id={`fee-to-${i}`}
                  locale={i18n.language}
                  value={r.to_cm ?? null}
                  onChange={(v) => set(i, { to_cm: v ?? 0 })}
                />
              </Field>
            </>
          ) : null}
          {table.basis === 'per_m2_berth' ? (
            <Field className="w-44">
              <FieldLabel htmlFor={`fee-rate-${i}`}>{t('rights:fees.ratePerM2')}</FieldLabel>
              <MoneyInput
                id={`fee-rate-${i}`}
                locale={i18n.language}
                value={r.rate_cents_per_m2 ?? null}
                onChange={(v) => set(i, { rate_cents_per_m2: v ?? 0 })}
              />
            </Field>
          ) : (
            <Field className="w-44">
              <FieldLabel htmlFor={`fee-amount-${i}`}>{t('rights:fees.amount')}</FieldLabel>
              <MoneyInput
                id={`fee-amount-${i}`}
                locale={i18n.language}
                value={r.amount_cents ?? null}
                onChange={(v) => set(i, { amount_cents: v ?? 0 })}
              />
            </Field>
          )}
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('rights:fees.removeRow')}
            onClick={() => setRows(rows.filter((_, j) => j !== i))}
          >
            <TrashIcon />
          </Button>
        </div>
      ))}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" onClick={() => setRows([...rows, {}])}>
          <PlusIcon />
          {t('rights:fees.addRow')}
        </Button>
        <Button
          size="sm"
          disabled={update.isPending}
          onClick={() =>
            update.mutate(
              {
                id: table.id,
                version: table.version,
                patch: { name: name.trim(), istat_bp: istat ?? 0, rows },
              },
              { onSuccess: onDone },
            )
          }
        >
          {t('common:actions.save')}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDone}>
          {t('common:actions.cancel')}
        </Button>
      </div>
      <M4ProblemAlert error={update.error} />
    </div>
  );
}

function RunsSection({ year, tables, runs }: { year: number; tables: FeeTable[]; runs: FeeRun[] }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreateFeeRun(marinaId);
  const active = tables.filter((x) => x.status === 'active');
  const [tableId, setTableId] = useState('');
  return (
    <Section title={t('rights:fees.runs')} description={t('rights:fees.runsHint')}>
      <div className="flex flex-wrap items-end gap-2">
        <EnumSelect
          id="fee-run-table"
          label={t('rights:fees.table')}
          values={active.map((x) => x.id)}
          labelOf={(id) => active.find((x) => x.id === id)?.name ?? id}
          value={tableId}
          onChange={setTableId}
        />
        <Button
          disabled={!tableId || create.isPending}
          onClick={() =>
            create.mutate({
              year,
              table_id: tableId,
              plan: { schedule: { mode: 'single', due_offset_days: 30 }, rounding_unit_cents: 1 },
            })
          }
        >
          {t('rights:fees.newRun')}
        </Button>
      </div>
      <M4ProblemAlert error={create.error} />
      {runs.map((r) => (
        <FeeRunCard key={r.id} run={r} />
      ))}
    </Section>
  );
}

function FeeRunCard({ run }: { run: FeeRun }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const simulate = useSimulateFeeRun(marinaId);
  const issue = useIssueFeeRun(marinaId);
  const lines = useFeeRunLines(marinaId, run.id, run.status !== 'draft');
  const sim = run.totals;
  const v = { id: run.id, version: run.version };
  return (
    <div className="flex flex-col gap-3 rounded-md border p-3" data-testid="fee-run">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="secondary">{t(`docsEnums:feeRunStatus.${run.status}`)}</Badge>
        {run.status === 'draft' || run.status === 'simulated' ? (
          <Button
            size="sm"
            variant="outline"
            disabled={simulate.isPending}
            onClick={() => simulate.mutate(v)}
          >
            {t('rights:fees.simulate')}
          </Button>
        ) : null}
        {run.status === 'simulated' ? (
          <>
            <Button size="sm" disabled={issue.isPending} onClick={() => issue.mutate(v)}>
              {t('rights:fees.issue')}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={issue.isPending}
              onClick={() => issue.mutate({ ...v, discard: true })}
            >
              {t('rights:fees.discard')}
            </Button>
          </>
        ) : null}
      </div>
      <M4ProblemAlert error={simulate.error ?? issue.error} />
      {sim ? (
        <>
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('rights:fees.category')}</TableHead>
                  <TableHead>{t('rights:fees.rights')}</TableHead>
                  <TableHead className="text-right">{t('rights:fees.amount')}</TableHead>
                  <TableHead className="text-right">{t('rights:fees.previous')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sim.totals_by_category.map((c) => (
                  <TableRow key={c.category}>
                    <TableCell>{c.category}</TableCell>
                    <TableCell>{c.rights}</TableCell>
                    <TableCell className="text-right">
                      <Money cents={c.amount_cents} />
                    </TableCell>
                    <TableCell className="text-right">
                      <Money cents={c.previous_cents} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
              <TableFooter>
                <TableRow>
                  <TableCell colSpan={2}>{t('rights:fees.total')}</TableCell>
                  <TableCell className="text-right" data-testid="fee-total">
                    <Money cents={sim.total_cents} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money cents={sim.previous_total_cents} />
                  </TableCell>
                </TableRow>
              </TableFooter>
            </Table>
          </div>
          {sim.diff_previous_cents !== undefined ? (
            <p className="text-sm">
              {t('rights:fees.diff')} <Money cents={sim.diff_previous_cents} />
            </p>
          ) : null}
          {sim.excluded.length > 0 ? (
            <div className="text-sm">
              <p className="font-medium">
                {t('rights:fees.excluded', { count: sim.excluded.length })}
              </p>
              <ul className="text-muted-foreground">
                {sim.excluded.map((e) => (
                  <li key={e.right_id}>{t(`rights:fees.exclusion.${e.reason}`)}</li>
                ))}
              </ul>
            </div>
          ) : null}
        </>
      ) : null}
      {lines.data && run.status === 'issued' ? (
        <p className="text-muted-foreground text-sm">
          {t('rights:fees.lines', { count: lines.data.items.length })}
        </p>
      ) : null}
    </div>
  );
}
