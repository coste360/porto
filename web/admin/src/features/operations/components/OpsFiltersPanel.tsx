import type { BerthGroup, MapDocument } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';

import { localName } from '../../maps/lib';
import { hasFilters, type ContractFilter, type OpsFilters } from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { LengthInput } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

const ALL = '__all__';
const CONTRACT_FILTERS: readonly ContractFilter[] = [
  'annual',
  'seasonal',
  'monthly',
  'period',
  'transit',
  'none',
];

/** Filters combined with the lens (spec 9.2): excluded elements turn grey, they never disappear. */
export function OpsFiltersPanel({
  doc,
  groups,
  filters,
  onChange,
}: {
  doc: MapDocument;
  groups: readonly BerthGroup[];
  filters: OpsFilters;
  onChange: (f: OpsFilters) => void;
}) {
  const { t, i18n } = useT();
  const categories = [
    ...new Set([...doc.berths, ...doc.dynamic_areas].map((e) => e.price_class).filter(Boolean)),
  ].sort() as string[];
  const set = <K extends keyof OpsFilters>(k: K, v: OpsFilters[K] | undefined) => {
    const next = { ...filters };
    if (v === undefined || v === '') delete next[k];
    else next[k] = v;
    onChange(next);
  };
  const select = (
    id: string,
    label: string,
    value: string | undefined,
    options: { value: string; label: string }[],
    onValue: (v: string | undefined) => void,
  ) => (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id}>{label}</Label>
      <Select value={value ?? ALL} onValueChange={(v) => onValue(v === ALL ? undefined : v)}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{t('opsMap:filters.all')}</SelectItem>
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
  return (
    <section aria-label={t('opsMap:filters.label')} className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <Label htmlFor="ops-filter-text">{t('opsMap:filters.text')}</Label>
        <Input
          id="ops-filter-text"
          value={filters.text ?? ''}
          placeholder={t('opsMap:filters.textPlaceholder')}
          onChange={(e) => set('text', e.target.value)}
        />
      </div>
      {select(
        'ops-filter-structure',
        t('opsMap:filters.structure'),
        filters.structureId,
        doc.structures.filter((s) => s.id).map((s) => ({ value: s.id!, label: s.code })),
        (v) => set('structureId', v),
      )}
      {groups.length > 0
        ? select(
            'ops-filter-group',
            t('opsMap:filters.group'),
            filters.groupId,
            groups.map((g) => ({ value: g.id, label: localName(g.name, i18n.language) })),
            (v) => set('groupId', v),
          )
        : null}
      {categories.length > 0
        ? select(
            'ops-filter-category',
            t('opsMap:filters.category'),
            filters.category,
            categories.map((c) => ({ value: c, label: c })),
            (v) => set('category', v),
          )
        : null}
      {select(
        'ops-filter-contract',
        t('opsMap:filters.contractType'),
        filters.contractType,
        CONTRACT_FILTERS.map((c) => ({ value: c, label: t(`opsMap:filters.contract.${c}`) })),
        (v) => set('contractType', v as ContractFilter | undefined),
      )}
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <Label htmlFor="ops-filter-loa-min">{t('opsMap:filters.loaMin')}</Label>
          <LengthInput
            id="ops-filter-loa-min"
            locale={i18n.language}
            value={filters.loaMin ?? null}
            onChange={(v) => set('loaMin', v ?? undefined)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="ops-filter-loa-max">{t('opsMap:filters.loaMax')}</Label>
          <LengthInput
            id="ops-filter-loa-max"
            locale={i18n.language}
            value={filters.loaMax ?? null}
            onChange={(v) => set('loaMax', v ?? undefined)}
          />
        </div>
      </div>
      <Button
        variant="outline"
        size="sm"
        disabled={!hasFilters(filters)}
        onClick={() => onChange({})}
      >
        {t('opsMap:filters.clear')}
      </Button>
    </section>
  );
}
