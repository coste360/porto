import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  formulaIssuesOf,
  useCreateWorkType,
  useCreateYardQualification,
  usePutYardItemSettings,
  useUpdateWorkType,
  useUpdateYardQualification,
  useWorkTypes,
  useYardItems,
  useYardQualifications,
  type WorkType,
  type YardItem,
  type YardQualification,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
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
import { Label } from '@porto/shared/ui/label';
import { MoneyInput } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, M4ProblemAlert, Money } from '../../contracts-m4/components/kit';
import { i18nBody } from '../lib';
import { FormulaField } from './FormulaField';

/** Italian and English name fields of a catalogue entry. */
export function NameFields({
  idPrefix,
  it,
  en,
  onIt,
  onEn,
}: {
  idPrefix: string;
  it: string;
  en: string;
  onIt: (v: string) => void;
  onEn: (v: string) => void;
}) {
  const { t } = useT();
  return (
    <>
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-it`}>{t('yardCatalog:fields.nameIt')}</FieldLabel>
        <Input
          id={`${idPrefix}-it`}
          value={it}
          maxLength={100}
          onChange={(e) => onIt(e.target.value)}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-en`}>{t('yardCatalog:fields.nameEn')}</FieldLabel>
        <Input
          id={`${idPrefix}-en`}
          value={en}
          maxLength={100}
          onChange={(e) => onEn(e.target.value)}
        />
      </Field>
    </>
  );
}

// ---- work types ----

export function WorkTypesTab({ marinaId, canManage }: { marinaId: string; canManage: boolean }) {
  const { t } = useT();
  const list = useWorkTypes(marinaId);
  const [editing, setEditing] = useState<WorkType | 'new' | null>(null);
  if (list.isPending) return <LoadingState rows={3} />;
  if (list.isError)
    return <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />;
  const items = list.data.items;
  return (
    <div className="flex flex-col gap-3">
      {canManage ? (
        <div>
          <Button onClick={() => setEditing('new')}>
            <PlusIcon />
            {t('yardCatalog:workTypes.new')}
          </Button>
        </div>
      ) : null}
      <SimpleTable
        head={[
          t('yardCatalog:fields.code'),
          t('yardCatalog:fields.name'),
          t('yardCatalog:fields.status'),
          '',
        ]}
        empty={t('yardCatalog:workTypes.empty')}
        rows={items.length}
      >
        {items.map((w) => (
          <TableRow key={w.id} data-testid="work-type-row">
            <TableCell className="font-medium">{w.code}</TableCell>
            <TableCell>{w.name.it}</TableCell>
            <TableCell>
              {w.active ? null : <Badge variant="outline">{t('yardCatalog:inactive')}</Badge>}
            </TableCell>
            <TableCell className="text-right">
              {canManage ? (
                <Button size="sm" variant="outline" onClick={() => setEditing(w)}>
                  {t('yardCatalog:edit')}
                </Button>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      {editing ? (
        <WorkTypeDialog
          marinaId={marinaId}
          workType={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

function WorkTypeDialog({
  marinaId,
  workType,
  onClose,
}: {
  marinaId: string;
  workType: WorkType | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateWorkType(marinaId);
  const update = useUpdateWorkType(marinaId);
  const [code, setCode] = useState(workType?.code ?? '');
  const [it, setIt] = useState(workType?.name.it ?? '');
  const [en, setEn] = useState(workType?.name.en ?? '');
  const [active, setActive] = useState(workType?.active ?? true);
  const save = () => {
    const body = { code: code.trim().toUpperCase(), name: i18nBody(it, en), active };
    if (workType)
      update.mutate({ id: workType.id, version: workType.version, body }, { onSuccess: onClose });
    else create.mutate(body, { onSuccess: onClose });
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {workType ? t('yardCatalog:workTypes.edit') : t('yardCatalog:workTypes.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="wt-code">{t('yardCatalog:fields.code')}</FieldLabel>
            <Input
              id="wt-code"
              value={code}
              maxLength={16}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <div className="flex items-center gap-2">
            <Switch id="wt-active" checked={active} onCheckedChange={setActive} />
            <Label htmlFor="wt-active">{t('yardCatalog:fields.active')}</Label>
          </div>
          <NameFields idPrefix="wt-name" it={it} en={en} onIt={setIt} onEn={setEn} />
        </div>
        <M4ProblemAlert error={create.error ?? update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            onClick={save}
            disabled={!code.trim() || !it.trim() || create.isPending || update.isPending}
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---- qualifications ----

export function QualificationsTab({
  marinaId,
  canManage,
}: {
  marinaId: string;
  canManage: boolean;
}) {
  const { t } = useT();
  const list = useYardQualifications(marinaId);
  const labour = useYardItems(marinaId, { item_type: 'labour' });
  const [editing, setEditing] = useState<YardQualification | 'new' | null>(null);
  if (list.isPending) return <LoadingState rows={3} />;
  if (list.isError)
    return <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />;
  const items = list.data.items;
  const itemCode = (id: string) => labour.data?.items.find((i) => i.item_id === id)?.code ?? '';
  return (
    <div className="flex flex-col gap-3">
      {canManage ? (
        <div>
          <Button onClick={() => setEditing('new')}>
            <PlusIcon />
            {t('yardCatalog:qualifications.new')}
          </Button>
        </div>
      ) : null}
      <SimpleTable
        head={[
          t('yardCatalog:fields.code'),
          t('yardCatalog:fields.name'),
          t('yardCatalog:qualifications.labourItem'),
          t('yardCatalog:qualifications.defaultCost'),
          '',
        ]}
        empty={t('yardCatalog:qualifications.empty')}
        rows={items.length}
      >
        {items.map((q) => (
          <TableRow key={q.id} data-testid="qualification-row">
            <TableCell className="font-medium">{q.code}</TableCell>
            <TableCell>
              {q.name.it}
              {q.lift_operator ? (
                <Badge variant="secondary" className="ml-2">
                  {t('yardCatalog:qualifications.liftOperator')}
                </Badge>
              ) : null}
            </TableCell>
            <TableCell>{itemCode(q.labour_item_id)}</TableCell>
            <TableCell>
              <Money cents={q.default_cost_cents} />
            </TableCell>
            <TableCell className="text-right">
              {canManage ? (
                <Button size="sm" variant="outline" onClick={() => setEditing(q)}>
                  {t('yardCatalog:edit')}
                </Button>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </SimpleTable>
      {editing ? (
        <QualificationDialog
          marinaId={marinaId}
          qualification={editing === 'new' ? null : editing}
          labourItems={labour.data?.items ?? []}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

function QualificationDialog({
  marinaId,
  qualification,
  labourItems,
  onClose,
}: {
  marinaId: string;
  qualification: YardQualification | null;
  labourItems: YardItem[];
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const create = useCreateYardQualification(marinaId);
  const update = useUpdateYardQualification(marinaId);
  const [code, setCode] = useState(qualification?.code ?? '');
  const [it, setIt] = useState(qualification?.name.it ?? '');
  const [en, setEn] = useState(qualification?.name.en ?? '');
  const [item, setItem] = useState(qualification?.labour_item_id ?? '');
  const [cost, setCost] = useState<number | null>(qualification?.default_cost_cents ?? 0);
  const [lift, setLift] = useState(qualification?.lift_operator ?? false);
  const save = () => {
    const body = {
      code: code.trim().toLowerCase(),
      name: i18nBody(it, en),
      labour_item_id: item,
      default_cost_cents: cost ?? 0,
      lift_operator: lift,
    };
    if (qualification)
      update.mutate(
        { id: qualification.id, version: qualification.version, body },
        { onSuccess: onClose },
      );
    else create.mutate(body, { onSuccess: onClose });
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {qualification
              ? t('yardCatalog:qualifications.edit')
              : t('yardCatalog:qualifications.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="q-code">{t('yardCatalog:fields.code')}</FieldLabel>
            <Input
              id="q-code"
              value={code}
              maxLength={16}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <EnumSelect
            id="q-item"
            label={t('yardCatalog:qualifications.labourItem')}
            values={labourItems.map((i) => i.item_id)}
            value={item}
            onChange={setItem}
            labelOf={(id) => labourItems.find((i) => i.item_id === id)?.code ?? id}
          />
          <NameFields idPrefix="q-name" it={it} en={en} onIt={setIt} onEn={setEn} />
          <Field>
            <FieldLabel htmlFor="q-cost">{t('yardCatalog:qualifications.defaultCost')}</FieldLabel>
            <MoneyInput id="q-cost" value={cost} onChange={setCost} locale={i18n.language} />
          </Field>
          <div className="flex items-center gap-2">
            <Switch id="q-lift" checked={lift} onCheckedChange={setLift} />
            <Label htmlFor="q-lift">{t('yardCatalog:qualifications.liftOperator')}</Label>
          </div>
        </div>
        <M4ProblemAlert error={create.error ?? update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            onClick={save}
            disabled={!code.trim() || !it.trim() || !item || create.isPending || update.isPending}
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---- items ----

export function ItemsTab({ marinaId, canManage }: { marinaId: string; canManage: boolean }) {
  const { t } = useT();
  const [q, setQ] = useState('');
  const list = useYardItems(marinaId, q.trim() ? { q: q.trim() } : {});
  const [editing, setEditing] = useState<YardItem | null>(null);
  return (
    <div className="flex flex-col gap-3">
      <Field className="max-w-sm">
        <FieldLabel htmlFor="yi-search">{t('yardCatalog:items.search')}</FieldLabel>
        <Input id="yi-search" value={q} onChange={(e) => setQ(e.target.value)} />
      </Field>
      {list.isPending ? (
        <LoadingState rows={4} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <SimpleTable
          head={[
            t('yardCatalog:fields.code'),
            t('yardCatalog:fields.name'),
            t('yardCatalog:items.type'),
            t('yardCatalog:items.cost'),
            t('yardCatalog:items.qtyFormula'),
            '',
          ]}
          empty={t('yardCatalog:items.empty')}
          rows={list.data.items.length}
        >
          {list.data.items.map((it) => (
            <TableRow key={it.item_id} data-testid="yard-item-row">
              <TableCell className="font-medium">{it.code}</TableCell>
              <TableCell>{it.name.it}</TableCell>
              <TableCell>{t(`yardCatalog:itemTypes.${it.item_type}`)}</TableCell>
              <TableCell>{it.configured ? <Money cents={it.cost_cents} /> : null}</TableCell>
              <TableCell className="font-mono text-xs">{it.qty_formula}</TableCell>
              <TableCell className="text-right">
                {canManage ? (
                  <Button size="sm" variant="outline" onClick={() => setEditing(it)}>
                    {t('yardCatalog:items.settings')}
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      <p className="text-muted-foreground text-sm">{t('yardCatalog:items.pricesHint')}</p>
      {editing ? (
        <ItemDialog marinaId={marinaId} item={editing} onClose={() => setEditing(null)} />
      ) : null}
    </div>
  );
}

function ItemDialog({
  marinaId,
  item,
  onClose,
}: {
  marinaId: string;
  item: YardItem;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const put = usePutYardItemSettings(marinaId);
  const [enabled, setEnabled] = useState(item.configured ? item.yard_enabled : true);
  const [cost, setCost] = useState<number | null>(item.cost_cents);
  const [category, setCategory] = useState(item.category);
  const [qtyFormula, setQtyFormula] = useState(item.qty_formula);
  const [priceFormula, setPriceFormula] = useState(item.price_formula);
  const issues = formulaIssuesOf(put.error);
  const save = () =>
    put.mutate(
      {
        itemId: item.item_id,
        version: item.configured ? item.version : 0,
        body: {
          yard_enabled: enabled,
          cost_cents: cost ?? 0,
          category: category.trim(),
          qty_formula: qtyFormula.trim(),
          price_formula: priceFormula.trim(),
        },
      },
      { onSuccess: onClose },
    );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('yardCatalog:items.settingsOf', { code: item.code })}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex items-center gap-2">
            <Switch id="yi-enabled" checked={enabled} onCheckedChange={setEnabled} />
            <Label htmlFor="yi-enabled">{t('yardCatalog:items.enabled')}</Label>
          </div>
          <Field>
            <FieldLabel htmlFor="yi-cost">{t('yardCatalog:items.cost')}</FieldLabel>
            <MoneyInput id="yi-cost" value={cost} onChange={setCost} locale={i18n.language} />
          </Field>
          <Field>
            <FieldLabel htmlFor="yi-category">{t('yardCatalog:items.category')}</FieldLabel>
            <Input
              id="yi-category"
              value={category}
              maxLength={40}
              onChange={(e) => setCategory(e.target.value)}
            />
          </Field>
        </div>
        <FormulaField
          id="yi-qty"
          label={t('yardCatalog:items.qtyFormula')}
          value={qtyFormula}
          onChange={setQtyFormula}
          marinaId={marinaId}
          issues={issues.filter((i) => i.field === 'qty_formula')}
        />
        <FormulaField
          id="yi-price"
          label={t('yardCatalog:items.priceFormula')}
          value={priceFormula}
          onChange={setPriceFormula}
          marinaId={marinaId}
          purpose="price"
          itemId={item.item_id}
          issues={issues.filter((i) => i.field === 'price_formula')}
          description={t('yardCatalog:formula.priceHint')}
        />
        {issues.length === 0 ? <M4ProblemAlert error={put.error} /> : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button onClick={save} disabled={put.isPending}>
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
