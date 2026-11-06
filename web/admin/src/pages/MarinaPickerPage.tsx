import { Link, Navigate, useNavigate } from '@tanstack/react-router';
import { AnchorIcon, ChevronRightIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { EmptyState, PageHeader } from '@porto/shared/ui/patterns';

import { useMe } from '../auth/hooks';
import { ownedOrganizations } from '../auth/me';
import { CreateMarinaForm } from '../components/CreateMarinaForm';
import { LoadingState } from '../components/PageStates';

export function MarinaPickerPage() {
  const { t } = useT();
  const navigate = useNavigate();
  const { data: me, isPending } = useMe();
  if (isPending) return <LoadingState />;
  if (!me) return null;

  const marinas = me.marinas;
  if (marinas.length === 1 && marinas[0]) {
    return <Navigate to="/m/$marinaId" params={{ marinaId: marinas[0].id }} replace />;
  }

  const owned = ownedOrganizations(me);
  if (marinas.length === 0) {
    const org = owned[0];
    if (org) {
      return (
        <Card className="mx-auto max-w-lg">
          <CardHeader>
            <CardTitle>{t('marina:create.title')}</CardTitle>
            <CardDescription>{t('marina:create.description')}</CardDescription>
          </CardHeader>
          <CardContent>
            <CreateMarinaForm
              organizationId={org.id}
              onCreated={(marina) =>
                void navigate({ to: '/m/$marinaId', params: { marinaId: marina.id } })
              }
            />
          </CardContent>
        </Card>
      );
    }
    return (
      <EmptyState
        icon={<AnchorIcon />}
        title={t('common:picker.none')}
        description={t('common:picker.noneHint')}
      />
    );
  }

  return (
    <>
      <PageHeader title={t('common:picker.title')} description={t('common:picker.description')} />
      <ul className="flex flex-col gap-2">
        {marinas.map((m) => (
          <li key={m.id}>
            <Link
              to="/m/$marinaId"
              params={{ marinaId: m.id }}
              className="hover:bg-accent focus-visible:ring-ring flex items-center justify-between rounded-md border p-4 focus-visible:ring-2 focus-visible:outline-none"
            >
              <span className="flex flex-col">
                <span className="font-medium">{m.name}</span>
                <span className="text-muted-foreground text-sm">{m.slug}</span>
              </span>
              <ChevronRightIcon className="size-4" />
            </Link>
          </li>
        ))}
      </ul>
    </>
  );
}
