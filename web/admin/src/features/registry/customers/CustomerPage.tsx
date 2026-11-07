import { useQueryClient } from '@tanstack/react-query';
import { getRouteApi, Link } from '@tanstack/react-router';
import type { ColumnDef } from '@tanstack/react-table';
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  DownloadIcon,
  GitMergeIcon,
  PlusIcon,
  UserXIcon,
} from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  isProblem,
  queryKeys,
  useArchiveCustomer,
  useCustomerOverview,
  useExportCustomer,
  useUpdateCustomer,
  type CustomerOverview,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent } from '@porto/shared/ui/card';
import { DataTable, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';
import { CustomerDashboardTab, CustomerStatementTab } from '../../billing/public';
import { DocumentsPanel } from '../documents/DocumentsPanel';
import { DeadlineTable } from '../deadlines/DeadlineTable';
import { customerName, downloadBlob, formatShare } from '../lib/format';
import { useMarinaToday } from '../lib/today';
import { AnonymizeDialog } from './AnonymizeDialog';
import { ConsentsPanel } from './ConsentsPanel';
import { CustomerForm } from './CustomerForm';
import { customerToValues, valuesToPatch } from './customerSchema';
import { MergeDialog } from './MergeDialog';

const routeApi = getRouteApi('/auth/m/$marinaId/customers/$customerId');

export function CustomerPage() {
  const { can, marinaId } = useCurrentMarina();
  const { customerId } = routeApi.useParams();
  const overview = useCustomerOverview(marinaId, customerId);
  return (
    <RequirePermission allowed={can('customers.read')}>
      {overview.isPending ? (
        <LoadingState rows={6} />
      ) : overview.isError ? (
        <QueryErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      ) : (
        <CustomerView data={overview.data} />
      )}
    </RequirePermission>
  );
}

function CustomerView({ data }: { data: CustomerOverview }) {
  const { t, i18n } = useT();
  const { can, marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const customer = data.customer;
  const locked = customer.anonymized_at !== null || customer.merged_into_id !== null;
  const canWrite = can('customers.write') && !locked;
  const [dialog, setDialog] = useState<'none' | 'merge' | 'anonymize'>('none');
  const queryClient = useQueryClient();
  const update = useUpdateCustomer(marinaId);
  const archive = useArchiveCustomer(marinaId);
  const exportCustomer = useExportCustomer(marinaId);
  const onError = (error: unknown) => toastProblem(t, error);

  const doExport = () =>
    exportCustomer.mutate(customer.id, {
      onSuccess: (payload) => {
        downloadBlob(
          `customer-${customer.id}.json`,
          new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' }),
        );
        toast.success(t('customers:actions.exported'));
      },
      onError,
    });

  const vesselColumns: ColumnDef<CustomerOverview['vessels'][number], unknown>[] = [
    {
      header: t('vessels:columns.name'),
      cell: ({ row }) => (
        <Link
          to="/m/$marinaId/vessels/$vesselId"
          params={{ marinaId, vesselId: row.original.vessel.id }}
          className="font-medium underline-offset-4 hover:underline"
        >
          {row.original.vessel.name}
        </Link>
      ),
    },
    {
      header: t('vessels:columns.registration'),
      cell: ({ row }) => row.original.vessel.registration ?? '',
    },
    {
      header: t('vessels:columns.dimensions'),
      cell: ({ row }) =>
        `${formatCm(row.original.vessel.effective_loa_cm, i18n.language)} × ${formatCm(
          row.original.vessel.effective_beam_cm,
          i18n.language,
        )}`,
    },
    {
      header: t('customers:vessels.roles'),
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          {row.original.roles.map((r: Schemas['VesselPartyRole']) => (
            <Badge key={r} variant="outline">
              {t(`vessels:parties.roles.${r}`)}
              {r === 'owner' && row.original.share_millis !== null
                ? ` ${formatShare(row.original.share_millis, i18n.language)}%`
                : ''}
            </Badge>
          ))}
        </div>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title={customerName(t, customer)}
        description={t(`customers:kind.${customer.kind}`)}
        actions={
          <>
            {can('customers.write') && !locked ? (
              <>
                {customer.archived_at ? (
                  <Button
                    variant="outline"
                    disabled={archive.isPending}
                    onClick={() =>
                      archive.mutate(
                        { customerId: customer.id, archive: false },
                        {
                          onSuccess: () => toast.success(t('customers:actions.unarchived')),
                          onError,
                        },
                      )
                    }
                  >
                    <ArchiveRestoreIcon />
                    {t('customers:actions.unarchive')}
                  </Button>
                ) : (
                  <Button
                    variant="outline"
                    disabled={archive.isPending}
                    onClick={() =>
                      archive.mutate(
                        { customerId: customer.id, archive: true },
                        {
                          onSuccess: () => toast.success(t('customers:actions.archived')),
                          onError,
                        },
                      )
                    }
                  >
                    <ArchiveIcon />
                    {t('customers:actions.archive')}
                  </Button>
                )}
                <Button variant="outline" onClick={() => setDialog('merge')}>
                  <GitMergeIcon />
                  {t('customers:actions.merge')}
                </Button>
              </>
            ) : null}
            {customer.anonymized_at ? null : (
              <Button variant="outline" onClick={doExport} disabled={exportCustomer.isPending}>
                <DownloadIcon />
                {t('customers:actions.export')}
              </Button>
            )}
            {can('customers.write') && !customer.anonymized_at ? (
              <Button variant="destructive" onClick={() => setDialog('anonymize')}>
                <UserXIcon />
                {t('customers:actions.anonymize')}
              </Button>
            ) : null}
          </>
        }
      />

      <div className="mb-4 flex flex-wrap gap-2">
        {customer.anonymized_at ? (
          <Badge variant="secondary">{t('customers:status.anonymized')}</Badge>
        ) : null}
        {customer.archived_at ? (
          <Badge variant="secondary">{t('customers:status.archived')}</Badge>
        ) : null}
        {customer.category ? (
          <Badge variant="outline">{t(`customers:categories.${customer.category}`)}</Badge>
        ) : null}
        {customer.tags.map((tag) => (
          <Badge key={tag} variant="outline">
            {tag}
          </Badge>
        ))}
      </div>

      {customer.merged_into_id ? (
        <Alert className="mb-4">
          <GitMergeIcon />
          <AlertTitle>{t('customers:status.merged')}</AlertTitle>
          <AlertDescription>
            <Link
              to="/m/$marinaId/customers/$customerId"
              params={{ marinaId, customerId: customer.merged_into_id }}
              className="underline-offset-4 hover:underline"
            >
              {t('customers:merge.openTarget')}
            </Link>
          </AlertDescription>
        </Alert>
      ) : null}

      {data.duplicates.length > 0 && !locked ? (
        <Alert className="mb-4">
          <GitMergeIcon />
          <AlertTitle>{t('customers:duplicates.title')}</AlertTitle>
          <AlertDescription>
            <p>{t('customers:duplicates.pageHint', { count: data.duplicates.length })}</p>
          </AlertDescription>
        </Alert>
      ) : null}

      <Tabs defaultValue="data">
        <TabsList className="flex-wrap">
          <TabsTrigger value="data">{t('customers:tabs.data')}</TabsTrigger>
          <TabsTrigger value="vessels">
            {t('customers:tabs.vessels')} ({data.vessels.length})
          </TabsTrigger>
          <TabsTrigger value="documents">
            {t('customers:tabs.documents')} ({data.documents.length})
          </TabsTrigger>
          <TabsTrigger value="consents">{t('customers:tabs.consents')}</TabsTrigger>
          <TabsTrigger value="deadlines">
            {t('customers:tabs.deadlines')} ({data.deadlines.length})
          </TabsTrigger>
          <TabsTrigger value="dashboard">{t('customerBilling:tabs.dashboard')}</TabsTrigger>
          {can('charges.manage') ? (
            <TabsTrigger value="statement">{t('customerBilling:tabs.statement')}</TabsTrigger>
          ) : null}
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
              <CustomerForm
                key={customer.version}
                defaultValues={customerToValues(customer)}
                ibanMasked={customer.iban_masked}
                submitLabel={t('common:actions.save')}
                pending={update.isPending}
                disabled={!canWrite}
                error={update.error}
                onSubmit={(values, { clearIban }) =>
                  update.mutate(
                    {
                      customerId: customer.id,
                      version: customer.version,
                      patch: valuesToPatch(values, clearIban),
                    },
                    { onSuccess: () => toast.success(t('customers:form.saved')) },
                  )
                }
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="vessels" className="flex flex-col gap-3">
          {can('customers.write') && !locked ? (
            <div className="flex justify-end">
              <Button asChild variant="outline">
                <Link
                  to="/m/$marinaId/vessels/new"
                  params={{ marinaId }}
                  search={{ customer: customer.id }}
                >
                  <PlusIcon />
                  {t('customers:vessels.add')}
                </Link>
              </Button>
            </div>
          ) : null}
          <DataTable
            columns={vesselColumns}
            data={data.vessels}
            getRowId={(v) => v.vessel.id}
            empty={t('customers:vessels.empty')}
          />
        </TabsContent>

        <TabsContent value="documents">
          <DocumentsPanel
            owner={{ type: 'customer', id: customer.id }}
            documents={data.documents}
            canWrite={canWrite}
            today={today}
          />
        </TabsContent>

        <TabsContent value="consents">
          <ConsentsPanel customerId={customer.id} current={data.consents} canWrite={canWrite} />
        </TabsContent>

        <TabsContent value="deadlines">
          <DeadlineTable deadlines={data.deadlines} canWrite={can('customers.write')} hideSubject />
        </TabsContent>

        {/* M5 (m5-plan §9.1): customer dashboard (11.4) and statement of account. */}
        <TabsContent value="dashboard">
          <CustomerDashboardTab customerId={customer.id} customerName={customerName(t, customer)} />
        </TabsContent>
        {can('charges.manage') ? (
          <TabsContent value="statement">
            <CustomerStatementTab customerId={customer.id} />
          </TabsContent>
        ) : null}
      </Tabs>

      <MergeDialog
        target={customer}
        suggestions={data.duplicates}
        open={dialog === 'merge'}
        onOpenChange={(o) => setDialog(o ? 'merge' : 'none')}
        onMerged={() => toast.success(t('customers:merge.done'))}
      />
      <AnonymizeDialog
        customerId={customer.id}
        open={dialog === 'anonymize'}
        onOpenChange={(o) => setDialog(o ? 'anonymize' : 'none')}
        onDone={() => toast.success(t('customers:anonymize.done'))}
      />
    </>
  );
}
