import { SlidersHorizontalIcon, XIcon } from 'lucide-react';
import { useState } from 'react';

import { useMaps } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import type { ReportSearch } from '@porto/shared/lib/reportLinks';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@porto/shared/ui/sheet';

import { PeriodSelector } from '../../dashboard/components/PeriodSelector';
import {
  PERIOD_PRESETS,
  presetPeriod,
  type Period,
  type PeriodPreset,
} from '../../dashboard/lib/period';
import { localName } from '../../maps/lib';
import { FILTER_VALUES, FILTER_VALUE_GROUP, type FilterKey, type ReportDef } from '../lib/catalog';

const ALL = '__all';

/** Search keys a control of the bar edits (others are shown as removable chips, e.g. drill dimensions). */
function controlledKeys(def: ReportDef): string[] {
  return [
    'from',
    'to',
    'page',
    'page_size',
    'group_by',
    'section',
    'lang',
    'format',
    ...def.filters,
    ...(def.dims ? ['map_id'] : []),
  ];
}

export type ReportFilterBarProps = {
  def: ReportDef;
  marinaId: string;
  search: ReportSearch;
  period: Period;
  today: string;
  onPatch: (patch: Record<string, string | undefined>) => void;
};

/** The preset whose period equals from/to (custom otherwise). */
function presetOf(period: Period, today: string): PeriodPreset {
  return (
    PERIOD_PRESETS.find((p) => {
      if (p === 'custom') return false;
      const q = presetPeriod(p, today);
      return q.from === period.from && q.to === period.to;
    }) ?? 'custom'
  );
}

/**
 * Filter bar of a report page (m7-plan §10.1): period presets and dates, group_by, the report's filters and the
 * map dimension; inline from the small breakpoint up, inside a Sheet on a phone. Every control writes the URL
 * search, so the page is always its own request (and a tile link reproduces it).
 */
export function ReportFilterBar(props: ReportFilterBarProps) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3">
      <div className="hidden sm:flex sm:flex-wrap sm:items-end sm:gap-3">
        <FilterControls {...props} idPrefix="report" />
      </div>
      <div className="sm:hidden">
        <Sheet>
          <SheetTrigger asChild>
            <Button variant="outline" className="min-h-11 w-full">
              <SlidersHorizontalIcon />
              {t('reports:filters.open')}
            </Button>
          </SheetTrigger>
          <SheetContent side="bottom" className="max-h-dvh overflow-y-auto">
            <SheetHeader>
              <SheetTitle>{t('reports:filters.title')}</SheetTitle>
              <SheetDescription>{t('reports:filters.description')}</SheetDescription>
            </SheetHeader>
            <div className="flex flex-col gap-3 px-4 pb-4">
              <FilterControls {...props} idPrefix="report-sheet" />
            </div>
          </SheetContent>
        </Sheet>
      </div>
      <ActiveChips {...props} />
    </div>
  );
}

function FilterControls({
  def,
  marinaId,
  search,
  period,
  today,
  onPatch,
  idPrefix,
}: ReportFilterBarProps & { idPrefix: string }) {
  const { t } = useT();
  const [custom, setCustom] = useState(false);
  const preset = custom ? 'custom' : presetOf(period, today);
  return (
    <>
      <PeriodSelector
        idPrefix={idPrefix}
        presets={PERIOD_PRESETS}
        preset={preset}
        period={period}
        onPreset={(p) => {
          setCustom(p === 'custom');
          if (p === 'custom') {
            onPatch({ from: period.from, to: period.to });
            return;
          }
          const next = presetPeriod(p, today);
          onPatch({ from: next.from, to: next.to });
        }}
        onPeriod={(p) => onPatch({ from: p.from, to: p.to })}
      />
      {def.groupBy.length > 0 ? (
        <SelectField
          id={`${idPrefix}-group-by`}
          label={t('reports:filters.groupBy')}
          value={search.group_by}
          values={def.groupBy}
          labelOf={(v) => t(`reports:groupBy.${v}`, { defaultValue: v })}
          allLabel={t('reports:filters.noGrouping')}
          onChange={(v) => onPatch({ group_by: v })}
        />
      ) : null}
      {def.filters.map((f) =>
        f === 'as_of' ? (
          <Field key={f} className="sm:max-w-44">
            <FieldLabel htmlFor={`${idPrefix}-as-of`}>{t('reports:filters.as_of')}</FieldLabel>
            <DatePicker
              id={`${idPrefix}-as-of`}
              value={search.as_of ?? null}
              onChange={(d) => onPatch({ as_of: d ?? undefined })}
            />
          </Field>
        ) : (
          <SelectField
            key={f}
            id={`${idPrefix}-${f}`}
            label={t(`reports:filters.${f}`)}
            value={search[f]}
            values={FILTER_VALUES[f]}
            labelOf={(v) => t(`${FILTER_VALUE_GROUP[f]}.${v}`, { defaultValue: v })}
            allLabel={t(defaultLabel(f))}
            onChange={(v) => onPatch({ [f]: v })}
          />
        ),
      )}
      {def.dims ? (
        <MapField
          id={`${idPrefix}-map`}
          marinaId={marinaId}
          value={search.map_id}
          onChange={(v) => onPatch({ map_id: v })}
        />
      ) : null}
    </>
  );
}

/** Map dimension (only with more than one map). */
function MapField({
  id,
  marinaId,
  value,
  onChange,
}: {
  id: string;
  marinaId: string;
  value: string | undefined;
  onChange: (v: string | undefined) => void;
}) {
  const { t, i18n } = useT();
  const maps = useMaps(marinaId);
  const items = maps.data ?? [];
  if (items.length < 2) return null;
  return (
    <SelectField
      id={id}
      label={t('reports:filters.map_id')}
      value={value}
      values={items.map((m) => m.id)}
      labelOf={(mid) => {
        const m = items.find((x) => x.id === mid);
        return m ? localName(m.name, i18n.language) : mid;
      }}
      allLabel={t('reports:filters.all')}
      onChange={onChange}
    />
  );
}

/** Label of the "no value" option of a filter (the server default). */
function defaultLabel(f: FilterKey): string {
  switch (f) {
    case 'variant':
    case 'family':
    case 'basis':
    case 'suggestions':
      return `reports:filters.default.${f}`;
    default:
      return 'reports:filters.all';
  }
}

function SelectField({
  id,
  label,
  value,
  values,
  labelOf,
  allLabel,
  onChange,
}: {
  id: string;
  label: string;
  value: string | undefined;
  values: readonly string[];
  labelOf: (v: string) => string;
  allLabel: string;
  onChange: (v: string | undefined) => void;
}) {
  return (
    <Field className="sm:max-w-56">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={value ?? ALL} onValueChange={(v) => onChange(v === ALL ? undefined : v)}>
        <SelectTrigger id={id} className="min-h-11 w-full sm:min-h-9">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{allLabel}</SelectItem>
          {values.map((v) => (
            <SelectItem key={v} value={v}>
              {labelOf(v)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}

/** Filters of the search without a control (drill dimensions, customer, item): shown and removable. */
function ActiveChips({ def, search, onPatch }: ReportFilterBarProps) {
  const { t } = useT();
  const controlled = controlledKeys(def);
  const extra = Object.entries(search).filter(([k]) => !controlled.includes(k));
  if (extra.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-2" data-testid="report-active-filters">
      <span className="text-muted-foreground text-sm">{t('reports:filters.active')}</span>
      {extra.map(([k, v]) => (
        <Badge key={k} variant="secondary" className="gap-1">
          {t(`reports:filters.${k}`, { defaultValue: k })}: {v}
          <Button
            variant="ghost"
            size="icon"
            className="size-5"
            aria-label={t('reports:filters.remove', {
              name: t(`reports:filters.${k}`, { defaultValue: k }),
            })}
            onClick={() => onPatch({ [k]: undefined })}
          >
            <XIcon />
          </Button>
        </Badge>
      ))}
    </div>
  );
}
