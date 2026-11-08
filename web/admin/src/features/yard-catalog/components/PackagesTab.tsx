import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useState } from 'react';

import {
  formulaIssuesOf,
  useCreateWorkPackage,
  useSaveWorkPackageVersion,
  useSetWorkPackageActive,
  useWorkPackage,
  useWorkPackages,
  useWorkTypes,
  useYardItems,
  type WorkPackage,
  type WorkPackageLine,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, M4ProblemAlert, Section } from '../../contracts-m4/components/kit';
import { i18nBody, issuesFor } from '../lib';
import { FormulaField } from './FormulaField';
import { IssueList } from './IssueList';
import { NameFields } from './TaxonomyTabs';

export function PackagesTab({ marinaId, canManage }: { marinaId: string; canManage: boolean }) {
  const { t } = useT();
  const list = useWorkPackages(marinaId);
  const types = useWorkTypes(marinaId);
  const active = useSetWorkPackageActive(marinaId);
  const [editing, setEditing] = useState<WorkPackage | 'new' | null>(null);
  if (list.isPending) return <LoadingState rows={3} />;
  if (list.isError)
    return <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />;
  const typeName = (id: string) => types.data?.items.find((w) => w.id === id)?.name.it ?? '';
  return (
    <div className="flex flex-col gap-3">
      {canManage ? (
        <div>
          <Button onClick={() => setEditing('new')}>
            <PlusIcon />
            {t('yardCatalog:packages.new')}
          </Button>
        </div>
      ) : null}
      <SimpleTable
        head={[
          t('yardCatalog:fields.code'),
          t('yardCatalog:fields.name'),
          t('yardCatalog:packages.workType'),
          t('yardCatalog:packages.version'),
          '',
        ]}
        empty={t('yardCatalog:packages.empty')}
        rows={list.data.items.length}
      >
        {list.data.items.map((p) => (
          <TableRow key={p.id} data-testid="package-row">
            <TableCell className="font-medium">{p.code}</TableCell>
            <TableCell>
              {p.name.it}
              {!p.active ? (
                <Badge variant="outline" className="ml-2">
                  {t('yardCatalog:inactive')}
                </Badge>
              ) : null}
            </TableCell>
            <TableCell>{typeName(p.work_type_id)}</TableCell>
            <TableCell className="tabular-nums">{`v${p.current_version}`}</TableCell>
            <TableCell className="flex justify-end gap-2">
              <Button size="sm" variant="outline" onClick={() => setEditing(p)}>
                {canManage ? t('yardCatalog:edit') : t('yardCatalog:open')}
              </Button>
              {canManage ? (
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={active.isPending}
                  onClick={() => active.mutate({ id: p.id, version: p.version, active: !p.active })}
                >
                  {p.active
                    ? t('yardCatalog:packages.deactivate')
                    : t('yardCatalog:packages.activate')}
                </Button>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      {editing ? (
        <PackageEditor
          marinaId={marinaId}
          packageId={editing === 'new' ? undefined : editing.id}
          canManage={canManage}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

type Line = {
  key: number;
  item_id: string;
  work_type_id: string;
  qty_formula: string;
  price_formula: string;
  task: string;
};

function toLines(lines: readonly WorkPackageLine[]): Line[] {
  return lines.map((l, i) => ({
    key: i,
    item_id: l.item_id,
    work_type_id: l.work_type_id ?? '',
    qty_formula: l.qty_formula,
    price_formula: l.price_formula ?? '',
    task: l.task?.title.it ?? '',
  }));
}

function toBody(lines: readonly Line[]): WorkPackageLine[] {
  return lines.map((l, i) => ({
    seq: i + 1,
    item_id: l.item_id,
    qty_formula: l.qty_formula.trim(),
    ...(l.price_formula.trim() ? { price_formula: l.price_formula.trim() } : {}),
    ...(l.work_type_id ? { work_type_id: l.work_type_id } : {}),
    ...(l.task.trim() ? { task: { title: { it: l.task.trim() } } } : {}),
  }));
}

/** Package editor (m10-plan §14.1): lines with formulas, inline issues, version history; saving inserts N+1. */
function PackageEditor({
  marinaId,
  packageId,
  canManage,
  onClose,
}: {
  marinaId: string;
  packageId: string | undefined;
  canManage: boolean;
  onClose: () => void;
}) {
  const pkg = useWorkPackage(marinaId, packageId);
  if (packageId && pkg.isPending) return null;
  return (
    <PackageForm
      marinaId={marinaId}
      pkg={packageId ? (pkg.data ?? null) : null}
      canManage={canManage}
      onClose={onClose}
    />
  );
}

function PackageForm({
  marinaId,
  pkg,
  canManage,
  onClose,
}: {
  marinaId: string;
  pkg: WorkPackage | null;
  canManage: boolean;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const items = useYardItems(marinaId);
  const types = useWorkTypes(marinaId);
  const create = useCreateWorkPackage(marinaId);
  const save = useSaveWorkPackageVersion(marinaId);
  const [code, setCode] = useState(pkg?.code ?? '');
  const [it, setIt] = useState(pkg?.name.it ?? '');
  const [en, setEn] = useState(pkg?.name.en ?? '');
  const [workType, setWorkType] = useState(pkg?.work_type_id ?? '');
  const [jobVars, setJobVars] = useState((pkg?.content?.job_vars ?? []).join(', '));
  const [lines, setLines] = useState<Line[]>(toLines(pkg?.content?.lines ?? []));
  const [nextKey, setNextKey] = useState(lines.length);
  const error = create.error ?? save.error;
  const issues = formulaIssuesOf(error);
  const itemOptions = items.data?.items ?? [];
  const typeOptions = types.data?.items ?? [];
  const patch = (key: number, p: Partial<Line>) =>
    setLines((ls) => ls.map((l) => (l.key === key ? { ...l, ...p } : l)));
  const submit = () => {
    const body = {
      lines: toBody(lines),
      job_vars: jobVars
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
    };
    if (pkg)
      save.mutate(
        {
          id: pkg.id,
          version: pkg.version,
          body: { ...body, name: i18nBody(it, en), work_type_id: workType },
        },
        { onSuccess: onClose },
      );
    else
      create.mutate(
        {
          ...body,
          code: code.trim().toUpperCase(),
          name: i18nBody(it, en),
          work_type_id: workType,
        },
        { onSuccess: onClose },
      );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-svh overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>
            {pkg
              ? t('yardCatalog:packages.editOf', { code: pkg.code })
              : t('yardCatalog:packages.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field>
            <FieldLabel htmlFor="wp-code">{t('yardCatalog:fields.code')}</FieldLabel>
            <Input
              id="wp-code"
              value={code}
              maxLength={16}
              disabled={!!pkg}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <EnumSelect
            id="wp-type"
            label={t('yardCatalog:packages.workType')}
            values={typeOptions.map((w) => w.id)}
            value={workType}
            onChange={setWorkType}
            labelOf={(id) => typeOptions.find((w) => w.id === id)?.name.it ?? id}
          />
          <Field>
            <FieldLabel htmlFor="wp-jobvars">{t('yardCatalog:packages.jobVars')}</FieldLabel>
            <Input
              id="wp-jobvars"
              className="font-mono"
              value={jobVars}
              onChange={(e) => setJobVars(e.target.value)}
            />
          </Field>
          <NameFields idPrefix="wp-name" it={it} en={en} onIt={setIt} onEn={setEn} />
        </div>
        <Section title={t('yardCatalog:packages.lines')}>
          {lines.map((l, i) => {
            const prefix = `lines[${i}]`;
            return (
              <div
                key={l.key}
                className="flex flex-col gap-2 rounded-md border p-3"
                data-testid="package-line"
              >
                <div className="grid gap-3 sm:grid-cols-3">
                  <EnumSelect
                    id={`wl-item-${l.key}`}
                    label={t('yardCatalog:packages.item', { n: i + 1 })}
                    values={itemOptions.map((o) => o.item_id)}
                    value={l.item_id}
                    onChange={(v) => patch(l.key, { item_id: v })}
                    labelOf={(id) => {
                      const o = itemOptions.find((x) => x.item_id === id);
                      return o ? `${o.code} · ${o.name.it}` : id;
                    }}
                  />
                  <Field>
                    <FieldLabel htmlFor={`wl-task-${l.key}`}>
                      {t('yardCatalog:packages.task')}
                    </FieldLabel>
                    <Input
                      id={`wl-task-${l.key}`}
                      value={l.task}
                      maxLength={200}
                      onChange={(e) => patch(l.key, { task: e.target.value })}
                    />
                  </Field>
                  {canManage ? (
                    <div className="flex items-end justify-end">
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label={t('yardCatalog:packages.removeLine', { n: i + 1 })}
                        onClick={() => setLines((ls) => ls.filter((x) => x.key !== l.key))}
                      >
                        <Trash2Icon />
                      </Button>
                    </div>
                  ) : null}
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <FormulaField
                    id={`wl-qty-${l.key}`}
                    label={t('yardCatalog:items.qtyFormula')}
                    value={l.qty_formula}
                    onChange={(v) => patch(l.key, { qty_formula: v })}
                    marinaId={marinaId}
                    issues={issuesFor(issues, `${prefix}.qty_formula`)}
                  />
                  <FormulaField
                    id={`wl-price-${l.key}`}
                    label={t('yardCatalog:items.priceFormula')}
                    value={l.price_formula}
                    onChange={(v) => patch(l.key, { price_formula: v })}
                    marinaId={marinaId}
                    purpose="price"
                    itemId={l.item_id}
                    issues={issuesFor(issues, `${prefix}.price_formula`)}
                  />
                </div>
              </div>
            );
          })}
          {canManage ? (
            <div>
              <Button
                variant="outline"
                onClick={() => {
                  setLines((ls) => [
                    ...ls,
                    {
                      key: nextKey,
                      item_id: '',
                      work_type_id: '',
                      qty_formula: '1',
                      price_formula: '',
                      task: '',
                    },
                  ]);
                  setNextKey((k) => k + 1);
                }}
              >
                <PlusIcon />
                {t('yardCatalog:packages.addLine')}
              </Button>
            </div>
          ) : null}
        </Section>
        <IssueList issues={issues.filter((i) => !(i.field ?? '').startsWith('lines['))} />
        {issues.length === 0 ? <M4ProblemAlert error={error} /> : null}
        {pkg?.versions?.length ? (
          <Section title={t('yardCatalog:packages.history')}>
            <ul className="text-sm">
              {pkg.versions.map((v) => (
                <li key={v.version} className="tabular-nums">
                  {t('yardCatalog:packages.versionAt', {
                    version: v.version,
                    at: formatDateTime(v.created_at, i18n.language),
                  })}
                </li>
              ))}
            </ul>
          </Section>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          {canManage ? (
            <Button
              onClick={submit}
              disabled={
                !it.trim() || !workType || lines.length === 0 || create.isPending || save.isPending
              }
            >
              {pkg ? t('yardCatalog:packages.saveVersion') : t('common:actions.create')}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
