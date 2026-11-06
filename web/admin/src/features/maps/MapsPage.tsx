import { Link } from '@tanstack/react-router';
import {
  ArrowDownIcon,
  ArrowUpIcon,
  GripVerticalIcon,
  MapIcon,
  MoreHorizontalIcon,
  PlusIcon,
  UploadIcon,
} from 'lucide-react';
import { useRef, useState, type ChangeEvent } from 'react';
import { toast } from 'sonner';

import {
  useDeleteMap,
  useImportMap,
  useMaps,
  useReorderMaps,
  type MapSummary,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@porto/shared/ui/alert-dialog';
import { Badge } from '@porto/shared/ui/badge';
import { Button, buttonVariants } from '@porto/shared/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@porto/shared/ui/card';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@porto/shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { EmptyState, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { toastProblem } from '../../lib/toast';
import { CreateMapDialogBody } from './CreateMapDialog';
import { localName, moveId, parseMapExport } from './lib';

export function MapsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const configure = can('harbor.configure');
  const maps = useMaps(marinaId);
  const importMap = useImportMap(marinaId);
  const [creating, setCreating] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  const onImportFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    const parsed = parseMapExport(await file.text());
    if (!parsed) {
      toast.error(t('maps:import.invalidFile'));
      return;
    }
    try {
      await importMap.mutateAsync(parsed);
      toast.success(t('maps:import.done'));
    } catch (err) {
      toastProblem(t, err);
    }
  };

  return (
    <RequirePermission allowed={can('map.view')}>
      <PageHeader
        title={t('maps:title')}
        description={t('maps:description')}
        actions={
          configure ? (
            <>
              <Button
                variant="outline"
                onClick={() => fileInput.current?.click()}
                disabled={importMap.isPending}
              >
                <UploadIcon />
                {t('maps:import.action')}
              </Button>
              <input
                ref={fileInput}
                type="file"
                accept="application/json,.json"
                className="hidden"
                aria-label={t('maps:import.action')}
                onChange={(e) => void onImportFile(e)}
              />
              <Button onClick={() => setCreating(true)}>
                <PlusIcon />
                {t('maps:create.action')}
              </Button>
            </>
          ) : null
        }
      />
      {maps.isPending ? (
        <LoadingState />
      ) : maps.isError ? (
        <QueryErrorState error={maps.error} onRetry={() => void maps.refetch()} />
      ) : maps.data.length === 0 ? (
        <EmptyState icon={<MapIcon />} title={t('maps:empty')} />
      ) : (
        <MapCards
          maps={[...maps.data].sort((a, b) => a.sort_order - b.sort_order)}
          configure={configure}
        />
      )}
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t('maps:create.title')}</DialogTitle>
          </DialogHeader>
          {creating ? (
            <CreateMapDialogBody marinaId={marinaId} onDone={() => setCreating(false)} />
          ) : null}
        </DialogContent>
      </Dialog>
    </RequirePermission>
  );
}

function MapCards({ maps, configure }: { maps: MapSummary[]; configure: boolean }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const reorder = useReorderMaps(marinaId);
  const remove = useDeleteMap(marinaId);
  const [dragging, setDragging] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<MapSummary | null>(null);
  const ids = maps.map((m) => m.id);

  const moveTo = (id: string, index: number) => {
    const next = moveId(ids, id, index);
    if (next.join() !== ids.join())
      reorder.mutate(next, { onError: (err) => toastProblem(t, err) });
  };

  return (
    <>
      <ul className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3" aria-label={t('maps:listLabel')}>
        {maps.map((m, index) => (
          <li
            key={m.id}
            draggable={configure}
            onDragStart={() => setDragging(m.id)}
            onDragEnd={() => setDragging(null)}
            onDragOver={(e) => {
              if (dragging) e.preventDefault();
            }}
            onDrop={(e) => {
              e.preventDefault();
              if (dragging && dragging !== m.id) moveTo(dragging, index);
              setDragging(null);
            }}
            className={dragging === m.id ? 'opacity-50' : undefined}
          >
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  {configure ? (
                    <GripVerticalIcon
                      className="text-muted-foreground size-4 cursor-grab"
                      aria-hidden="true"
                    />
                  ) : null}
                  <Link
                    to="/m/$marinaId/maps/$mapId"
                    params={{ marinaId, mapId: m.id }}
                    className="hover:underline"
                  >
                    {localName(m.name, i18n.language)}
                  </Link>
                </CardTitle>
                <CardDescription>
                  {t(`maps:kind.${m.kind}`)} · {formatCm(m.width_cm, i18n.language)} ×{' '}
                  {formatCm(m.height_cm, i18n.language)}
                </CardDescription>
                <CardAction>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon" aria-label={t('common:actions.actions')}>
                        <MoreHorizontalIcon />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem asChild>
                        <Link to="/m/$marinaId/maps/$mapId" params={{ marinaId, mapId: m.id }}>
                          {t('maps:actions.open')}
                        </Link>
                      </DropdownMenuItem>
                      <DropdownMenuItem asChild>
                        <Link
                          to="/m/$marinaId/maps/$mapId/print"
                          params={{ marinaId, mapId: m.id }}
                        >
                          {t('maps:actions.print')}
                        </Link>
                      </DropdownMenuItem>
                      {configure ? (
                        <>
                          <DropdownMenuItem asChild>
                            <Link
                              to="/m/$marinaId/maps/$mapId/edit"
                              params={{ marinaId, mapId: m.id }}
                            >
                              {t('maps:actions.edit')}
                            </Link>
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            disabled={index === 0}
                            onSelect={() => moveTo(m.id, index - 1)}
                          >
                            <ArrowUpIcon />
                            {t('maps:actions.moveUp')}
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            disabled={index === maps.length - 1}
                            onSelect={() => moveTo(m.id, index + 1)}
                          >
                            <ArrowDownIcon />
                            {t('maps:actions.moveDown')}
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" onSelect={() => setDeleting(m)}>
                            {t('common:actions.delete')}
                          </DropdownMenuItem>
                        </>
                      ) : null}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </CardAction>
              </CardHeader>
              <CardContent className="flex flex-wrap gap-2 text-sm">
                <Badge variant="secondary">
                  {t('maps:counts.berths', { count: m.counts.berths })}
                </Badge>
                <Badge variant="secondary">
                  {t('maps:counts.structures', { count: m.counts.structures })}
                </Badge>
                {m.counts.dynamic_areas > 0 ? (
                  <Badge variant="secondary">
                    {t('maps:counts.areas', { count: m.counts.dynamic_areas })}
                  </Badge>
                ) : null}
                {m.counts.land_resources > 0 ? (
                  <Badge variant="secondary">
                    {t('maps:counts.landResources', { count: m.counts.land_resources })}
                  </Badge>
                ) : null}
                {m.georeferenced ? (
                  <Badge variant="outline">{t('maps:georeferenced')}</Badge>
                ) : null}
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>
      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => (open ? null : setDeleting(null))}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('maps:delete.title')}</AlertDialogTitle>
            <AlertDialogDescription>{t('maps:delete.description')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common:actions.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className={buttonVariants({ variant: 'destructive' })}
              onClick={() => {
                if (!deleting) return;
                remove.mutate(
                  { mapId: deleting.id, version: deleting.version },
                  {
                    onSuccess: () => toast.success(t('maps:delete.done')),
                    onError: (err) => toastProblem(t, err),
                  },
                );
              }}
            >
              {t('common:actions.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
