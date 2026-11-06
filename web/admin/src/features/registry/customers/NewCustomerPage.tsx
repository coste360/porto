import { Link, useNavigate } from '@tanstack/react-router';
import { TriangleAlertIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import { useCreateCustomer, type CustomerCreated } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent } from '@porto/shared/ui/card';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { customerName } from '../lib/format';
import { CustomerForm } from './CustomerForm';
import { valuesToInput } from './customerSchema';

export function NewCustomerPage() {
  const { t } = useT();
  const { can, marinaId } = useCurrentMarina();
  const navigate = useNavigate();
  const create = useCreateCustomer(marinaId);
  const [created, setCreated] = useState<CustomerCreated | null>(null);

  const open = (customerId: string) =>
    void navigate({ to: '/m/$marinaId/customers/$customerId', params: { marinaId, customerId } });

  return (
    <RequirePermission allowed={can('customers.write')}>
      <PageHeader title={t('customers:form.createTitle')} />
      {created ? (
        <Alert className="mb-6">
          <TriangleAlertIcon />
          <AlertTitle>{t('customers:duplicates.title')}</AlertTitle>
          <AlertDescription>
            <p>{t('customers:duplicates.description')}</p>
            <ul className="my-2 flex list-disc flex-col gap-1 pl-5">
              {created.possible_duplicates.map((d) => (
                <li key={d.id}>
                  <Link
                    to="/m/$marinaId/customers/$customerId"
                    params={{ marinaId, customerId: d.id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {customerName(t, d)}
                  </Link>
                  {d.fiscal_code ? (
                    <span className="text-muted-foreground ml-2 font-mono text-xs">
                      {d.fiscal_code}
                    </span>
                  ) : null}
                </li>
              ))}
            </ul>
            <Button size="sm" onClick={() => open(created.customer.id)}>
              {t('customers:duplicates.open')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      <Card>
        <CardContent>
          {create.isError ? <ProblemAlert error={create.error} className="mb-4" /> : null}
          <CustomerForm
            submitLabel={t('common:actions.create')}
            pending={create.isPending}
            error={create.error}
            disabled={created !== null}
            onCancel={() => void navigate({ to: '/m/$marinaId/customers', params: { marinaId } })}
            onSubmit={(values) =>
              create.mutate(valuesToInput(values), {
                onSuccess: (result) => {
                  toast.success(t('customers:form.created'));
                  if (result.possible_duplicates.length > 0) setCreated(result);
                  else open(result.customer.id);
                },
              })
            }
          />
        </CardContent>
      </Card>
    </RequirePermission>
  );
}
