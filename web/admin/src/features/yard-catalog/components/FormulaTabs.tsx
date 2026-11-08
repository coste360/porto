import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  formulaIssuesOf,
  useCreateFormulaTable,
  useCreateFormulaVariable,
  useDeleteFormulaVariable,
  useFormulaTables,
  useFormulaVariables,
  usePutYardSettings,
  useUpdateFormulaTable,
  useUpdateFormulaVariable,
  useYardSettings,
  type FormulaTable,
  type FormulaVariable,
  type YardSettings,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatQtyMilli, parseQtyToMilli } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { LengthInput, PercentInput } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Textarea } from '@porto/shared/ui/textarea';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, M4ProblemAlert, Section } from '../../contracts-m4/components/kit';
import { valueText } from '../../yard-quotes/lib';
import { i18nBody, parseBandRows } from '../lib';
import { FormulaField } from './FormulaField';
import { IssueList } from './IssueList';
import { NameFields } from './TaxonomyTabs';

const UNITS = ['m', 'm2', 't', 'kw', 'h', 'pcs', 'l', 'pct', 'none'] as const;
type Unit = (typeof UNITS)[number];
const KINDS = ['number', 'bool', 'enum'] as const;
type Kind = (typeof KINDS)[number];
const HULL_TYPES = [
  'monohull_motor',
  'monohull_sail',
  'catamaran',
  'trimaran',
  'rib',
  'gozzo',
  'other',
] as const;
type Hull = (typeof HULL_TYPES)[number];

// ---- variables ----

export function VariablesTab({ marinaId, canManage }: { marinaId: string; canManage: boolean }) {
  const { t, i18n } = useT();
  const list = useFormulaVariables(marinaId);
  const del = useDeleteFormulaVariable(marinaId);
  const [editing, setEditing] = useState<FormulaVariable | 'derived' | 'job' | null>(null);
  if (list.isPending) return <LoadingState rows={6} />;
  if (list.isError)
    return <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />;
  const builtins = list.data.items.filter((v) => v.builtin);
  const own = list.data.items.filter((v) => !v.builtin);
  const head = [
    t('yardCatalog:variables.name'),
    t('yardCatalog:variables.source'),
    t('yardCatalog:variables.unit'),
    t('yardCatalog:variables.expression'),
    t('yardCatalog:variables.testValue'),
    '',
  ];
  return (
    <div className="flex flex-col gap-4">
      <Section title={t('yardCatalog:variables.own')}>
        {canManage ? (
          <div className="flex gap-2">
            <Button onClick={() => setEditing('derived')}>
              <PlusIcon />
              {t('yardCatalog:variables.newDerived')}
            </Button>
            <Button variant="outline" onClick={() => setEditing('job')}>
              <PlusIcon />
              {t('yardCatalog:variables.newJob')}
            </Button>
          </div>
        ) : null}
        <SimpleTable head={head} empty={t('yardCatalog:variables.empty')} rows={own.length}>
          {own.map((v) => (
            <TableRow key={v.name} data-testid="variable-row">
              <TableCell className="font-mono">{v.name}</TableCell>
              <TableCell>{t(`yardCatalog:sources.${v.source}`)}</TableCell>
              <TableCell>{t(`yardCatalog:units.${v.unit}`)}</TableCell>
              <TableCell className="font-mono text-xs">{v.expression ?? ''}</TableCell>
              <TableCell className="tabular-nums">
                {valueText(v.test_value, i18n.language)}
              </TableCell>
              <TableCell className="flex justify-end gap-2">
                {canManage ? (
                  <>
                    <Button size="sm" variant="outline" onClick={() => setEditing(v)}>
                      {t('yardCatalog:edit')}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={del.isPending}
                      onClick={() => v.id && del.mutate({ id: v.id, version: v.version ?? 0 })}
                    >
                      {t('common:actions.delete')}
                    </Button>
                  </>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
        <M4ProblemAlert error={del.error} />
      </Section>
      <Section
        title={t('yardCatalog:variables.builtin')}
        description={t('yardCatalog:variables.builtinHint')}
      >
        <SimpleTable head={head.slice(0, 5)} empty="" rows={builtins.length}>
          {builtins.map((v) => (
            <TableRow key={v.name}>
              <TableCell className="font-mono">{v.name}</TableCell>
              <TableCell>{t(`yardCatalog:sources.${v.source}`)}</TableCell>
              <TableCell>{t(`yardCatalog:units.${v.unit}`)}</TableCell>
              <TableCell>{v.label.it}</TableCell>
              <TableCell className="tabular-nums">
                {valueText(v.test_value, i18n.language)}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      </Section>
      {editing ? (
        <VariableDialog
          marinaId={marinaId}
          variable={typeof editing === 'string' ? null : editing}
          source={
            typeof editing === 'string' ? editing : editing.source === 'job' ? 'job' : 'derived'
          }
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

function milliText(v: number | undefined, locale: string) {
  return v === undefined ? '' : formatQtyMilli(v, locale);
}

function VariableDialog({
  marinaId,
  variable,
  source,
  onClose,
}: {
  marinaId: string;
  variable: FormulaVariable | null;
  source: 'derived' | 'job';
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const loc = i18n.language;
  const create = useCreateFormulaVariable(marinaId);
  const update = useUpdateFormulaVariable(marinaId);
  const [name, setName] = useState(variable?.name ?? '');
  const [kind, setKind] = useState<Kind | ''>((variable?.kind as Kind | undefined) ?? 'number');
  const [unit, setUnit] = useState<Unit | ''>((variable?.unit as Unit | undefined) ?? 'none');
  const [expression, setExpression] = useState(variable?.expression ?? '');
  const [min, setMin] = useState(milliText(variable?.min_milli, loc));
  const [max, setMax] = useState(milliText(variable?.max_milli, loc));
  const [def, setDef] = useState(milliText(variable?.default_milli, loc));
  const [enumValues, setEnumValues] = useState((variable?.enum_values ?? []).join(', '));
  const [defEnum, setDefEnum] = useState(variable?.default_enum ?? '');
  const [it, setIt] = useState(variable?.label.it ?? '');
  const [en, setEn] = useState(variable?.label.en ?? '');
  const error = create.error ?? update.error;
  const issues = formulaIssuesOf(error);
  const num = (s: string) => (s.trim() ? parseQtyToMilli(s, loc) : null);
  const save = () => {
    const values = enumValues
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    const job =
      source === 'job'
        ? kind === 'enum'
          ? { enum_values: values, ...(defEnum ? { default_enum: defEnum } : {}) }
          : {
              ...(num(min) !== null ? { min_milli: num(min)! } : {}),
              ...(num(max) !== null ? { max_milli: num(max)! } : {}),
              ...(num(def) !== null ? { default_milli: num(def)! } : {}),
            }
        : {};
    const label = it.trim() ? { label: i18nBody(it, en) } : {};
    if (variable?.id) {
      update.mutate(
        {
          id: variable.id,
          version: variable.version ?? 0,
          body: {
            unit: unit || 'none',
            ...(source === 'derived' ? { expression } : {}),
            ...job,
            ...label,
          },
        },
        { onSuccess: onClose },
      );
    } else {
      create.mutate(
        {
          name: name.trim(),
          source,
          kind: kind || 'number',
          unit: unit || 'none',
          ...(source === 'derived' ? { expression } : {}),
          ...job,
          ...label,
        },
        { onSuccess: onClose },
      );
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {source === 'derived'
              ? t('yardCatalog:variables.newDerived')
              : t('yardCatalog:variables.newJob')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field>
            <FieldLabel htmlFor="fv-name">{t('yardCatalog:variables.name')}</FieldLabel>
            <Input
              id="fv-name"
              className="font-mono"
              value={name}
              maxLength={32}
              disabled={!!variable}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <EnumSelect
            id="fv-kind"
            label={t('yardCatalog:variables.kind')}
            values={source === 'derived' ? KINDS.slice(0, 2) : KINDS}
            value={kind}
            onChange={setKind}
            labelOf={(k) => t(`yardCatalog:kinds.${k}`)}
          />
          <EnumSelect
            id="fv-unit"
            label={t('yardCatalog:variables.unit')}
            values={UNITS}
            value={unit}
            onChange={setUnit}
            labelOf={(u) => t(`yardCatalog:units.${u}`)}
          />
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <NameFields idPrefix="fv-label" it={it} en={en} onIt={setIt} onEn={setEn} />
        </div>
        {source === 'derived' ? (
          <FormulaField
            id="fv-expression"
            label={t('yardCatalog:variables.expression')}
            value={expression}
            onChange={setExpression}
            marinaId={marinaId}
            purpose={kind === 'bool' ? 'boolean' : 'derived'}
            issues={issues}
          />
        ) : kind === 'enum' ? (
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="fv-enum">{t('yardCatalog:variables.enumValues')}</FieldLabel>
              <Input
                id="fv-enum"
                value={enumValues}
                onChange={(e) => setEnumValues(e.target.value)}
              />
              <FieldDescription>{t('yardCatalog:variables.enumHint')}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="fv-defenum">{t('yardCatalog:variables.default')}</FieldLabel>
              <Input id="fv-defenum" value={defEnum} onChange={(e) => setDefEnum(e.target.value)} />
            </Field>
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="fv-min">{t('yardCatalog:variables.min')}</FieldLabel>
              <Input
                id="fv-min"
                inputMode="decimal"
                value={min}
                onChange={(e) => setMin(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="fv-max">{t('yardCatalog:variables.max')}</FieldLabel>
              <Input
                id="fv-max"
                inputMode="decimal"
                value={max}
                onChange={(e) => setMax(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="fv-def">{t('yardCatalog:variables.default')}</FieldLabel>
              <Input
                id="fv-def"
                inputMode="decimal"
                value={def}
                onChange={(e) => setDef(e.target.value)}
              />
            </Field>
          </div>
        )}
        {source !== 'derived' ? <IssueList issues={issues} /> : null}
        {issues.length === 0 ? <M4ProblemAlert error={error} /> : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button onClick={save} disabled={!name.trim() || create.isPending || update.isPending}>
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---- tables ----

function rowsText(rows: FormulaTable['rows'], locale: string): string {
  return rows
    .map(
      (r) =>
        `${r.up_to_milli == null ? '' : formatQtyMilli(r.up_to_milli, locale)};${formatQtyMilli(r.value_milli, locale)}`,
    )
    .join('\n');
}

export function TablesTab({ marinaId, canManage }: { marinaId: string; canManage: boolean }) {
  const { t, i18n } = useT();
  const list = useFormulaTables(marinaId);
  const [editing, setEditing] = useState<FormulaTable | 'new' | null>(null);
  if (list.isPending) return <LoadingState rows={3} />;
  if (list.isError)
    return <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />;
  return (
    <div className="flex flex-col gap-3">
      {canManage ? (
        <div>
          <Button onClick={() => setEditing('new')}>
            <PlusIcon />
            {t('yardCatalog:tables.new')}
          </Button>
        </div>
      ) : null}
      <SimpleTable
        head={[t('yardCatalog:tables.name'), t('yardCatalog:tables.rows'), '']}
        empty={t('yardCatalog:tables.empty')}
        rows={list.data.items.length}
      >
        {list.data.items.map((tb) => (
          <TableRow key={tb.id} data-testid="table-row">
            <TableCell className="font-mono">{tb.name}</TableCell>
            <TableCell className="text-xs whitespace-pre tabular-nums">
              {rowsText(tb.rows, i18n.language)}
            </TableCell>
            <TableCell className="text-right">
              {canManage ? (
                <Button size="sm" variant="outline" onClick={() => setEditing(tb)}>
                  {t('yardCatalog:edit')}
                </Button>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      {editing ? (
        <TableDialog
          marinaId={marinaId}
          table={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

function TableDialog({
  marinaId,
  table,
  onClose,
}: {
  marinaId: string;
  table: FormulaTable | null;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const create = useCreateFormulaTable(marinaId);
  const update = useUpdateFormulaTable(marinaId);
  const [name, setName] = useState(table?.name ?? '');
  const [text, setText] = useState(table ? rowsText(table.rows, i18n.language) : '');
  const rows = parseBandRows(text, i18n.language);
  const error = create.error ?? update.error;
  const save = () => {
    if (!rows) return;
    if (table)
      update.mutate(
        { id: table.id, version: table.version, body: { rows } },
        { onSuccess: onClose },
      );
    else create.mutate({ name: name.trim(), rows }, { onSuccess: onClose });
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {table ? t('yardCatalog:tables.edit') : t('yardCatalog:tables.new')}
          </DialogTitle>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="ft-name">{t('yardCatalog:tables.name')}</FieldLabel>
          <Input
            id="ft-name"
            className="font-mono"
            value={name}
            maxLength={32}
            disabled={!!table}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="ft-rows">{t('yardCatalog:tables.rows')}</FieldLabel>
          <Textarea
            id="ft-rows"
            className="font-mono"
            rows={6}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <FieldDescription>{t('yardCatalog:tables.rowsHint')}</FieldDescription>
        </Field>
        <IssueList issues={formulaIssuesOf(error)} />
        <M4ProblemAlert error={error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            onClick={save}
            disabled={!name.trim() || !rows || create.isPending || update.isPending}
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---- settings ----

export function SettingsTab({ marinaId, canManage }: { marinaId: string; canManage: boolean }) {
  const settings = useYardSettings(marinaId);
  if (settings.isPending) return <LoadingState rows={3} />;
  if (settings.isError)
    return <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />;
  return (
    <SettingsForm
      key={settings.data.version}
      marinaId={marinaId}
      settings={settings.data}
      canManage={canManage}
    />
  );
}

function SettingsForm({
  marinaId,
  settings,
  canManage,
}: {
  marinaId: string;
  settings: YardSettings;
  canManage: boolean;
}) {
  const { t } = useT();
  const put = usePutYardSettings(marinaId);
  const tv = settings.test_vessel;
  const [validity, setValidity] = useState(String(settings.quote_validity_days));
  const [deposit, setDeposit] = useState<number | null>(settings.deposit_bp);
  const [loa, setLoa] = useState<number | null>(tv.loa_cm);
  const [beam, setBeam] = useState<number | null>(tv.beam_cm);
  const [draft, setDraft] = useState<number | null>(tv.draft_cm ?? null);
  const [hull, setHull] = useState<Hull | ''>(tv.hull_type);
  const save = () =>
    put.mutate({
      version: settings.version,
      body: {
        quote_validity_days: Number(validity),
        deposit_bp: deposit ?? 0,
        test_vessel: {
          ...tv,
          loa_cm: loa ?? tv.loa_cm,
          beam_cm: beam ?? tv.beam_cm,
          ...(draft ? { draft_cm: draft } : {}),
          hull_type: hull || tv.hull_type,
        },
      },
    });
  return (
    <div className="flex flex-col gap-4">
      <Section title={t('yardCatalog:settings.quotes')}>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="ys-validity">{t('yardCatalog:settings.validity')}</FieldLabel>
            <Input
              id="ys-validity"
              inputMode="numeric"
              value={validity}
              onChange={(e) => setValidity(e.target.value.replace(/\D/g, ''))}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="ys-deposit">{t('yardCatalog:settings.deposit')}</FieldLabel>
            <PercentInput id="ys-deposit" value={deposit} onChange={setDeposit} />
          </Field>
        </div>
      </Section>
      <Section
        title={t('yardCatalog:settings.testVessel')}
        description={t('yardCatalog:settings.testVesselHint')}
      >
        <div className="grid gap-3 sm:grid-cols-4">
          <Field>
            <FieldLabel htmlFor="ys-loa">{t('yardCatalog:settings.loa')}</FieldLabel>
            <LengthInput id="ys-loa" value={loa} onChange={setLoa} />
          </Field>
          <Field>
            <FieldLabel htmlFor="ys-beam">{t('yardCatalog:settings.beam')}</FieldLabel>
            <LengthInput id="ys-beam" value={beam} onChange={setBeam} />
          </Field>
          <Field>
            <FieldLabel htmlFor="ys-draft">{t('yardCatalog:settings.draft')}</FieldLabel>
            <LengthInput id="ys-draft" value={draft} onChange={setDraft} />
          </Field>
          <EnumSelect
            id="ys-hull"
            label={t('yardCatalog:settings.hull')}
            values={HULL_TYPES}
            value={hull}
            onChange={setHull}
            labelOf={(h) => t(`yardCatalog:hullTypes.${h}`)}
          />
        </div>
      </Section>
      <M4ProblemAlert error={put.error} />
      {canManage ? (
        <div>
          <Button onClick={save} disabled={put.isPending}>
            {t('common:actions.save')}
          </Button>
        </div>
      ) : null}
      {settings.version === 0 ? (
        <Badge variant="outline">{t('yardCatalog:settings.defaults')}</Badge>
      ) : null}
    </div>
  );
}
