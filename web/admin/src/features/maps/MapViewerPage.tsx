import { getRouteApi, Link } from '@tanstack/react-router';
import {
  DownloadIcon,
  MaximizeIcon,
  PencilIcon,
  PrinterIcon,
  ZoomInIcon,
  ZoomOutIcon,
} from 'lucide-react';
import { useMemo, useState } from 'react';

import { exportMap, mapBackgroundUrl, useApi, useMap } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  DEFAULT_LAYERS,
  HarborMap,
  buildScene,
  entriesOf,
  useMapViewport,
  type LayerVisibility,
} from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { toastProblem } from '../../lib/toast';
import { LayerToggles } from './LayerToggles';
import { downloadJson, fileSlug, localName, mapAspectClass } from './lib';
import { useElementLabel } from './useElementLabel';
import { StructureList } from './StructureList';

const route = getRouteApi('/auth/m/$marinaId/maps/$mapId');

/** Read-only map viewer for every role with map.view (m1-plan §11 D29). */
export function MapViewerPage() {
  const { t, i18n } = useT();
  const api = useApi();
  const { mapId } = route.useParams();
  const { marinaId, can } = useCurrentMarina();
  const query = useMap(marinaId, mapId);
  const doc = query.data;
  const scene = useMemo(() => (doc ? buildScene(entriesOf(doc)) : []), [doc]);
  const content = useMemo(
    () => ({ minX: 0, minY: 0, maxX: doc?.width_cm ?? 10_000, maxY: doc?.height_cm ?? 10_000 }),
    [doc?.width_cm, doc?.height_cm],
  );
  const { viewport, bindContainer, bindSvg, gestures, fit, zoomBy } = useMapViewport(content);
  const [layers, setLayers] = useState<LayerVisibility>(DEFAULT_LAYERS);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const labelFor = useElementLabel();
  const configure = can('harbor.configure');

  const doExport = async (format: 'json' | 'geojson') => {
    try {
      const data = await exportMap(api, marinaId, mapId, format);
      const name = fileSlug(localName(doc?.name, i18n.language));
      downloadJson(
        data,
        `${name}.${format === 'json' ? 'json' : 'geojson'}`,
        format === 'json' ? 'application/json' : 'application/geo+json',
      );
    } catch (err) {
      toastProblem(t, err);
    }
  };

  return (
    <RequirePermission allowed={can('map.view')}>
      {query.isPending ? (
        <LoadingState />
      ) : query.isError ? (
        <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <>
          <PageHeader
            title={localName(query.data.name, i18n.language)}
            description={t(`maps:kind.${query.data.kind}`)}
            actions={
              <>
                <Button variant="outline" asChild>
                  <Link to="/m/$marinaId/maps/$mapId/print" params={{ marinaId, mapId }}>
                    <PrinterIcon />
                    {t('maps:actions.print')}
                  </Link>
                </Button>
                {configure ? (
                  <>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="outline">
                          <DownloadIcon />
                          {t('maps:export.action')}
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onSelect={() => void doExport('json')}>
                          {t('maps:export.json')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          disabled={!query.data.georef}
                          onSelect={() => void doExport('geojson')}
                        >
                          {t('maps:export.geojson')}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                    <Button variant="outline" asChild>
                      <Link
                        to="/m/$marinaId/maps/$mapId/import-berths"
                        params={{ marinaId, mapId }}
                      >
                        {t('maps:actions.importBerths')}
                      </Link>
                    </Button>
                    <Button asChild>
                      <Link to="/m/$marinaId/maps/$mapId/edit" params={{ marinaId, mapId }}>
                        <PencilIcon />
                        {t('maps:actions.edit')}
                      </Link>
                    </Button>
                  </>
                ) : null}
              </>
            }
          />
          <div className="grid gap-4 lg:grid-cols-4">
            <div className="flex flex-col gap-2 lg:col-span-3">
              <div
                className="flex flex-wrap items-center gap-2"
                role="toolbar"
                aria-label={t('maps:viewer.toolbar')}
              >
                <Button variant="outline" size="sm" onClick={fit}>
                  <MaximizeIcon />
                  {t('maps:viewer.fit')}
                </Button>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => zoomBy(1.25)}
                  aria-label={t('maps:viewer.zoomIn')}
                >
                  <ZoomInIcon />
                </Button>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => zoomBy(0.8)}
                  aria-label={t('maps:viewer.zoomOut')}
                >
                  <ZoomOutIcon />
                </Button>
                <div className="ml-auto">
                  <LayerToggles layers={layers} onChange={setLayers} />
                </div>
              </div>
              <div
                ref={bindContainer}
                data-testid="map-canvas"
                className={`bg-muted w-full overflow-hidden rounded-md border ${mapAspectClass(query.data.width_cm, query.data.height_cm)} max-h-svh lg:aspect-auto lg:h-160`}
              >
                <HarborMap
                  svgRef={bindSvg}
                  meta={query.data}
                  scene={scene}
                  viewport={viewport}
                  layers={layers}
                  selected={selected}
                  interactive
                  labelFor={labelFor}
                  backgroundUrl={
                    query.data.background
                      ? mapBackgroundUrl(marinaId, mapId, query.data.background.file_id)
                      : undefined
                  }
                  aria-label={t('maps:viewer.canvas')}
                  role="group"
                  onElementPointerDown={(ref) => setSelected(new Set([ref]))}
                  onElementActivate={(ref) => setSelected(new Set([ref]))}
                  onBackgroundPointerDown={(e) => {
                    gestures.onPointerDown(e, true);
                  }}
                  onPointerMove={(e) => gestures.onPointerMove(e)}
                  onPointerUp={(e) => gestures.onPointerUp(e)}
                  onPointerCancel={(e) => gestures.onPointerUp(e)}
                />
              </div>
            </div>
            <Card>
              <CardHeader>
                <CardTitle>{t('maps:list.title')}</CardTitle>
              </CardHeader>
              <CardContent>
                <StructureList
                  items={scene}
                  selected={selected}
                  onPick={(ref) => setSelected(new Set([ref]))}
                />
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </RequirePermission>
  );
}
