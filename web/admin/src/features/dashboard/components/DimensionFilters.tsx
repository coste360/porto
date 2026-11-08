import { useBerthCategories, useBerthGroups, useMap, useMaps } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { localName } from '../../maps/lib';

const ALL = '__all';

/** Dimensions of the manager dashboard (spec 21.2: per marina, map, pier, group, category). */
export type Dims = {
  map_id?: string;
  structure_id?: string;
  group_id?: string;
  category?: string;
};

type Option = { value: string; label: string };

/**
 * Dimension selectors of the manager dashboard (m7-plan §4): map (with more than one), pier (structures of the
 * chosen or only map), berth group and berth category. Each selector appears only when it has options the user
 * may read; changing the map clears the pier. The server applies the dimensions to the tiles and carries them in
 * the drill parameters, so a tile still opens a report equal to it.
 */
export function DimensionFilters({
  marinaId,
  value,
  onChange,
}: {
  marinaId: string;
  value: Dims;
  onChange: (next: Dims) => void;
}) {
  const { t, i18n } = useT();
  const maps = useMaps(marinaId).data ?? [];
  const groups = useBerthGroups(marinaId).data ?? [];
  const categories = (useBerthCategories(marinaId).data?.items ?? []).filter((c) => !c.archived);
  const mapId = value.map_id ?? (maps.length === 1 ? maps[0]!.id : undefined);
  return (
    <>
      {maps.length > 1 ? (
        <DimSelect
          id="dashboard-map"
          label={t('dashboard:dims.map')}
          value={value.map_id}
          options={maps.map((m) => ({ value: m.id, label: localName(m.name, i18n.language) }))}
          onChange={(v) => onChange({ ...value, map_id: v, structure_id: undefined })}
        />
      ) : null}
      {mapId ? (
        <StructureSelect
          marinaId={marinaId}
          mapId={mapId}
          value={value.structure_id}
          onChange={(v) => onChange({ ...value, structure_id: v })}
        />
      ) : null}
      {groups.length > 0 ? (
        <DimSelect
          id="dashboard-group"
          label={t('dashboard:dims.group')}
          value={value.group_id}
          options={groups.map((g) => ({
            value: g.id,
            label: localName(g.name, i18n.language) || g.code,
          }))}
          onChange={(v) => onChange({ ...value, group_id: v })}
        />
      ) : null}
      {categories.length > 0 ? (
        <DimSelect
          id="dashboard-category"
          label={t('dashboard:dims.category')}
          value={value.category}
          options={categories.map((c) => ({
            value: c.code,
            label: localName(c.name, i18n.language) || c.code,
          }))}
          onChange={(v) => onChange({ ...value, category: v })}
        />
      ) : null}
    </>
  );
}

/** Pier selector: the structures of one map. */
function StructureSelect({
  marinaId,
  mapId,
  value,
  onChange,
}: {
  marinaId: string;
  mapId: string;
  value: string | undefined;
  onChange: (v: string | undefined) => void;
}) {
  const { t } = useT();
  const structures = (useMap(marinaId, mapId).data?.structures ?? []).filter(
    (s): s is typeof s & { id: string } => !!s.id,
  );
  if (structures.length === 0) return null;
  return (
    <DimSelect
      id="dashboard-structure"
      label={t('dashboard:dims.structure')}
      value={value}
      options={structures.map((s) => ({ value: s.id, label: s.name || s.code }))}
      onChange={onChange}
    />
  );
}

function DimSelect({
  id,
  label,
  value,
  options,
  onChange,
}: {
  id: string;
  label: string;
  value: string | undefined;
  options: Option[];
  onChange: (v: string | undefined) => void;
}) {
  const { t } = useT();
  return (
    <Field className="lg:max-w-52">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={value ?? ALL} onValueChange={(v) => onChange(v === ALL ? undefined : v)}>
        <SelectTrigger id={id} className="min-h-11 w-full sm:min-h-9">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{t('dashboard:dims.all')}</SelectItem>
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}
