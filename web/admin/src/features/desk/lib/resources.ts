import type { Schemas } from '@porto/shared/api';

type ResourceType = Schemas['ResourceType'];

export type ResourceOption = {
  resource_type: ResourceType;
  resource_id: string;
  code: string;
  areaKind?: Schemas['AreaKind'];
};

/** Resources of a map as combobox options (active elements with an id). */
export function resourceOptions(
  doc: Pick<Schemas['MapDocument'], 'berths' | 'dynamic_areas' | 'land_resources'> | undefined,
  types: readonly ResourceType[],
): ResourceOption[] {
  if (!doc) return [];
  const out: ResourceOption[] = [];
  if (types.includes('berth'))
    for (const b of doc.berths)
      if (b.id && b.active) out.push({ resource_type: 'berth', resource_id: b.id, code: b.code });
  if (types.includes('dynamic_area'))
    for (const a of doc.dynamic_areas)
      if (a.id && a.active)
        out.push({
          resource_type: 'dynamic_area',
          resource_id: a.id,
          code: a.code,
          areaKind: a.kind,
        });
  if (types.includes('land_resource'))
    for (const l of doc.land_resources)
      if (l.id && l.active)
        out.push({ resource_type: 'land_resource', resource_id: l.id, code: l.code });
  return out;
}
