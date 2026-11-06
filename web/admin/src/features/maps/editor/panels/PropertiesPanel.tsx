import type {
  Berth,
  BerthGroup,
  Decoration,
  DynamicArea,
  HarborSettings,
  LandResource,
  Pedestal,
  Structure,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { KIT_ASSETS, MIN_RECT_SIDE_CM, type SceneEntry } from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { Separator } from '@porto/shared/ui/separator';

import { metaCommand, updateCommand, type Command } from '../model/commands';
import type { EditorDocument } from '../model/document';
import { priceClassFor } from '../model/priceClass';
import { MIXED, common } from './common';
import { AngleField, BoolField, CmField, IntField, SelectField, TextField } from './fields';

const MOORING_TYPES: readonly Berth['mooring_type'][] = [
  'mooring_line',
  'finger',
  'alongside',
  'piles',
  'buoy',
];
const SIDES: readonly Berth['side'][] = ['left', 'right', 'head', 'none'];
const LAND_KINDS: readonly LandResource['kind'][] = [
  'parking',
  'box',
  'storeroom',
  'locker',
  'cradle',
  'trailer_spot',
  'custom',
];
const MODES: readonly NonNullable<DynamicArea['mooring_mode']>[] = ['stern_to', 'alongside'];
const GRID_ROTATIONS = ['0', '900', '1800', '2700'] as const;

export type PropertiesPanelProps = {
  doc: EditorDocument;
  selection: ReadonlySet<string>;
  groups: readonly BerthGroup[];
  settings?: HarborSettings;
  onCommand: (cmd: Command) => void;
};

type Entries<K extends SceneEntry['kind']> = Extract<SceneEntry, { kind: K }>[];

/** Contextual properties of the selection (spec 8.1): one kind edits its fields, mixed values as placeholder. */
export function PropertiesPanel({
  doc,
  selection,
  groups,
  settings,
  onCommand,
}: PropertiesPanelProps) {
  const { t } = useT();
  const entries = [...selection]
    .map((r) => doc.elements.get(r))
    .filter((e): e is SceneEntry => !!e);
  if (entries.length === 0) return <MapProperties doc={doc} onCommand={onCommand} />;
  const kinds = new Set(entries.map((e) => e.kind));
  const refs = [...selection];
  const update = <K extends SceneEntry['kind']>(
    kind: K,
    patch: (el: Extract<SceneEntry, { kind: K }>['el']) => object,
  ) =>
    onCommand(
      updateCommand(
        doc,
        refs,
        (e) =>
          e.kind === kind ? ({ kind, el: { ...e.el, ...patch(e.el as never) } } as SceneEntry) : e,
        'editor:commands.properties',
      ),
    );
  const set = <K extends SceneEntry['kind']>(kind: K, patch: object) => update(kind, () => patch);

  return (
    <div className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">
        {t('editor:properties.selected', { count: entries.length })}
      </p>
      {kinds.size > 1 ? <p className="text-sm">{t('editor:properties.mixedKinds')}</p> : null}
      {kinds.has('berth') ? (
        <BerthFields
          berths={entries.filter((e) => e.kind === 'berth') as Entries<'berth'>}
          set={(p) => set('berth', p)}
          update={(fn) => update('berth', fn)}
          settings={settings}
        />
      ) : null}
      {kinds.has('structure') && kinds.size === 1 ? (
        <StructureFields items={entries as Entries<'structure'>} set={(p) => set('structure', p)} />
      ) : null}
      {kinds.has('area') && kinds.size === 1 ? (
        <AreaFields items={entries as Entries<'area'>} set={(p) => set('area', p)} />
      ) : null}
      {kinds.has('land') && kinds.size === 1 ? (
        <LandFields items={entries as Entries<'land'>} set={(p) => set('land', p)} />
      ) : null}
      {kinds.has('pedestal') && kinds.size === 1 ? (
        <PedestalFields items={entries as Entries<'pedestal'>} set={(p) => set('pedestal', p)} />
      ) : null}
      {kinds.has('decoration') && kinds.size === 1 ? (
        <DecorationFields
          items={entries as Entries<'decoration'>}
          set={(p) => set('decoration', p)}
        />
      ) : null}
      {(kinds.has('berth') || kinds.has('area') || kinds.has('land')) && groups.length > 0 ? (
        <>
          <Separator />
          <GroupMembership doc={doc} refs={refs} groups={groups} onCommand={onCommand} />
        </>
      ) : null}
    </div>
  );
}

function MapProperties({
  doc,
  onCommand,
}: {
  doc: EditorDocument;
  onCommand: (cmd: Command) => void;
}) {
  const { t } = useT();
  const m = doc.meta;
  return (
    <div className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">{t('editor:properties.mapHint')}</p>
      <TextField
        id="map-name-it"
        label={t('maps:create.nameIt')}
        value={m.name.it}
        maxLength={200}
        required
        onCommit={(v) =>
          onCommand(metaCommand(doc, { name: { ...m.name, it: v } }, 'editor:commands.properties'))
        }
      />
      <TextField
        id="map-name-en"
        label={t('maps:create.nameEn')}
        value={m.name.en}
        maxLength={200}
        onCommit={(v) => {
          const { en: _en, ...rest } = m.name;
          onCommand(
            metaCommand(doc, { name: v ? { ...rest, en: v } : rest }, 'editor:commands.properties'),
          );
        }}
      />
      <CmField
        id="map-width"
        label={t('maps:create.width')}
        value={m.width_cm}
        min={100}
        onCommit={(v) =>
          v && onCommand(metaCommand(doc, { width_cm: v }, 'editor:commands.properties'))
        }
      />
      <CmField
        id="map-height"
        label={t('maps:create.height')}
        value={m.height_cm}
        min={100}
        onCommit={(v) =>
          v && onCommand(metaCommand(doc, { height_cm: v }, 'editor:commands.properties'))
        }
      />
      <CmField
        id="map-grid"
        label={t('maps:create.grid')}
        value={m.grid_cm}
        onCommit={(v) =>
          v && onCommand(metaCommand(doc, { grid_cm: v }, 'editor:commands.properties'))
        }
      />
      {m.water_polygons.length > 0 || m.land_polygons.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {m.water_polygons.length > 0 ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                onCommand(metaCommand(doc, { water_polygons: [] }, 'editor:commands.clearWater'))
              }
            >
              {t('editor:properties.clearWater', { count: m.water_polygons.length })}
            </Button>
          ) : null}
          {m.land_polygons.length > 0 ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                onCommand(metaCommand(doc, { land_polygons: [] }, 'editor:commands.clearLand'))
              }
            >
              {t('editor:properties.clearLand', { count: m.land_polygons.length })}
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function BerthFields({
  berths,
  set,
  update,
  settings,
}: {
  berths: Entries<'berth'>;
  set: (patch: Partial<Berth>) => void;
  update: (fn: (el: Berth) => Partial<Berth>) => void;
  settings?: HarborSettings;
}) {
  const { t } = useT();
  const els = berths.map((b) => b.el);
  const c = <K extends keyof Berth>(k: K) => common(els.map((e) => e[k]));
  const single = els.length === 1 ? els[0] : undefined;
  return (
    <section className="flex flex-col gap-3" aria-label={t('editor:properties.berths')}>
      {single ? (
        <TextField
          id="berth-code"
          label={t('editor:fields.code')}
          value={single.code}
          required
          onCommit={(code) => set({ code })}
        />
      ) : null}
      <div className="grid grid-cols-2 gap-2">
        <CmField
          id="berth-width"
          min={MIN_RECT_SIDE_CM}
          label={t('editor:fields.width')}
          value={c('width_cm')}
          onCommit={(v) => v && set({ width_cm: v })}
        />
        <CmField
          id="berth-length"
          min={MIN_RECT_SIDE_CM}
          label={t('editor:fields.length')}
          value={c('length_cm')}
          onCommit={(v) => v && set({ length_cm: v })}
        />
        <CmField
          id="berth-max-loa"
          label={t('editor:fields.maxLoa')}
          value={c('max_loa_cm')}
          onCommit={(v) => set({ max_loa_cm: v })}
        />
        <CmField
          id="berth-max-beam"
          label={t('editor:fields.maxBeam')}
          value={c('max_beam_cm')}
          onCommit={(v) => set({ max_beam_cm: v })}
        />
        <CmField
          id="berth-depth"
          label={t('editor:fields.depth')}
          value={c('depth_cm')}
          onCommit={(v) => set({ depth_cm: v })}
        />
        <CmField
          id="berth-air-draft"
          label={t('editor:fields.maxAirDraft')}
          value={c('max_air_draft_cm')}
          onCommit={(v) => set({ max_air_draft_cm: v })}
        />
      </div>
      <AngleField
        id="berth-rotation"
        label={t('editor:fields.rotation')}
        value={c('rotation_dd')}
        onCommit={(rotation_dd) => set({ rotation_dd })}
      />
      <SelectField
        id="berth-mooring"
        label={t('editor:fields.mooringType')}
        value={c('mooring_type')}
        options={MOORING_TYPES}
        optionLabel={(v) => t(`editor:mooring.${v}`)}
        onCommit={(mooring_type) =>
          set(
            mooring_type === 'buoy'
              ? { mooring_type, swing_radius_cm: single?.swing_radius_cm ?? 1200 }
              : { mooring_type },
          )
        }
      />
      {c('mooring_type') === 'buoy' ? (
        <CmField
          id="berth-swing"
          label={t('editor:fields.swingRadius')}
          value={c('swing_radius_cm')}
          onCommit={(v) => v && set({ swing_radius_cm: v })}
        />
      ) : null}
      <SelectField
        id="berth-side"
        label={t('editor:fields.side')}
        value={c('side')}
        options={SIDES}
        optionLabel={(v) => t(`editor:sides.${v}`)}
        onCommit={(side) => set({ side })}
      />
      <div className="flex items-end gap-2">
        <div className="flex-1">
          <TextField
            id="berth-price-class"
            label={t('editor:fields.priceClass')}
            value={c('price_class')}
            maxLength={16}
            onCommit={(price_class) =>
              set(price_class ? { price_class } : { price_class: undefined })
            }
          />
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={!settings}
          onClick={() =>
            update((el) => ({
              price_class: priceClassFor(el.width_cm, settings?.price_class_rule) || undefined,
            }))
          }
        >
          {t('editor:properties.proposeClass')}
        </Button>
      </div>
      <IntField
        id="berth-position"
        label={t('editor:fields.position')}
        value={c('position')}
        min={-1_000_000_000}
        onCommit={(position) => set({ position })}
      />
      <div className="grid grid-cols-2 gap-2">
        <BoolField
          id="berth-active"
          label={t('editor:fields.active')}
          value={c('active')}
          onCommit={(active) => set({ active })}
        />
        <BoolField
          id="berth-overhang"
          label={t('editor:fields.overhang')}
          value={c('overhang_allowed')}
          onCommit={(overhang_allowed) => set({ overhang_allowed })}
        />
        <BoolField
          id="berth-online"
          label={t('editor:fields.onlineSellable')}
          value={c('online_sellable')}
          onCommit={(online_sellable) => set({ online_sellable })}
        />
        <BoolField
          id="berth-water"
          label={t('editor:fields.water')}
          value={c('water')}
          onCommit={(water) => set({ water })}
        />
        <BoolField
          id="berth-wifi"
          label={t('editor:fields.wifi')}
          value={c('wifi')}
          onCommit={(wifi) => set({ wifi })}
        />
      </div>
      <SelectField
        id="berth-power"
        label={t('editor:fields.powerA')}
        value={(() => {
          const v = c('power_a');
          return v === MIXED ? MIXED : v === undefined ? 'none' : String(v);
        })()}
        options={['none', '16', '32', '63', '125'] as const}
        optionLabel={(v) => (v === 'none' ? t('editor:fields.none') : `${v} A`)}
        onCommit={(v) =>
          set({ power_a: v === 'none' ? undefined : (Number(v) as Berth['power_a']) })
        }
      />
      {single ? (
        <TextField
          id="berth-socket"
          label={t('editor:fields.pedestalSocket')}
          value={single.pedestal_socket}
          maxLength={16}
          onCommit={(v) => set({ pedestal_socket: v || undefined })}
        />
      ) : null}
    </section>
  );
}

function StructureFields({
  items,
  set,
}: {
  items: Entries<'structure'>;
  set: (patch: Partial<Structure>) => void;
}) {
  const { t } = useT();
  const els = items.map((i) => i.el);
  const single = els.length === 1 ? els[0] : undefined;
  return (
    <section className="flex flex-col gap-3" aria-label={t('editor:properties.structures')}>
      {single ? (
        <>
          <p className="text-sm font-medium">{t(`editor:structureKinds.${single.kind}`)}</p>
          <TextField
            id="structure-code"
            label={t('editor:fields.code')}
            value={single.code}
            required
            onCommit={(code) => set({ code })}
          />
          <TextField
            id="structure-name"
            label={t('editor:fields.name')}
            value={single.name}
            maxLength={100}
            onCommit={(name) => set({ name: name || undefined })}
          />
        </>
      ) : null}
      {els.every(
        (e) => e.kind === 'pier' || e.kind === 'walkway' || (e.kind === 'breakwater' && !e.closed),
      ) ? (
        <CmField
          id="structure-width"
          label={t('editor:fields.width')}
          value={common(els.map((e) => e.width_cm))}
          onCommit={(v) => v && set({ width_cm: v })}
        />
      ) : null}
      {single?.kind === 'finger' && single.rect ? (
        <div className="grid grid-cols-2 gap-2">
          <CmField
            id="finger-width"
            label={t('editor:fields.width')}
            value={single.rect.width_cm}
            onCommit={(v) => v && set({ rect: { ...single.rect!, width_cm: v } })}
          />
          <CmField
            id="finger-length"
            label={t('editor:fields.length')}
            value={single.rect.length_cm}
            onCommit={(v) => v && set({ rect: { ...single.rect!, length_cm: v } })}
          />
        </div>
      ) : null}
      <BoolField
        id="structure-floating"
        label={t('editor:fields.floating')}
        value={common(els.map((e) => e.floating ?? false))}
        onCommit={(floating) => set({ floating })}
      />
      {single && (single.kind === 'pier' || single.kind === 'quay') ? (
        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-medium">{t('editor:fields.sides')}</legend>
          {(['left', 'right'] as const).map((s) => (
            <BoolField
              key={s}
              id={`structure-side-${s}`}
              label={t(`editor:sides.${s}`)}
              value={(single.sides ?? []).includes(s)}
              onCommit={(on) => {
                const cur = new Set(single.sides ?? []);
                if (single.kind === 'quay') set({ sides: on ? [s] : [] });
                else {
                  if (on) cur.add(s);
                  else cur.delete(s);
                  set({ sides: [...cur] });
                }
              }}
            />
          ))}
        </fieldset>
      ) : null}
    </section>
  );
}

function AreaFields({
  items,
  set,
}: {
  items: Entries<'area'>;
  set: (patch: Partial<DynamicArea>) => void;
}) {
  const { t } = useT();
  const els = items.map((i) => i.el);
  const c = <K extends keyof DynamicArea>(k: K) => common(els.map((e) => e[k]));
  const single = els.length === 1 ? els[0] : undefined;
  const linear = els.every((e) => e.kind === 'linear');
  const surface = els.every((e) => e.kind === 'surface');
  return (
    <section className="flex flex-col gap-3" aria-label={t('editor:properties.areas')}>
      {single ? (
        <TextField
          id="area-code"
          label={t('editor:fields.code')}
          value={single.code}
          required
          onCommit={(code) => set({ code })}
        />
      ) : null}
      {linear ? (
        <>
          <div className="grid grid-cols-2 gap-2">
            <CmField
              id="area-from"
              label={t('editor:fields.from')}
              value={c('from_cm')}
              min={0}
              onCommit={(v) => v !== undefined && set({ from_cm: v })}
            />
            <CmField
              id="area-to"
              label={t('editor:fields.to')}
              value={c('to_cm')}
              onCommit={(v) => v && set({ to_cm: v })}
            />
          </div>
          <SelectField
            id="area-mode"
            label={t('editor:fields.mode')}
            value={c('mooring_mode')}
            options={MODES}
            optionLabel={(v) => t(`editor:modes.${v}`)}
            onCommit={(mooring_mode) => set({ mooring_mode })}
          />
        </>
      ) : null}
      {surface ? (
        <SelectField
          id="area-grid-rotation"
          label={t('editor:fields.gridRotation')}
          value={(() => {
            const v = c('grid_rotation_dd');
            return v === MIXED
              ? MIXED
              : v === undefined
                ? undefined
                : (String(v) as (typeof GRID_ROTATIONS)[number]);
          })()}
          options={GRID_ROTATIONS}
          optionLabel={(v) => `${Number(v) / 10}°`}
          onCommit={(v) => set({ grid_rotation_dd: Number(v) as DynamicArea['grid_rotation_dd'] })}
        />
      ) : null}
      <div className="grid grid-cols-2 gap-2">
        <CmField
          id="area-cell"
          label={t('editor:fields.cell')}
          value={c('cell_cm')}
          min={10}
          onCommit={(v) => v && set({ cell_cm: v })}
        />
        <CmField
          id="area-margin"
          label={t('editor:fields.margin')}
          value={c('margin_cm')}
          min={0}
          onCommit={(v) => v !== undefined && set({ margin_cm: v })}
        />
        <CmField
          id="area-max-loa"
          label={t('editor:fields.maxLoa')}
          value={c('max_loa_cm')}
          onCommit={(v) => set({ max_loa_cm: v })}
        />
        <CmField
          id="area-max-beam"
          label={t('editor:fields.maxBeam')}
          value={c('max_beam_cm')}
          onCommit={(v) => set({ max_beam_cm: v })}
        />
        {surface ? (
          <CmField
            id="area-max-height"
            label={t('editor:fields.maxHeight')}
            value={c('max_height_cm')}
            onCommit={(v) => set({ max_height_cm: v })}
          />
        ) : null}
      </div>
      <TextField
        id="area-price-class"
        label={t('editor:fields.priceClass')}
        value={c('price_class')}
        maxLength={16}
        onCommit={(v) => set({ price_class: v || undefined })}
      />
      <BoolField
        id="area-active"
        label={t('editor:fields.active')}
        value={c('active')}
        onCommit={(active) => set({ active })}
      />
    </section>
  );
}

function LandFields({
  items,
  set,
}: {
  items: Entries<'land'>;
  set: (patch: Partial<LandResource>) => void;
}) {
  const { t } = useT();
  const els = items.map((i) => i.el);
  const c = <K extends keyof LandResource>(k: K) => common(els.map((e) => e[k]));
  const single = els.length === 1 ? els[0] : undefined;
  return (
    <section className="flex flex-col gap-3" aria-label={t('editor:properties.landResources')}>
      {single ? (
        <TextField
          id="land-code"
          label={t('editor:fields.code')}
          value={single.code}
          required
          onCommit={(code) => set({ code })}
        />
      ) : null}
      <SelectField
        id="land-kind"
        label={t('editor:fields.kind')}
        value={c('kind')}
        options={LAND_KINDS}
        optionLabel={(v) => t(`editor:landKinds.${v}`)}
        onCommit={(kind) => set({ kind })}
      />
      <div className="grid grid-cols-2 gap-2">
        <CmField
          id="land-width"
          min={MIN_RECT_SIDE_CM}
          label={t('editor:fields.width')}
          value={c('width_cm')}
          onCommit={(v) => v && set({ width_cm: v })}
        />
        <CmField
          id="land-length"
          min={MIN_RECT_SIDE_CM}
          label={t('editor:fields.length')}
          value={c('length_cm')}
          onCommit={(v) => v && set({ length_cm: v })}
        />
      </div>
      <AngleField
        id="land-rotation"
        label={t('editor:fields.rotation')}
        value={c('rotation_dd')}
        onCommit={(rotation_dd) => set({ rotation_dd })}
      />
      <TextField
        id="land-price-class"
        label={t('editor:fields.priceClass')}
        value={c('price_class')}
        maxLength={16}
        onCommit={(v) => set({ price_class: v || undefined })}
      />
      <BoolField
        id="land-active"
        label={t('editor:fields.active')}
        value={c('active')}
        onCommit={(active) => set({ active })}
      />
    </section>
  );
}

function PedestalFields({
  items,
  set,
}: {
  items: Entries<'pedestal'>;
  set: (patch: Partial<Pedestal>) => void;
}) {
  const { t } = useT();
  const single = items.length === 1 ? items[0]!.el : undefined;
  if (!single) return null;
  return (
    <section className="flex flex-col gap-3" aria-label={t('editor:properties.pedestals')}>
      <TextField
        id="pedestal-code"
        label={t('editor:fields.code')}
        value={single.code}
        required
        onCommit={(code) => set({ code })}
      />
      <p className="text-sm font-medium">{t('editor:fields.sockets')}</p>
      <ul className="flex flex-col gap-1 text-sm">
        {single.sockets.map((s, i) => (
          <li key={`${s.code}-${i}`} className="flex items-center justify-between gap-2">
            <span>
              {s.code} · {t(`editor:utility.${s.kind}`)}
              {s.amps ? ` · ${s.amps} A` : ''}
            </span>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => set({ sockets: single.sockets.filter((_x, j) => j !== i) })}
            >
              {t('common:actions.remove')}
            </Button>
          </li>
        ))}
      </ul>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={single.sockets.length >= 16}
          onClick={() =>
            set({
              sockets: [
                ...single.sockets,
                {
                  code: String(single.sockets.length + 1),
                  kind: 'power',
                  amps: 16,
                  phase: 'single',
                },
              ],
            })
          }
        >
          {t('editor:properties.addPower')}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={single.sockets.length >= 16}
          onClick={() =>
            set({
              sockets: [
                ...single.sockets,
                { code: `W${single.sockets.length + 1}`, kind: 'water' },
              ],
            })
          }
        >
          {t('editor:properties.addWater')}
        </Button>
      </div>
    </section>
  );
}

function DecorationFields({
  items,
  set,
}: {
  items: Entries<'decoration'>;
  set: (patch: Partial<Decoration>) => void;
}) {
  const { t } = useT();
  const els = items.map((i) => i.el);
  const single = els.length === 1 ? els[0] : undefined;
  return (
    <section className="flex flex-col gap-3" aria-label={t('editor:properties.decorations')}>
      {single?.kind === 'symbol' ? (
        <SelectField
          id="deco-asset"
          label={t('editor:fields.asset')}
          value={single.asset}
          options={KIT_ASSETS.map((a) => a.id)}
          optionLabel={(v) => t(`kit:assets.${v}`)}
          onCommit={(asset) => set({ asset })}
        />
      ) : null}
      {single?.kind === 'text' ? (
        <>
          <TextField
            id="deco-text-it"
            label={t('editor:fields.textIt')}
            value={single.label?.it}
            maxLength={200}
            required
            onCommit={(v) => set({ label: { ...single.label, it: v } })}
          />
          <TextField
            id="deco-text-en"
            label={t('editor:fields.textEn')}
            value={single.label?.en}
            maxLength={200}
            onCommit={(v) => set({ label: { it: single.label?.it ?? v, ...(v ? { en: v } : {}) } })}
          />
        </>
      ) : null}
      {single?.kind === 'depth_label' ? (
        <CmField
          id="deco-depth"
          label={t('editor:fields.depth')}
          value={single.depth_cm}
          min={0}
          onCommit={(v) => v !== undefined && set({ depth_cm: v })}
        />
      ) : null}
      <CmField
        id="deco-size"
        label={t('editor:fields.size')}
        value={common(els.map((e) => e.size_cm))}
        onCommit={(v) => v && set({ size_cm: v })}
      />
      <AngleField
        id="deco-rotation"
        label={t('editor:fields.rotation')}
        value={common(els.map((e) => e.rotation_dd ?? 0))}
        onCommit={(rotation_dd) => set({ rotation_dd })}
      />
    </section>
  );
}

function GroupMembership({
  doc,
  refs,
  groups,
  onCommand,
}: {
  doc: EditorDocument;
  refs: string[];
  groups: readonly BerthGroup[];
  onCommand: (cmd: Command) => void;
}) {
  const { t } = useT();
  const members = refs
    .map((r) => doc.elements.get(r))
    .filter(
      (e): e is Extract<SceneEntry, { kind: 'berth' | 'area' | 'land' }> =>
        e?.kind === 'berth' || e?.kind === 'area' || e?.kind === 'land',
    );
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="text-sm font-medium">{t('editor:properties.groups')}</legend>
      {groups.map((g) => {
        const has = members.map((m) => m.el.group_ids.includes(g.id));
        const value = has.every(Boolean) ? true : has.some(Boolean) ? MIXED : false;
        return (
          <BoolField
            key={g.id}
            id={`group-${g.id}`}
            label={g.code}
            value={value}
            onCommit={(on) =>
              onCommand(
                updateCommand(
                  doc,
                  refs,
                  (e) => {
                    if (e.kind !== 'berth' && e.kind !== 'area' && e.kind !== 'land') return e;
                    const ids = new Set(e.el.group_ids);
                    if (on) ids.add(g.id);
                    else ids.delete(g.id);
                    return { ...e, el: { ...e.el, group_ids: [...ids] } } as SceneEntry;
                  },
                  'editor:commands.groups',
                ),
              )
            }
          />
        );
      })}
    </fieldset>
  );
}
