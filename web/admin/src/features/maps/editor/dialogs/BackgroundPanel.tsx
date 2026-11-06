import { CompassIcon, CrosshairIcon, UploadIcon } from 'lucide-react';
import { useRef, useState, type ChangeEvent } from 'react';
import { toast } from 'sonner';

import {
  BACKGROUND_TYPES,
  MAX_BACKGROUND_BYTES,
  useConvertConcessionGeojson,
  useUploadMapBackground,
  type ControlPoint,
  type Georef,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm, formatE7, parseDecimalToE7 } from '@porto/shared/lib/format';
import type { Pt } from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { LengthInput, ProblemAlert } from '@porto/shared/ui/patterns';
import { Progress } from '@porto/shared/ui/progress';
import { Separator } from '@porto/shared/ui/separator';
import { Slider } from '@porto/shared/ui/slider';
import { Switch } from '@porto/shared/ui/switch';

import { toastProblem } from '../../../../lib/toast';
import {
  INITIAL_SCALE_MCM_PER_PX,
  calibrate,
  formatDd,
  parseDegreesToDd,
} from '../model/calibration';
import { metaCommand, type Command } from '../model/commands';
import type { EditorDocument } from '../model/document';

export type BackgroundPick = { kind: 'calibrate' } | { kind: 'georef'; index: number };

export type BackgroundPanelProps = {
  marinaId: string;
  doc: EditorDocument;
  onCommand: (cmd: Command) => void;
  /** Two points picked with the Calibrate tool (map cm), waiting for the real distance. */
  calibrationPoints: [Pt, Pt] | null;
  onClearCalibration: () => void;
  picking: BackgroundPick | null;
  onPick: (pick: BackgroundPick | null) => void;
};

/**
 * Background image, calibration and georeference (spec 8.2, m1-plan §5.4), plus the concession perimeter
 * import. Every change is a command of the document, saved with the map.
 */
export function BackgroundPanel(p: BackgroundPanelProps) {
  const { t, i18n } = useT();
  const upload = useUploadMapBackground(p.marinaId, p.doc.mapId);
  const convert = useConvertConcessionGeojson(p.marinaId, p.doc.mapId);
  const file = useRef<HTMLInputElement>(null);
  const geojson = useRef<HTMLInputElement>(null);
  const [distance, setDistance] = useState<number | null>(null);
  const bg = p.doc.meta.background;
  const m = t('maps:units.m');

  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    e.target.value = '';
    if (!f) return;
    if (!(BACKGROUND_TYPES as readonly string[]).includes(f.type)) {
      toast.error(t('errors:unsupported_media_type'));
      return;
    }
    if (f.size > MAX_BACKGROUND_BYTES) {
      toast.error(t('errors:file_too_large'));
      return;
    }
    try {
      const r = await upload.mutateAsync(f);
      p.onCommand(
        metaCommand(
          p.doc,
          {
            background: {
              file_id: r.file_id,
              content_type: r.content_type,
              width_px: r.width_px,
              height_px: r.height_px,
              origin: { x: 0, y: 0 },
              scale_mcm_per_px: INITIAL_SCALE_MCM_PER_PX,
              rotation_dd: 0,
              opacity_pct: 60,
              visible: true,
            },
          },
          'editor:commands.background',
        ),
      );
    } catch (err) {
      toastProblem(t, err);
    }
  };

  const setBg = (patch: Partial<NonNullable<typeof bg>>, label = 'editor:commands.background') => {
    if (bg) p.onCommand(metaCommand(p.doc, { background: { ...bg, ...patch } }, label));
  };

  const onGeojson = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    e.target.value = '';
    if (!f) return;
    let parsed: Record<string, unknown>;
    try {
      parsed = JSON.parse(await f.text()) as Record<string, unknown>;
    } catch {
      toast.error(t('editor:background.invalidGeojson'));
      return;
    }
    try {
      const outline = await convert.mutateAsync(
        p.doc.meta.georef ? { geojson: parsed, georef: p.doc.meta.georef } : { geojson: parsed },
      );
      p.onCommand(
        metaCommand(p.doc, { concession_outline: outline }, 'editor:commands.setConcession'),
      );
      toast.success(t('editor:background.concessionImported'));
    } catch (err) {
      toastProblem(t, err);
    }
  };

  return (
    <section className="flex flex-col gap-4" aria-label={t('editor:background.title')}>
      <h2 className="text-base font-semibold">{t('editor:background.title')}</h2>
      <div className="flex flex-col gap-2">
        <Button variant="outline" onClick={() => file.current?.click()} disabled={upload.isPending}>
          <UploadIcon />
          {bg ? t('editor:background.replace') : t('editor:background.upload')}
        </Button>
        <input
          ref={file}
          type="file"
          accept={BACKGROUND_TYPES.join(',')}
          className="hidden"
          aria-label={t('editor:background.upload')}
          data-testid="background-file"
          onChange={(e) => void onFile(e)}
        />
        {upload.isPending ? (
          <Progress
            aria-label={t('editor:background.uploading')}
            className="animate-pulse"
            value={100}
          />
        ) : null}
        {upload.error ? <ProblemAlert error={upload.error} /> : null}
      </div>
      {bg ? (
        <>
          <div className="flex items-center justify-between gap-2">
            <Label htmlFor="bg-visible">{t('editor:background.visible')}</Label>
            <Switch
              id="bg-visible"
              checked={bg.visible}
              onCheckedChange={(visible) => setBg({ visible })}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="bg-opacity">
              {t('editor:background.opacity', { value: bg.opacity_pct })}
            </Label>
            <Slider
              id="bg-opacity"
              min={0}
              max={100}
              step={5}
              value={[bg.opacity_pct]}
              aria-label={t('editor:background.opacityLabel')}
              onValueCommit={([v]) => setBg({ opacity_pct: v ?? bg.opacity_pct })}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="bg-rotation">{t('editor:background.rotation')}</Label>
            <Slider
              min={0}
              max={3599}
              step={1}
              value={[bg.rotation_dd]}
              aria-label={t('editor:background.rotation')}
              onValueCommit={([v]) => setBg({ rotation_dd: v ?? bg.rotation_dd })}
            />
            <div className="flex gap-2">
              <Input
                key={bg.rotation_dd}
                id="bg-rotation"
                inputMode="decimal"
                defaultValue={formatDd(bg.rotation_dd)}
                onBlur={(e) => {
                  const dd = parseDegreesToDd(e.target.value);
                  if (dd !== null && dd !== bg.rotation_dd) setBg({ rotation_dd: dd });
                }}
              />
              <Button variant="outline" onClick={() => setBg({ rotation_dd: 0 })}>
                <CompassIcon />
                {t('editor:background.northUp')}
              </Button>
            </div>
          </div>
          <p className="text-muted-foreground text-sm">
            {t('editor:background.scale', {
              value: (bg.scale_mcm_per_px / 1000).toLocaleString(i18n.language, {
                maximumFractionDigits: 3,
              }),
            })}
          </p>
          <div className="flex flex-col gap-2 rounded-md border p-3">
            <p className="text-sm font-medium">{t('editor:background.calibrate')}</p>
            <p className="text-muted-foreground text-sm">{t('editor:background.calibrateHint')}</p>
            {p.calibrationPoints ? (
              <>
                <Label htmlFor="bg-distance">{t('editor:background.distance')}</Label>
                <LengthInput
                  id="bg-distance"
                  value={distance}
                  onChange={setDistance}
                  unit={m}
                  locale={i18n.language}
                />
                <div className="flex gap-2">
                  <Button
                    disabled={!distance}
                    onClick={() => {
                      const [a, b] = p.calibrationPoints!;
                      const next = distance ? calibrate(bg, a, b, distance) : null;
                      if (next)
                        p.onCommand(
                          metaCommand(p.doc, { background: next }, 'editor:commands.calibrate'),
                        );
                      p.onClearCalibration();
                      setDistance(null);
                    }}
                  >
                    {t('editor:background.applyCalibration')}
                  </Button>
                  <Button variant="ghost" onClick={p.onClearCalibration}>
                    {t('common:actions.cancel')}
                  </Button>
                </div>
              </>
            ) : (
              <Button
                variant={p.picking?.kind === 'calibrate' ? 'secondary' : 'outline'}
                onClick={() =>
                  p.onPick(p.picking?.kind === 'calibrate' ? null : { kind: 'calibrate' })
                }
              >
                <CrosshairIcon />
                {t('editor:background.pickTwo')}
              </Button>
            )}
          </div>
          <Button
            variant="ghost"
            onClick={() =>
              p.onCommand(metaCommand(p.doc, { background: null }, 'editor:commands.background'))
            }
          >
            {t('editor:background.remove')}
          </Button>
        </>
      ) : null}
      <Separator />
      <GeorefEditor doc={p.doc} onCommand={p.onCommand} picking={p.picking} onPick={p.onPick} />
      <Separator />
      <div className="flex flex-col gap-2">
        <p className="text-sm font-medium">{t('editor:background.concession')}</p>
        <p className="text-muted-foreground text-sm">{t('editor:background.concessionHint')}</p>
        <Button
          variant="outline"
          onClick={() => geojson.current?.click()}
          disabled={convert.isPending}
        >
          <UploadIcon />
          {t('editor:background.importGeojson')}
        </Button>
        <input
          ref={geojson}
          type="file"
          accept="application/geo+json,application/json,.geojson,.json"
          className="hidden"
          aria-label={t('editor:background.importGeojson')}
          onChange={(e) => void onGeojson(e)}
        />
        {p.doc.meta.concession_outline ? (
          <Button
            variant="ghost"
            onClick={() =>
              p.onCommand(
                metaCommand(p.doc, { concession_outline: null }, 'editor:commands.setConcession'),
              )
            }
          >
            {t('editor:background.removeConcession')}
          </Button>
        ) : null}
      </div>
    </section>
  );
}

const EMPTY_POINT: ControlPoint = { x: 0, y: 0, lat_e7: 0, lon_e7: 0 };

/** Three control points: map position (picked) and WGS84 typed as decimal degrees, stored as integers ×1e7. */
function GeorefEditor({
  doc,
  onCommand,
  picking,
  onPick,
}: {
  doc: EditorDocument;
  onCommand: (cmd: Command) => void;
  picking: BackgroundPick | null;
  onPick: (pick: BackgroundPick | null) => void;
}) {
  const { t, i18n } = useT();
  const current = doc.meta.georef?.points ?? [EMPTY_POINT, EMPTY_POINT, EMPTY_POINT];
  const setPoint = (i: number, patch: Partial<ControlPoint>) => {
    const points = current.map((cp, j) => (j === i ? { ...cp, ...patch } : cp));
    onCommand(metaCommand(doc, { georef: { points } as Georef }, 'editor:commands.georef'));
  };
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm font-medium">{t('editor:background.georef')}</p>
      <p className="text-muted-foreground text-sm">{t('editor:background.georefHint')}</p>
      {current.map((cp, i) => (
        <fieldset key={i} className="flex flex-col gap-2 rounded-md border p-2">
          <legend className="px-1 text-sm">
            {t('editor:background.controlPoint', { n: i + 1 })}
          </legend>
          <div className="flex items-center gap-2 text-sm">
            <span>
              {formatCm(cp.x, i18n.language)}, {formatCm(cp.y, i18n.language)}
            </span>
            <Button
              size="sm"
              variant={picking?.kind === 'georef' && picking.index === i ? 'secondary' : 'outline'}
              onClick={() =>
                onPick(
                  picking?.kind === 'georef' && picking.index === i
                    ? null
                    : { kind: 'georef', index: i },
                )
              }
            >
              <CrosshairIcon />
              {t('editor:generators.pick')}
            </Button>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div className="flex flex-col gap-1">
              <Label htmlFor={`cp-lat-${i}`}>{t('editor:background.lat')}</Label>
              <Input
                key={`lat-${cp.lat_e7}`}
                id={`cp-lat-${i}`}
                inputMode="decimal"
                defaultValue={cp.lat_e7 ? formatE7(cp.lat_e7) : ''}
                onBlur={(e) => {
                  const v = parseDecimalToE7(e.target.value);
                  if (v !== null && v !== cp.lat_e7 && Math.abs(v) <= 900_000_000)
                    setPoint(i, { lat_e7: v });
                }}
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor={`cp-lon-${i}`}>{t('editor:background.lon')}</Label>
              <Input
                key={`lon-${cp.lon_e7}`}
                id={`cp-lon-${i}`}
                inputMode="decimal"
                defaultValue={cp.lon_e7 ? formatE7(cp.lon_e7) : ''}
                onBlur={(e) => {
                  const v = parseDecimalToE7(e.target.value);
                  if (v !== null && v !== cp.lon_e7 && Math.abs(v) <= 1_800_000_000)
                    setPoint(i, { lon_e7: v });
                }}
              />
            </div>
          </div>
        </fieldset>
      ))}
      {doc.meta.georef ? (
        <Button
          variant="ghost"
          onClick={() => onCommand(metaCommand(doc, { georef: null }, 'editor:commands.georef'))}
        >
          {t('editor:background.removeGeoref')}
        </Button>
      ) : null}
    </div>
  );
}
