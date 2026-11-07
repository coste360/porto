import { useMemo } from 'react';

import { useMap, useMaps } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  DEFAULT_LAYERS,
  HarborMap,
  buildScene,
  entriesOf,
  indexScene,
  useMapViewport,
} from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { ScrollArea } from '@porto/shared/ui/scroll-area';

import { useCurrentMarina } from '../../../auth/hooks';
import { EnumSelect } from '../../contracts-m4/components/kit';
import { localName } from '../../maps/lib';

/**
 * Selection of berths on a harbour map (communications audience, m4-plan §8.1): click berths on the map to toggle
 * them, or use the list next to it (keyboard). Only berths are selectable.
 */
export function MapSelection({
  mapId,
  selected,
  onMap,
  onChange,
}: {
  mapId: string;
  selected: readonly string[];
  onMap: (mapId: string) => void;
  onChange: (ids: string[], mapId: string) => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const maps = useMaps(marinaId);
  const current = mapId || maps.data?.[0]?.id || '';
  const doc = useMap(marinaId, current).data;
  const content = useMemo(
    () => ({ minX: 0, minY: 0, maxX: doc?.width_cm ?? 10_000, maxY: doc?.height_cm ?? 10_000 }),
    [doc?.width_cm, doc?.height_cm],
  );
  const { viewport, bindContainer, bindSvg, gestures } = useMapViewport(content);
  const scene = useMemo(() => (doc ? buildScene(entriesOf(doc)) : []), [doc]);
  const index = useMemo(() => indexScene(scene), [scene]);
  const berths = useMemo(() => (doc?.berths ?? []).filter((b) => b.id), [doc]);
  const set = useMemo(() => new Set(selected), [selected]);
  const toggle = (id: string) => {
    if (!berths.some((b) => b.id === id)) return;
    onChange(set.has(id) ? selected.filter((x) => x !== id) : [...selected, id], current);
  };
  return (
    <div className="flex flex-col gap-2">
      {(maps.data?.length ?? 0) > 1 ? (
        <EnumSelect
          id="comm-map"
          label={t('communications:audience.map')}
          values={(maps.data ?? []).map((m) => m.id)}
          labelOf={(id) => localName(maps.data?.find((m) => m.id === id)?.name, i18n.language)}
          value={current}
          onChange={(id) => {
            onMap(id);
            onChange([], id);
          }}
        />
      ) : null}
      <div className="grid gap-3 lg:grid-cols-3">
        <div
          ref={bindContainer}
          className="bg-muted aspect-video w-full overflow-hidden rounded-md border lg:col-span-2"
        >
          {doc ? (
            <HarborMap
              svgRef={bindSvg}
              meta={doc}
              scene={scene}
              index={index}
              viewport={viewport}
              layers={DEFAULT_LAYERS}
              selected={set}
              interactive
              aria-label={t('communications:audience.mapCanvas')}
              role="group"
              onElementPointerDown={(ref) => toggle(ref)}
              onElementActivate={(ref) => toggle(ref)}
              onBackgroundPointerDown={(e) => gestures.onPointerDown(e, true)}
              onPointerUp={(e) => gestures.onPointerUp(e)}
            />
          ) : null}
        </div>
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2 text-sm">
            <span data-testid="map-selection-count">
              {t('communications:audience.selected', { count: selected.length })}
            </span>
            <Button
              size="sm"
              variant="ghost"
              onClick={() =>
                onChange(
                  berths.map((b) => b.id!),
                  current,
                )
              }
            >
              {t('communications:audience.selectAll')}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => onChange([], current)}>
              {t('communications:audience.clear')}
            </Button>
          </div>
          <ScrollArea className="h-64 rounded-md border p-2">
            <ul className="flex flex-col gap-1">
              {berths.map((b) => (
                <li key={b.id}>
                  <Field orientation="horizontal">
                    <Checkbox
                      id={`comm-berth-${b.id}`}
                      checked={set.has(b.id!)}
                      onCheckedChange={() => toggle(b.id!)}
                    />
                    <FieldLabel htmlFor={`comm-berth-${b.id}`}>{b.code}</FieldLabel>
                  </Field>
                </li>
              ))}
            </ul>
          </ScrollArea>
        </div>
      </div>
    </div>
  );
}
