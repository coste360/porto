// Building blocks of the maintenance pages (m9-plan §12.2): section tabs, enum badges, the element picker (map →
// element type → element, or equipment) and the conflict list of a refused schedule.
import { Link } from '@tanstack/react-router';
import { useEffect, useMemo } from 'react';

import {
  useEquipmentList,
  useMap,
  useMaps,
  type MaintenanceConflict,
  type MaintenanceElementType,
  type MaintenanceTask,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { ElementCombobox, type ComboboxOption } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { ELEMENT_TYPES } from '../lib';

export type ElementValue = {
  element_type: MaintenanceElementType;
  element_id: string;
  code?: string;
};

/** Section tabs of the maintenance pages. */
export function MaintenanceNav({
  marinaId,
  current,
}: {
  marinaId: string;
  current: 'tasks' | 'plans' | 'equipment';
}) {
  const { t } = useT();
  const cls = (on: boolean) =>
    on
      ? 'border-primary text-foreground border-b-2 px-3 py-2 text-sm font-medium'
      : 'text-muted-foreground hover:text-foreground px-3 py-2 text-sm';
  return (
    <nav aria-label={t('maintenance:nav.sections')} className="flex gap-1 border-b">
      <Link
        to="/m/$marinaId/maintenance"
        params={{ marinaId }}
        className={cls(current === 'tasks')}
      >
        {t('maintenance:nav.tasks')}
      </Link>
      <Link
        to="/m/$marinaId/maintenance/plans"
        params={{ marinaId }}
        className={cls(current === 'plans')}
      >
        {t('maintenance:nav.plans')}
      </Link>
      <Link
        to="/m/$marinaId/maintenance/equipment"
        params={{ marinaId }}
        className={cls(current === 'equipment')}
      >
        {t('maintenance:nav.equipment')}
      </Link>
    </nav>
  );
}

const STATUS_VARIANT: Record<
  MaintenanceTask['status'],
  'default' | 'secondary' | 'outline' | 'destructive'
> = {
  open: 'outline',
  scheduled: 'secondary',
  in_progress: 'default',
  done: 'secondary',
  cancelled: 'outline',
};

export function TaskStatusBadge({ status }: { status: MaintenanceTask['status'] }) {
  const { t } = useT();
  return <Badge variant={STATUS_VARIANT[status]}>{t(`maintenance:enums.status.${status}`)}</Badge>;
}

export function PriorityBadge({ priority }: { priority: MaintenanceTask['priority'] }) {
  const { t } = useT();
  const variant =
    priority === 'urgent' ? 'destructive' : priority === 'high' ? 'default' : 'outline';
  return <Badge variant={variant}>{t(`maintenance:enums.priority.${priority}`)}</Badge>;
}

/** The element of a task: type and display code. */
export function ElementLabel({ task }: { task: Pick<MaintenanceTask, 'element' | 'map_id'> }) {
  const { t } = useT();
  if (!task.element)
    return <span className="text-muted-foreground">{t('maintenance:task.mapPosition')}</span>;
  return (
    <span>
      {t(`maintenance:enums.elementType.${task.element.element_type}`)} {task.element.code ?? ''}
    </span>
  );
}

/** Options of the elements of a type on a map (saved elements only). */
function useElementOptions(
  marinaId: string,
  mapId: string,
  type: MaintenanceElementType,
): ComboboxOption[] {
  const map = useMap(marinaId, mapId);
  const equipment = useEquipmentList(marinaId, { active: true });
  return useMemo(() => {
    if (type === 'equipment') {
      return (equipment.data?.items ?? []).map((e) => ({
        value: e.id,
        label: `${e.code} ${e.name}`,
      }));
    }
    const doc = map.data;
    if (!doc) return [];
    const list: readonly { id?: string | null; code: string }[] =
      type === 'berth'
        ? doc.berths
        : type === 'structure'
          ? doc.structures
          : type === 'dynamic_area'
            ? doc.dynamic_areas
            : type === 'land_resource'
              ? doc.land_resources
              : doc.pedestals;
    return list.filter((x) => !!x.id).map((x) => ({ value: x.id!, label: x.code }));
  }, [type, map.data, equipment.data]);
}

/** Map, element type and element of a task or plan target. */
export function ElementPicker({
  marinaId,
  idPrefix,
  value,
  onChange,
  types = ELEMENT_TYPES,
}: {
  marinaId: string;
  idPrefix: string;
  value: { mapId: string; type: MaintenanceElementType; id: string | null };
  onChange: (v: {
    mapId: string;
    type: MaintenanceElementType;
    id: string | null;
    code?: string;
  }) => void;
  types?: readonly MaintenanceElementType[];
}) {
  const { t } = useT();
  const maps = useMaps(marinaId);
  const mapId = value.mapId || maps.data?.[0]?.id || '';
  const options = useElementOptions(marinaId, mapId, value.type);
  // The first map is shown as chosen: make it the value too, so a report or task "on the map" can be sent
  // without opening the select.
  useEffect(() => {
    if (!value.mapId && mapId) onChange({ ...value, mapId });
  }, [mapId, value, onChange]);
  return (
    <div className="grid gap-3 sm:grid-cols-3">
      {value.type !== 'equipment' ? (
        <Field>
          <FieldLabel htmlFor={`${idPrefix}-map`}>{t('maintenance:element.map')}</FieldLabel>
          <Select value={mapId} onValueChange={(v) => onChange({ ...value, mapId: v, id: null })}>
            <SelectTrigger id={`${idPrefix}-map`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(maps.data ?? []).map((m) => (
                <SelectItem key={m.id} value={m.id}>
                  {m.name.it}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      ) : null}
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-type`}>{t('maintenance:element.type')}</FieldLabel>
        <Select
          value={value.type}
          onValueChange={(v) =>
            onChange({ ...value, mapId, type: v as MaintenanceElementType, id: null })
          }
        >
          <SelectTrigger id={`${idPrefix}-type`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {types.map((ty) => (
              <SelectItem key={ty} value={ty}>
                {t(`maintenance:enums.elementType.${ty}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-element`}>{t('maintenance:element.element')}</FieldLabel>
        <ElementCombobox
          id={`${idPrefix}-element`}
          options={options}
          value={value.id}
          onChange={(id) =>
            onChange({
              ...value,
              mapId,
              id,
              code: options.find((o) => o.value === id)?.label ?? '',
            })
          }
          placeholder={t('maintenance:element.choose')}
          searchPlaceholder={t('maintenance:element.search')}
          emptyText={t('maintenance:element.none')}
        />
      </Field>
    </div>
  );
}

/** The vessels on the nights to book, each with its alternatives (the office moves them, then retries). */
export function ConflictList({ conflicts }: { conflicts: readonly MaintenanceConflict[] }) {
  const { t } = useT();
  return (
    <div
      role="alert"
      className="flex flex-col gap-2 rounded-md border border-destructive p-3"
      data-testid="maintenance-conflicts"
    >
      <p className="text-sm font-medium">
        {t('maintenance:schedule.conflicts', { count: conflicts.length })}
      </p>
      <ul className="flex flex-col gap-2">
        {conflicts.map((c) => (
          <li key={c.allocation_id} className="text-sm" data-testid="maintenance-conflict">
            <span>
              {t('maintenance:schedule.conflictNights', {
                from: formatIsoDate(c.from),
                to: formatIsoDate(c.to),
              })}
            </span>
            {c.alternatives.length > 0 ? (
              <span className="text-muted-foreground">
                {' '}
                {t('maintenance:schedule.alternatives', {
                  list: c.alternatives
                    .map((a) => a.segments.map((s) => s.code).join(' + '))
                    .join(', '),
                })}
              </span>
            ) : (
              <span className="text-muted-foreground">
                {' '}
                {t('maintenance:schedule.noAlternative')}
              </span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
