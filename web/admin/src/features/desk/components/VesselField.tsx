import { useDeferredValue, useEffect, useState } from 'react';

import { useVessel, useVessels, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ElementCombobox, LengthInput } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { HULL_TYPES } from '../../registry/lib/enums';
import { NO_VESSEL, emptyNewVessel, type VesselValue } from '../lib/vessel';

type HullType = Schemas['HullType'];

/** Existing vessel (searchable combobox) or, when `allowNew`, a vessel created on the spot with estimated dimensions. */
export function VesselField({
  marinaId,
  idPrefix,
  value,
  onChange,
  allowNew = false,
}: {
  marinaId: string;
  idPrefix: string;
  value: VesselValue;
  onChange: (v: VesselValue) => void;
  allowNew?: boolean;
}) {
  const { t, i18n } = useT();
  const [q, setQ] = useState('');
  const deferred = useDeferredValue(q.trim());
  const vessels = useVessels(marinaId, { q: deferred });
  const items = vessels.data?.pages.flatMap((p) => p.items) ?? [];
  const options = items.map((v) => ({
    value: v.id,
    label: `${v.name} · ${formatCm(v.effective_loa_cm, i18n.language)} × ${formatCm(v.effective_beam_cm, i18n.language)}`,
    keywords: v.registration ? [v.registration] : [],
  }));
  if (value.kind === 'existing' && value.id && !options.some((o) => o.value === value.id))
    options.unshift({ value: value.id, label: value.label, keywords: [] });

  return (
    <div className="flex flex-col gap-3">
      <VesselLabel marinaId={marinaId} value={value} onChange={onChange} />
      {allowNew ? (
        <ToggleGroup
          type="single"
          variant="outline"
          value={value.kind}
          onValueChange={(k) => {
            if (k === 'new' && value.kind !== 'new') onChange(emptyNewVessel());
            if (k === 'existing' && value.kind !== 'existing') onChange(NO_VESSEL);
          }}
          aria-label={t('desk:fields.vesselMode')}
        >
          <ToggleGroupItem value="existing">{t('desk:fields.vesselExisting')}</ToggleGroupItem>
          <ToggleGroupItem value="new">{t('desk:fields.vesselNew')}</ToggleGroupItem>
        </ToggleGroup>
      ) : null}
      {value.kind === 'existing' ? (
        <Field>
          <FieldLabel htmlFor={`${idPrefix}-vessel`}>{t('desk:fields.vessel')}</FieldLabel>
          <ElementCombobox
            id={`${idPrefix}-vessel`}
            options={options}
            value={value.id}
            onSearch={setQ}
            onChange={(id) => {
              const v = items.find((x) => x.id === id);
              onChange(
                id && v
                  ? {
                      kind: 'existing',
                      id,
                      label: options.find((o) => o.value === id)?.label ?? v.name,
                      dims: {
                        loa_cm: v.effective_loa_cm,
                        beam_cm: v.effective_beam_cm,
                        hull_type: v.hull_type,
                      },
                    }
                  : NO_VESSEL,
              );
            }}
            placeholder={t('desk:fields.vesselPlaceholder')}
            searchPlaceholder={t('desk:fields.vesselSearch')}
            emptyText={t('desk:fields.noVessels')}
          />
        </Field>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field className="sm:col-span-2">
            <FieldLabel htmlFor={`${idPrefix}-name`}>{t('desk:fields.vesselName')}</FieldLabel>
            <Input
              id={`${idPrefix}-name`}
              value={value.name}
              maxLength={100}
              onChange={(e) => onChange({ ...value, name: e.target.value })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-loa`}>{t('desk:fields.loa')}</FieldLabel>
            <LengthInput
              id={`${idPrefix}-loa`}
              locale={i18n.language}
              value={value.loaCm}
              onChange={(loaCm) => onChange({ ...value, loaCm })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-beam`}>{t('desk:fields.beam')}</FieldLabel>
            <LengthInput
              id={`${idPrefix}-beam`}
              locale={i18n.language}
              value={value.beamCm}
              onChange={(beamCm) => onChange({ ...value, beamCm })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-hull`}>{t('desk:fields.hullType')}</FieldLabel>
            <HullSelect
              id={`${idPrefix}-hull`}
              value={value.hullType}
              onChange={(hullType) => onChange({ ...value, hullType })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-draft`}>{t('desk:fields.draft')}</FieldLabel>
            <LengthInput
              id={`${idPrefix}-draft`}
              locale={i18n.language}
              value={value.draftCm}
              onChange={(draftCm) => onChange({ ...value, draftCm })}
            />
          </Field>
        </div>
      )}
    </div>
  );
}

export function HullSelect({
  id,
  value,
  onChange,
}: {
  id: string;
  value: HullType;
  onChange: (h: HullType) => void;
}) {
  const { t } = useT();
  return (
    <Select value={value} onValueChange={(v) => onChange(v as HullType)}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {HULL_TYPES.map((h) => (
          <SelectItem key={h} value={h}>
            {t(`vessels:hullTypes.${h}`)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** Fills the label and dimensions of a vessel known only by id (a prefill); renders nothing. */
export function VesselLabel({
  marinaId,
  value,
  onChange,
}: {
  marinaId: string;
  value: VesselValue;
  onChange: (v: VesselValue) => void;
}) {
  if (value.kind !== 'existing' || !value.id || value.label) return null;
  return <VesselLabelLoader marinaId={marinaId} vesselId={value.id} onChange={onChange} />;
}

function VesselLabelLoader({
  marinaId,
  vesselId,
  onChange,
}: {
  marinaId: string;
  vesselId: string;
  onChange: (v: VesselValue) => void;
}) {
  const v = useVessel(marinaId, vesselId);
  const data = v.data;
  useEffect(() => {
    if (data)
      onChange({
        kind: 'existing',
        id: data.id,
        label: data.name,
        dims: {
          loa_cm: data.effective_loa_cm,
          beam_cm: data.effective_beam_cm,
          hull_type: data.hull_type,
        },
      });
  }, [data, onChange]);
  return null;
}
