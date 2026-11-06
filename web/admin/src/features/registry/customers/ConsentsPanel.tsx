import { zodResolver } from '@hookform/resolvers/zod';
import type { ColumnDef } from '@tanstack/react-table';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import { useConsents, useRecordConsent, type Consent } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { FieldGroup } from '@porto/shared/ui/field';
import { DataTable, FormField, ProblemAlert } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { SelectField, TextField } from '../components/fields';
import { CONSENT_KINDS, CONSENT_SOURCES } from '../lib/enums';

const consentSchema = z.object({
  kind: z.enum(CONSENT_KINDS),
  policy_version: z.string().trim().min(1, 'common:validation.required').max(50),
  granted: z.boolean(),
  source: z.enum(CONSENT_SOURCES),
});
type ConsentValues = z.infer<typeof consentSchema>;

export function ConsentsPanel({
  customerId,
  current,
  canWrite,
}: {
  customerId: string;
  current: Consent[];
  canWrite: boolean;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const history = useConsents(marinaId, customerId);
  const record = useRecordConsent(marinaId);
  const form = useForm<ConsentValues>({
    resolver: zodResolver(consentSchema),
    defaultValues: { kind: 'privacy', policy_version: '', granted: true, source: 'desk' },
  });

  const columns: ColumnDef<Consent, unknown>[] = [
    {
      header: t('customers:consents.recordedAt'),
      cell: ({ row }) => formatDateTime(row.original.recorded_at, i18n.language),
    },
    {
      header: t('customers:consents.kind'),
      cell: ({ row }) => t(`customers:consents.kinds.${row.original.kind}`),
    },
    { header: t('customers:consents.policyVersion'), accessorKey: 'policy_version' },
    {
      header: t('customers:consents.value'),
      cell: ({ row }) =>
        row.original.granted ? (
          <Badge variant="secondary">{t('customers:consents.granted')}</Badge>
        ) : (
          <Badge variant="outline">{t('customers:consents.denied')}</Badge>
        ),
    },
    {
      header: t('customers:consents.source'),
      cell: ({ row }) => t(`customers:consents.sources.${row.original.source}`),
    },
    {
      header: t('customers:consents.by'),
      cell: ({ row }) => row.original.recorded_by?.name ?? '',
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-3 sm:grid-cols-3">
        {CONSENT_KINDS.map((kind) => {
          const c = current.find((x) => x.kind === kind);
          return (
            <Card key={kind}>
              <CardHeader>
                <CardTitle className="text-sm">{t(`customers:consents.kinds.${kind}`)}</CardTitle>
              </CardHeader>
              <CardContent className="flex items-center gap-2 text-sm">
                {c ? (
                  <>
                    <Badge variant={c.granted ? 'secondary' : 'outline'}>
                      {c.granted ? t('customers:consents.granted') : t('customers:consents.denied')}
                    </Badge>
                    <span className="text-muted-foreground">{c.policy_version}</span>
                  </>
                ) : (
                  <span className="text-muted-foreground">{t('customers:consents.none')}</span>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>

      {canWrite ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('customers:consents.record')}</CardTitle>
          </CardHeader>
          <CardContent>
            <form
              noValidate
              className="flex flex-col gap-4"
              onSubmit={form.handleSubmit((consent) =>
                record.mutate(
                  { customerId, consent },
                  {
                    onSuccess: () => {
                      toast.success(t('customers:consents.recorded'));
                      form.reset({ ...consent, policy_version: consent.policy_version });
                    },
                  },
                ),
              )}
            >
              {record.isError ? <ProblemAlert error={record.error} /> : null}
              <FieldGroup className="grid gap-4 sm:grid-cols-4">
                <SelectField
                  control={form.control}
                  name="kind"
                  label={t('customers:consents.kind')}
                  options={CONSENT_KINDS.map((v) => ({
                    value: v,
                    label: t(`customers:consents.kinds.${v}`),
                  }))}
                />
                <TextField
                  control={form.control}
                  name="policy_version"
                  label={t('customers:consents.policyVersion')}
                />
                <SelectField
                  control={form.control}
                  name="source"
                  label={t('customers:consents.source')}
                  options={CONSENT_SOURCES.map((v) => ({
                    value: v,
                    label: t(`customers:consents.sources.${v}`),
                  }))}
                />
                <FormField
                  control={form.control}
                  name="granted"
                  label={t('customers:consents.granted')}
                  render={({ value, onChange, id }) => (
                    <Switch id={id} checked={value} onCheckedChange={onChange} />
                  )}
                />
              </FieldGroup>
              <div className="flex justify-end">
                <Button type="submit" disabled={record.isPending}>
                  {t('customers:consents.record')}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      ) : null}

      <h3 className="text-sm font-medium">{t('customers:consents.history')}</h3>
      {history.isPending ? (
        <LoadingState />
      ) : history.isError ? (
        <QueryErrorState error={history.error} onRetry={() => void history.refetch()} />
      ) : (
        <DataTable
          columns={columns}
          data={history.data.items}
          getRowId={(c) => c.id}
          empty={t('customers:consents.empty')}
        />
      )}
    </div>
  );
}
