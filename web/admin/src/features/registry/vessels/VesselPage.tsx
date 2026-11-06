import { useQueryClient } from '@tanstack/react-query';
import { getRouteApi } from '@tanstack/react-router';
import { ArchiveIcon, ArchiveRestoreIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  isProblem,
  queryKeys,
  useArchiveVessel,
  useReplaceVesselParties,
  useUpdateVessel,
  useVessel,
  type Vessel,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm, formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';
import { DocumentsPanel } from '../documents/DocumentsPanel';
import { useMarinaToday } from '../lib/today';
import { MeasurementDialog } from './MeasurementDialog';
import { checkParties, partiesToRows, type PartyRow } from './parties';
import { PartiesEditor } from './PartiesEditor';
import { VesselForm } from './VesselForm';
import { valuesToVesselPatch, vesselToValues } from './vesselSchema';

const routeApi = getRouteApi('/auth/m/$marinaId/vessels/$vesselId');

export function VesselPage() {
  const { can, marinaId } = useCurrentMarina();
  const { vesselId } = routeApi.useParams();
  const vessel = useVessel(marinaId, vesselId);
  return (
    <RequirePermission allowed={can('customers.read')}>
      {vessel.isPending ? (
        <LoadingState rows={6} />
      ) : vessel.isError ? (
        <QueryErrorState error={vessel.error} onRetry={() => void vessel.refetch()} />
      ) : (
        <VesselView vessel={vessel.data} />
      )}
    </RequirePermission>
  );
}

function VesselView({ vessel }: { vessel: Vessel }) {
  const { t, i18n } = useT();
  const { can, marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const queryClient = useQueryClient();
  const canWrite = can('customers.write');
  const canMeasure = canWrite || can('presence.write');
  const update = useUpdateVessel(marinaId);
  const archive = useArchiveVessel(marinaId);
  const len = (cm: number | null) => (cm === null ? '–' : formatCm(cm, i18n.language));

  return (
    <>
      <PageHeader
        title={vessel.name}
        description={[vessel.registration, t(`vessels:hullTypes.${vessel.hull_type}`)]
          .filter(Boolean)
          .join(' · ')}
        actions={
          <>
            {canMeasure ? <MeasurementDialog key={vessel.version} vessel={vessel} /> : null}
            {canWrite ? (
              <Button
                variant="outline"
                disabled={archive.isPending}
                onClick={() =>
                  archive.mutate(
                    { vesselId: vessel.id, archive: vessel.archived_at === null },
                    {
                      onSuccess: () =>
                        toast.success(
                          vessel.archived_at
                            ? t('vessels:actions.unarchived')
                            : t('vessels:actions.archived'),
                        ),
                      onError: (e) => toastProblem(t, e),
                    },
                  )
                }
              >
                {vessel.archived_at ? <ArchiveRestoreIcon /> : <ArchiveIcon />}
                {vessel.archived_at ? t('vessels:actions.unarchive') : t('vessels:actions.archive')}
              </Button>
            ) : null}
          </>
        }
      />

      <div className="mb-4 flex flex-wrap gap-2">
        <Badge variant="outline">{t(`vessels:unitClasses.${vessel.unit_class}`)}</Badge>
        <Badge variant={vessel.dimensions_source === 'measured' ? 'secondary' : 'outline'}>
          {t(`vessels:dimensionsSource.${vessel.dimensions_source}`)}
        </Badge>
        {vessel.archived_at ? (
          <Badge variant="secondary">{t('vessels:status.archived')}</Badge>
        ) : null}
      </div>

      <Card className="mb-4">
        <CardHeader>
          <CardTitle>{t('vessels:dimensions.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
            <dt className="text-muted-foreground">{t('vessels:dimensions.effective')}</dt>
            <dd className="tabular-nums">
              {`${len(vessel.effective_loa_cm)} × ${len(vessel.effective_beam_cm)} · ${len(
                vessel.effective_draft_cm,
              )}`}
            </dd>
            <dt className="text-muted-foreground">{t('vessels:dimensions.declared')}</dt>
            <dd className="tabular-nums">
              {`${len(vessel.loa_cm)} × ${len(vessel.beam_cm)} · ${len(vessel.draft_cm)}`}
            </dd>
            <dt className="text-muted-foreground">{t('vessels:dimensions.measured')}</dt>
            <dd className="tabular-nums">
              {vessel.measured_at
                ? `${len(vessel.measured_loa_cm)} × ${len(vessel.measured_beam_cm)} · ${len(
                    vessel.measured_draft_cm,
                  )}`
                : t('vessels:dimensions.notMeasured')}
            </dd>
            <dt className="text-muted-foreground">{t('vessels:dimensions.measuredAt')}</dt>
            <dd>
              {vessel.measured_at
                ? `${formatDateTime(vessel.measured_at, i18n.language)}${
                    vessel.measured_by ? ` · ${vessel.measured_by.name}` : ''
                  }`
                : '–'}
            </dd>
          </dl>
        </CardContent>
      </Card>

      <Tabs defaultValue="data">
        <TabsList>
          <TabsTrigger value="data">{t('vessels:tabs.data')}</TabsTrigger>
          <TabsTrigger value="parties">
            {t('vessels:tabs.parties')} ({vessel.parties.length})
          </TabsTrigger>
          <TabsTrigger value="documents">
            {t('vessels:tabs.documents')} ({vessel.documents.length})
          </TabsTrigger>
        </TabsList>
        <TabsContent value="data">
          <Card>
            <CardContent>
              {update.isError && !isProblem(update.error, 'validation_failed') ? (
                <div className="mb-4 flex flex-col gap-2">
                  <ProblemAlert error={update.error} />
                  {isProblem(update.error, 'version_conflict') ? (
                    <div>
                      <Button
                        variant="outline"
                        onClick={() => {
                          update.reset();
                          void queryClient.invalidateQueries({
                            queryKey: queryKeys.registry(marinaId),
                          });
                        }}
                      >
                        {t('common:actions.reload')}
                      </Button>
                    </div>
                  ) : null}
                </div>
              ) : null}
              <VesselForm
                key={vessel.version}
                edit
                hasMeasurement={vessel.measured_at !== null}
                defaultValues={vesselToValues(vessel)}
                submitLabel={t('common:actions.save')}
                pending={update.isPending}
                disabled={!canWrite}
                error={update.error}
                onSubmit={(values) =>
                  update.mutate(
                    {
                      vesselId: vessel.id,
                      version: vessel.version,
                      patch: valuesToVesselPatch(values),
                    },
                    { onSuccess: () => toast.success(t('vessels:form.saved')) },
                  )
                }
              />
            </CardContent>
          </Card>
        </TabsContent>
        <TabsContent value="parties">
          <PartiesCard key={vessel.version} vessel={vessel} canWrite={canWrite} />
        </TabsContent>
        <TabsContent value="documents">
          <DocumentsPanel
            owner={{ type: 'vessel', id: vessel.id }}
            documents={vessel.documents}
            canWrite={canWrite}
            today={today}
          />
        </TabsContent>
      </Tabs>
    </>
  );
}

function PartiesCard({ vessel, canWrite }: { vessel: Vessel; canWrite: boolean }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const replace = useReplaceVesselParties(marinaId);
  const [rows, setRows] = useState<PartyRow[]>(() => partiesToRows(vessel.parties, i18n.language));
  const [showErrors, setShowErrors] = useState(false);
  const check = checkParties(rows);
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('vessels:parties.title')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-muted-foreground text-sm">{t('vessels:parties.description')}</p>
        {replace.isError ? <ProblemAlert error={replace.error} /> : null}
        <PartiesEditor
          rows={rows}
          onChange={setRows}
          errors={showErrors && !check.ok ? check.errors : {}}
          ownerTotal={check.ownerTotal}
          disabled={!canWrite || replace.isPending}
        />
        {canWrite ? (
          <div className="flex justify-end">
            <Button
              disabled={replace.isPending}
              onClick={() => {
                if (!check.ok) {
                  setShowErrors(true);
                  return;
                }
                replace.mutate(
                  { vesselId: vessel.id, version: vessel.version, parties: check.parties },
                  { onSuccess: () => toast.success(t('vessels:parties.saved')) },
                );
              }}
            >
              {t('vessels:parties.save')}
            </Button>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
