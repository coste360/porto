import { MapIcon } from 'lucide-react';
import { useState } from 'react';

import { useMap, useMaps, type PlacementInput, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { ElementCombobox, LengthInput } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { localName } from '../../maps/lib';
import { PlacementPicker } from '../../operations/public';
import { resourceOptions } from '../lib/resources';
import type { VesselDims } from '../lib/vessel';

type ResourceType = Schemas['ResourceType'];

const encode = (t: ResourceType, id: string) => `${t}:${id}`;

/**
 * Target of a stay: map, resource (berth, dynamic area or land resource) and, for a dynamic area, the position
 * along the axis (and across, on surfaces). "Scegli sulla mappa" opens the operational map picker with the ghost
 * preview when the vessel is known.
 */
export function ResourceField({
  marinaId,
  idPrefix,
  value,
  onChange,
  types = ['berth', 'dynamic_area', 'land_resource'],
  vessel,
  date,
  from,
  to,
}: {
  marinaId: string;
  idPrefix: string;
  value: PlacementInput | null;
  onChange: (p: PlacementInput | null) => void;
  types?: readonly ResourceType[];
  vessel?: VesselDims | null;
  date: string;
  from: string;
  to: string;
}) {
  const { t, i18n } = useT();
  const maps = useMaps(marinaId);
  const [chosenMap, setMapId] = useState<string>('');
  const [picking, setPicking] = useState(false);
  const mapId = chosenMap || (maps.data?.[0]?.id ?? '');
  const doc = useMap(marinaId, mapId);
  const options = resourceOptions(mapId ? doc.data : undefined, types);
  const current = value ? options.find((o) => o.resource_id === value.resource_id) : undefined;

  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-2">
        {(maps.data?.length ?? 0) > 1 ? (
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-map`}>{t('desk:fields.map')}</FieldLabel>
            <Select value={mapId} onValueChange={setMapId}>
              <SelectTrigger id={`${idPrefix}-map`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(maps.data ?? []).map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {localName(m.name, i18n.language)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ) : null}
        <Field>
          <FieldLabel htmlFor={`${idPrefix}-resource`}>{t('desk:fields.resource')}</FieldLabel>
          <ElementCombobox
            id={`${idPrefix}-resource`}
            options={options.map((o) => ({
              value: encode(o.resource_type, o.resource_id),
              label: `${o.code} · ${t(`occupancyEnums:resourceType.${o.resource_type}`)}`,
              keywords: [o.code],
            }))}
            value={value ? encode(value.resource_type, value.resource_id) : null}
            onChange={(v) => {
              const o = options.find((x) => encode(x.resource_type, x.resource_id) === v);
              onChange(o ? { resource_type: o.resource_type, resource_id: o.resource_id } : null);
            }}
            placeholder={t('desk:fields.resourcePlaceholder')}
            searchPlaceholder={t('desk:fields.resourceSearch')}
            emptyText={t('desk:fields.noResources')}
          />
        </Field>
      </div>
      {value?.resource_type === 'dynamic_area' ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-x`}>{t('desk:fields.positionX')}</FieldLabel>
            <LengthInput
              id={`${idPrefix}-x`}
              locale={i18n.language}
              value={value.x_cm ?? null}
              onChange={(x) => {
                const { x_cm: _x, ...rest } = value;
                onChange(x === null ? rest : { ...rest, x_cm: x });
              }}
            />
          </Field>
          {current?.areaKind === 'surface' ? (
            <Field>
              <FieldLabel htmlFor={`${idPrefix}-y`}>{t('desk:fields.positionY')}</FieldLabel>
              <LengthInput
                id={`${idPrefix}-y`}
                locale={i18n.language}
                value={value.y_cm ?? null}
                onChange={(y) => {
                  const { y_cm: _y, ...rest } = value;
                  onChange(y === null ? rest : { ...rest, y_cm: y });
                }}
              />
            </Field>
          ) : null}
        </div>
      ) : null}
      {vessel ? (
        <div>
          <Button type="button" variant="outline" size="sm" onClick={() => setPicking(true)}>
            <MapIcon />
            {t('desk:fields.pickOnMap')}
          </Button>
          <Dialog open={picking} onOpenChange={setPicking}>
            <DialogContent className="sm:max-w-4xl">
              <DialogHeader>
                <DialogTitle>{t('desk:fields.pickOnMap')}</DialogTitle>
              </DialogHeader>
              {picking ? (
                <PlacementPicker
                  marinaId={marinaId}
                  {...(mapId ? { mapId } : {})}
                  date={date}
                  vessel={vessel}
                  from={from}
                  to={to}
                  onPick={(p) => {
                    onChange(p);
                    setPicking(false);
                  }}
                  onCancel={() => setPicking(false)}
                />
              ) : null}
            </DialogContent>
          </Dialog>
        </div>
      ) : null}
    </div>
  );
}
