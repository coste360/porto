import { getRouteApi } from '@tanstack/react-router';
import { PrinterIcon } from 'lucide-react';
import { useMemo, useState } from 'react';

import { mapBackgroundUrl, useMap } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import {
  DEFAULT_LAYERS,
  HarborMap,
  MAP_TOKENS,
  buildScene,
  entriesOf,
  type Viewport,
} from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { Label } from '@porto/shared/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { printPage } from '@porto/shared/lib/download';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { localName } from './lib';
import { printLayout, type Orientation, type Paper, type PrintScale } from './print';

const route = getRouteApi('/auth/m/$marinaId/maps/$mapId/print');

/** Printable page in scale with legend (spec 8.7): A4/A3, portrait/landscape, 1:500 … 1:2000 or fit. */
export function MapPrintPage() {
  const { t, i18n } = useT();
  const { mapId } = route.useParams();
  const { marinaId, can } = useCurrentMarina();
  const query = useMap(marinaId, mapId);
  const [paper, setPaper] = useState<Paper>('A4');
  const [orientation, setOrientation] = useState<Orientation>('landscape');
  const [scale, setScale] = useState<PrintScale>('fit');
  const doc = query.data;
  const scene = useMemo(() => (doc ? buildScene(entriesOf(doc)) : []), [doc]);

  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const map = query.data;
  const layout = printLayout(map.width_cm, map.height_cm, paper, orientation, scale);
  // Pixels per mm at 300 dpi: the level of detail of the print matches the paper, not the screen.
  const pxPerMm = 300 / 25.4;
  const viewport: Viewport = {
    x: 0,
    y: 0,
    w: layout.viewWcm,
    h: layout.viewHcm,
    screenW: layout.mapWmm * pxPerMm,
    screenH: layout.mapHmm * pxPerMm,
  };
  const legend: { key: string; cls: string }[] = [
    { key: 'berths', cls: MAP_TOKENS.berth },
    { key: 'structures', cls: MAP_TOKENS.structure.pier },
    { key: 'areas', cls: MAP_TOKENS.area },
    { key: 'land_resources', cls: MAP_TOKENS.land_resource },
    { key: 'water_land', cls: MAP_TOKENS.water },
  ];
  const classes = [
    ...new Set(map.berths.map((b) => b.price_class).filter((c): c is string => !!c)),
  ].sort();

  return (
    <RequirePermission allowed={can('map.view')}>
      <div className="flex flex-col gap-4">
        <div
          className="flex flex-wrap items-end gap-4 print:hidden"
          role="group"
          aria-label={t('maps:print.options')}
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="print-paper">{t('maps:print.paper')}</Label>
            <Select value={paper} onValueChange={(v) => setPaper(v as Paper)}>
              <SelectTrigger id="print-paper" className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="A4">{'A4'}</SelectItem>
                <SelectItem value="A3">{'A3'}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="print-orientation">{t('maps:print.orientation')}</Label>
            <Select value={orientation} onValueChange={(v) => setOrientation(v as Orientation)}>
              <SelectTrigger id="print-orientation" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="portrait">{t('maps:print.portrait')}</SelectItem>
                <SelectItem value="landscape">{t('maps:print.landscape')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="print-scale">{t('maps:print.scale')}</Label>
            <Select
              value={String(scale)}
              onValueChange={(v) => setScale(v === 'fit' ? 'fit' : (Number(v) as PrintScale))}
            >
              <SelectTrigger id="print-scale" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="fit">{t('maps:print.fit')}</SelectItem>
                <SelectItem value="500">{'1:500'}</SelectItem>
                <SelectItem value="1000">{'1:1000'}</SelectItem>
                <SelectItem value="2000">{'1:2000'}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Button onClick={() => void printPage()}>
            <PrinterIcon />
            {t('maps:print.action')}
          </Button>
          {layout.cropped ? (
            <p className="text-muted-foreground text-sm">{t('maps:print.cropped')}</p>
          ) : null}
        </div>
        <article
          className="bg-background text-foreground flex flex-col gap-3"
          aria-label={t('maps:print.sheet')}
        >
          <header className="flex items-baseline justify-between gap-4">
            <h1 className="text-xl font-semibold">{localName(map.name, i18n.language)}</h1>
            <p className="text-sm" data-testid="print-scale">
              {t('maps:print.scaleLabel', { n: layout.denominator })}
            </p>
          </header>
          <HarborMap
            meta={map}
            scene={scene}
            viewport={viewport}
            layers={{ ...DEFAULT_LAYERS, grid: false }}
            backgroundUrl={
              map.background ? mapBackgroundUrl(marinaId, mapId, map.background.file_id) : undefined
            }
            hideChrome
            width={`${layout.mapWmm}mm`}
            height={`${layout.mapHmm}mm`}
            className="border"
            role="img"
            aria-label={t('maps:print.sheet')}
          />
          <footer className="flex flex-wrap items-start gap-6 text-sm">
            <ul className="flex flex-wrap gap-4" aria-label={t('maps:print.legend')}>
              {legend.map((l) => (
                <li key={l.key} className="flex items-center gap-2">
                  <svg viewBox="0 0 20 12" className="h-3 w-5" aria-hidden="true">
                    <rect x={1} y={1} width={18} height={10} className={l.cls} strokeWidth={1} />
                  </svg>
                  {t(`maps:layers.${l.key}`)}
                </li>
              ))}
            </ul>
            {classes.length > 0 ? (
              <p>
                {t('maps:print.priceClasses')}: {classes.join(', ')}
              </p>
            ) : null}
            <div className="flex items-center gap-2" aria-label={t('maps:print.scaleBar')}>
              <svg
                width={`${layout.bar.mm}mm`}
                height="3mm"
                viewBox={`0 0 ${layout.bar.mm} 3`}
                aria-hidden="true"
              >
                <rect
                  x={0}
                  y={0}
                  width={layout.bar.mm / 2}
                  height={3}
                  className="fill-foreground"
                />
                <rect
                  x={layout.bar.mm / 2}
                  y={0}
                  width={layout.bar.mm / 2}
                  height={3}
                  className="fill-background stroke-foreground"
                  strokeWidth={0.3}
                />
              </svg>
              <span>{formatCm(layout.bar.cm, i18n.language)}</span>
            </div>
          </footer>
        </article>
      </div>
    </RequirePermission>
  );
}
