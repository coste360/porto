import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { toast } from 'sonner';

import { useCreateVessel, useCustomerOverview } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { customerName } from '../lib/format';
import { checkParties, newRowKey, type PartyRow } from './parties';
import { PartiesEditor } from './PartiesEditor';
import { VesselForm } from './VesselForm';
import { valuesToVesselInput } from './vesselSchema';

const routeApi = getRouteApi('/auth/m/$marinaId/vessels/new');

export function NewVesselPage() {
  const { can, marinaId } = useCurrentMarina();
  const { customer } = routeApi.useSearch();
  return (
    <RequirePermission allowed={can('customers.write')}>
      {customer ? (
        <WithOwner marinaId={marinaId} customerId={customer} />
      ) : (
        <NewVesselForm initialRows={[]} />
      )}
    </RequirePermission>
  );
}

/** Opened from a customer page: the customer starts as the sole owner. */
function WithOwner({ marinaId, customerId }: { marinaId: string; customerId: string }) {
  const { t } = useT();
  const overview = useCustomerOverview(marinaId, customerId);
  if (overview.isPending) return null;
  const rows: PartyRow[] = overview.data
    ? [
        {
          key: newRowKey(),
          customer_id: customerId,
          display_name: customerName(t, overview.data.customer),
          role: 'owner',
          share: '100',
        },
      ]
    : [];
  return <NewVesselForm initialRows={rows} />;
}

function NewVesselForm({ initialRows }: { initialRows: PartyRow[] }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const navigate = useNavigate();
  const create = useCreateVessel(marinaId);
  const [rows, setRows] = useState<PartyRow[]>(initialRows);
  const [showErrors, setShowErrors] = useState(false);
  const check = checkParties(rows);

  return (
    <>
      <PageHeader title={t('vessels:form.createTitle')} />
      <Card>
        <CardContent>
          {create.isError ? <ProblemAlert error={create.error} className="mb-4" /> : null}
          <VesselForm
            submitLabel={t('common:actions.create')}
            pending={create.isPending}
            error={create.error}
            onCancel={() => void navigate({ to: '/m/$marinaId/vessels', params: { marinaId } })}
            onSubmit={(values) => {
              if (!check.ok) {
                setShowErrors(true);
                return;
              }
              create.mutate(valuesToVesselInput(values, check.parties), {
                onSuccess: (vessel) => {
                  toast.success(t('vessels:form.created'));
                  void navigate({
                    to: '/m/$marinaId/vessels/$vesselId',
                    params: { marinaId, vesselId: vessel.id },
                  });
                },
              });
            }}
          >
            <Card>
              <CardHeader>
                <CardTitle>{t('vessels:parties.title')}</CardTitle>
              </CardHeader>
              <CardContent>
                <PartiesEditor
                  rows={rows}
                  onChange={setRows}
                  errors={showErrors && !check.ok ? check.errors : {}}
                  ownerTotal={check.ownerTotal}
                  disabled={create.isPending}
                />
              </CardContent>
            </Card>
          </VesselForm>
        </CardContent>
      </Card>
    </>
  );
}
