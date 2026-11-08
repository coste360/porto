import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateMaintenancePlan,
  useMaintenancePlans,
  useUpdateMaintenancePlan,
  useUtilityMeters,
  type ChecklistEntry,
  type MaintenanceElementRef,
  type MaintenanceElementType,
  type MaintenanceIntervention,
  type MaintenancePlan,
  type MaintenancePlanInput,
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
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, M4ProblemAlert, PageFrame } from '../../contracts-m4/components/kit';
import { ElementPicker, MaintenanceNav } from '../components/kit';
import { periodicityText } from '../lib';

const INTERVENTIONS: readonly MaintenanceIntervention[] = [
  'underwater_inspection',
  'pennant_replacement',
  'chain_check',
  'rcd_test',
  'general',
  'custom',
];
type PeriodKind = MaintenancePlan['periodicity']['kind'];
const PERIOD_KINDS: readonly PeriodKind[] = ['months', 'fixed_date', 'cycles'];

/** Maintenance plans (m9-plan §8.2): list and editor (targets, periodicity, lead days, checklist builder). */
export function PlansPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const plans = useMaintenancePlans(marinaId);
  const [editing, setEditing] = useState<MaintenancePlan | 'new' | null>(null);
  const items = plans.data?.items ?? [];
  return (
    <RequirePermission allowed={can('maintenance.manage')}>
      <PageFrame>
        <PageHeader
          title={t('maintenance:plans.title')}
          description={t('maintenance:plans.description')}
          actions={
            <Button onClick={() => setEditing('new')}>
              <PlusIcon />
              {t('maintenance:plans.new')}
            </Button>
          }
        />
        <MaintenanceNav marinaId={marinaId} current="plans" />
        {plans.isPending ? (
          <LoadingState rows={4} />
        ) : plans.isError ? (
          <QueryErrorState error={plans.error} onRetry={() => void plans.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('maintenance:fields.name'),
              t('maintenance:fields.intervention'),
              t('maintenance:fields.periodicity'),
              t('maintenance:fields.targets'),
              t('maintenance:fields.active'),
              '',
            ]}
            empty={t('maintenance:plans.empty')}
            rows={items.length}
          >
            {items.map((p) => (
              <TableRow key={p.id} data-testid="maintenance-plan-row">
                <TableCell className="font-medium">{p.name}</TableCell>
                <TableCell>
                  {p.intervention === 'custom' && p.intervention_label
                    ? p.intervention_label
                    : t(`maintenance:enums.intervention.${p.intervention}`)}
                </TableCell>
                <TableCell>{periodicityText(t, p.periodicity)}</TableCell>
                <TableCell>{p.targets.length}</TableCell>
                <TableCell>
                  <Badge variant={p.active ? 'secondary' : 'outline'}>
                    {p.active ? t('maintenance:yes') : t('maintenance:no')}
                  </Badge>
                </TableCell>
                <TableCell className="text-right">
                  <Button size="sm" variant="outline" onClick={() => setEditing(p)}>
                    {t('maintenance:plans.edit')}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        {editing ? (
          <PlanEditor
            marinaId={marinaId}
            plan={editing === 'new' ? null : editing}
            onClose={() => setEditing(null)}
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

/** Editor of a plan; the checklist builder keeps keys stable (k1, k2, ...). */
export function PlanEditor({
  marinaId,
  plan,
  onClose,
}: {
  marinaId: string;
  plan: MaintenancePlan | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateMaintenancePlan(marinaId);
  const update = useUpdateMaintenancePlan(marinaId);
  const meters = useUtilityMeters(marinaId, { active: true });
  const [name, setName] = useState(plan?.name ?? '');
  const [intervention, setIntervention] = useState<MaintenanceIntervention | ''>(
    plan?.intervention ?? 'general',
  );
  const [label, setLabel] = useState(plan?.intervention_label ?? '');
  const [kind, setKind] = useState<PeriodKind | ''>(plan?.periodicity.kind ?? 'months');
  const [every, setEvery] = useState(String(plan ? planEvery(plan) : 6));
  const [monthDay, setMonthDay] = useState(plan?.periodicity.month_day ?? '');
  const [startsOn, setStartsOn] = useState<string | null>(plan?.periodicity.starts_on ?? null);
  const [meterId, setMeterId] = useState(plan?.periodicity.meter_id ?? '');
  const [lead, setLead] = useState(String(plan?.lead_days ?? 0));
  const [nights, setNights] = useState(String(plan?.nights ?? 1));
  const [requiresFree, setRequiresFree] = useState(plan?.requires_free ?? false);
  const [active, setActive] = useState(plan?.active ?? true);
  const [targets, setTargets] = useState<MaintenanceElementRef[]>(plan?.targets ?? []);
  const [checklist, setChecklist] = useState<ChecklistEntry[]>(plan?.checklist ?? []);
  const [picker, setPicker] = useState<{
    mapId: string;
    type: MaintenanceElementType;
    id: string | null;
    code?: string;
  }>({
    mapId: '',
    type: 'berth',
    id: null,
  });
  const hourMeters = (meters.data?.items ?? []).filter(
    (m) => m.kind === 'hours' || m.kind === 'cycles',
  );
  const body = (): MaintenancePlanInput => ({
    name: name.trim(),
    intervention: intervention || 'general',
    ...(intervention === 'custom' ? { intervention_label: label.trim() } : {}),
    targets: targets.map((x) => ({ element_type: x.element_type, element_id: x.element_id })),
    periodicity:
      kind === 'fixed_date'
        ? { kind, month_day: monthDay }
        : kind === 'cycles'
          ? { kind, every: Math.round(Number(every) * 1000), meter_id: meterId }
          : { kind: 'months', every: Number(every), ...(startsOn ? { starts_on: startsOn } : {}) },
    lead_days: Number(lead) || 0,
    nights: Number(nights) || 1,
    requires_free: requiresFree,
    active,
    checklist: checklist
      .filter((c) => c.label.trim())
      .map((c) => ({ key: c.key, label: c.label.trim() })),
  });
  const save = () => {
    if (plan)
      update.mutate({ id: plan.id, version: plan.version, body: body() }, { onSuccess: onClose });
    else create.mutate(body(), { onSuccess: onClose });
  };
  const nextKey = () => {
    let i = checklist.length + 1;
    while (checklist.some((c) => c.key === `k${i}`)) i++;
    return `k${i}`;
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-screen overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {plan ? t('maintenance:plans.edit') : t('maintenance:plans.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="plan-name">{t('maintenance:fields.name')}</FieldLabel>
              <Input
                id="plan-name"
                value={name}
                maxLength={100}
                onChange={(e) => setName(e.target.value)}
              />
            </Field>
            <EnumSelect
              id="plan-intervention"
              label={t('maintenance:fields.intervention')}
              values={INTERVENTIONS}
              value={intervention}
              onChange={setIntervention}
              labelOf={(v) => t(`maintenance:enums.intervention.${v}`)}
            />
          </div>
          {intervention === 'custom' ? (
            <Field>
              <FieldLabel htmlFor="plan-label">
                {t('maintenance:fields.interventionLabel')}
              </FieldLabel>
              <Input
                id="plan-label"
                value={label}
                maxLength={100}
                onChange={(e) => setLabel(e.target.value)}
              />
            </Field>
          ) : null}
          <div className="grid gap-3 sm:grid-cols-3">
            <EnumSelect
              id="plan-kind"
              label={t('maintenance:fields.periodicity')}
              values={PERIOD_KINDS}
              value={kind}
              onChange={setKind}
              labelOf={(v) => t(`maintenance:enums.periodKind.${v}`)}
            />
            {kind === 'fixed_date' ? (
              <Field>
                <FieldLabel htmlFor="plan-md">{t('maintenance:fields.monthDay')}</FieldLabel>
                <Input
                  id="plan-md"
                  value={monthDay}
                  maxLength={5}
                  onChange={(e) => setMonthDay(e.target.value)}
                />
              </Field>
            ) : (
              <Field>
                <FieldLabel htmlFor="plan-every">
                  {kind === 'cycles'
                    ? t('maintenance:fields.everyUnits')
                    : t('maintenance:fields.everyMonths')}
                </FieldLabel>
                <Input
                  id="plan-every"
                  inputMode="decimal"
                  value={every}
                  onChange={(e) => setEvery(e.target.value.replace(/[^\d.]/g, ''))}
                />
              </Field>
            )}
            {kind === 'months' ? (
              <Field>
                <FieldLabel htmlFor="plan-start">{t('maintenance:fields.startsOn')}</FieldLabel>
                <DatePicker id="plan-start" value={startsOn} onChange={setStartsOn} />
              </Field>
            ) : null}
            {kind === 'cycles' ? (
              <EnumSelect
                id="plan-meter"
                label={t('maintenance:fields.meter')}
                values={hourMeters.map((m) => m.id)}
                value={meterId}
                onChange={setMeterId}
                labelOf={(id) => hourMeters.find((m) => m.id === id)?.code ?? id}
              />
            ) : null}
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="plan-lead">{t('maintenance:fields.leadDays')}</FieldLabel>
              <Input
                id="plan-lead"
                inputMode="numeric"
                value={lead}
                onChange={(e) => setLead(e.target.value.replace(/\D/g, ''))}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="plan-nights">{t('maintenance:fields.planNights')}</FieldLabel>
              <Input
                id="plan-nights"
                inputMode="numeric"
                value={nights}
                onChange={(e) => setNights(e.target.value.replace(/\D/g, ''))}
              />
            </Field>
            <div className="flex flex-col justify-end gap-2">
              <div className="flex items-center gap-2">
                <Switch id="plan-free" checked={requiresFree} onCheckedChange={setRequiresFree} />
                <Label htmlFor="plan-free">{t('maintenance:fields.requiresFree')}</Label>
              </div>
              <div className="flex items-center gap-2">
                <Switch id="plan-active" checked={active} onCheckedChange={setActive} />
                <Label htmlFor="plan-active">{t('maintenance:fields.active')}</Label>
              </div>
            </div>
          </div>
          <fieldset className="flex flex-col gap-2">
            <legend className="text-sm font-medium">{t('maintenance:fields.targets')}</legend>
            <ElementPicker
              marinaId={marinaId}
              idPrefix="plan-target"
              value={picker}
              onChange={setPicker}
            />
            <div>
              <Button
                size="sm"
                variant="outline"
                disabled={!picker.id || targets.some((x) => x.element_id === picker.id)}
                onClick={() =>
                  setTargets((list) => [
                    ...list,
                    { element_type: picker.type, element_id: picker.id!, code: picker.code ?? '' },
                  ])
                }
              >
                <PlusIcon />
                {t('maintenance:plans.addTarget')}
              </Button>
            </div>
            <ul className="flex flex-wrap gap-2" data-testid="plan-targets">
              {targets.map((x) => (
                <li key={`${x.element_type}:${x.element_id}`}>
                  <Badge variant="secondary" className="gap-1">
                    {t(`maintenance:enums.elementType.${x.element_type}`)} {x.code ?? ''}
                    <button
                      type="button"
                      aria-label={t('maintenance:plans.removeTarget')}
                      onClick={() => setTargets((list) => list.filter((y) => y !== x))}
                    >
                      <Trash2Icon className="size-3" />
                    </button>
                  </Badge>
                </li>
              ))}
            </ul>
          </fieldset>
          <fieldset className="flex flex-col gap-2">
            <legend className="text-sm font-medium">{t('maintenance:fields.checklist')}</legend>
            {checklist.map((c, i) => (
              <div key={c.key} className="flex items-center gap-2" data-testid="checklist-row">
                <Input
                  aria-label={t('maintenance:plans.checklistItem', { n: i + 1 })}
                  value={c.label}
                  maxLength={200}
                  onChange={(e) =>
                    setChecklist((list) =>
                      list.map((x) => (x.key === c.key ? { ...x, label: e.target.value } : x)),
                    )
                  }
                />
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={t('maintenance:plans.removeItem')}
                  onClick={() => setChecklist((list) => list.filter((x) => x.key !== c.key))}
                >
                  <Trash2Icon />
                </Button>
              </div>
            ))}
            <div>
              <Button
                size="sm"
                variant="outline"
                disabled={checklist.length >= 40}
                onClick={() => setChecklist((list) => [...list, { key: nextKey(), label: '' }])}
              >
                <PlusIcon />
                {t('maintenance:plans.addItem')}
              </Button>
            </div>
          </fieldset>
          <M4ProblemAlert error={create.error ?? update.error} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!name.trim() || targets.length === 0 || create.isPending || update.isPending}
            onClick={save}
          >
            {t('maintenance:plans.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function planEvery(p: MaintenancePlan): number {
  const e = p.periodicity.every ?? 0;
  return p.periodicity.kind === 'cycles' ? e / 1000 : e;
}
