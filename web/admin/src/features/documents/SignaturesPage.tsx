import { useState } from 'react';
import { toast } from 'sonner';

import {
  useSignatureRequests,
  useSignatureSettings,
  useUpdateSignatureSettings,
  type SignatureRequestStatus,
  type SignatureSettings,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  EnumSelect,
  LoadMore,
  M4ProblemAlert,
  PageFrame,
  Section,
} from '../contracts-m4/components/kit';
import { SignatureDetail } from './components/SignaturePanel';
import { signatureVariant } from './lib/signature';

const STATUSES: readonly SignatureRequestStatus[] = [
  'pending_document',
  'sent',
  'partially_signed',
  'signed',
  'declined',
  'expired',
  'cancelled',
];

/** Signature requests by status (spec 15.4) with the detail and signers timeline of the selected one. */
export function SignaturesPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<SignatureRequestStatus | ''>('sent');
  const list = useSignatureRequests(marinaId, status ? { status } : {});
  const [selected, setSelected] = useState('');
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const current = items.find((r) => r.id === selected);
  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageFrame>
        <PageHeader
          title={t('signature:list.title')}
          description={t('signature:list.description')}
        />
        <EnumSelect
          id="sig-status"
          label={t('signature:list.status')}
          group="signatureStatus"
          values={STATUSES}
          value={status}
          onChange={setStatus}
          allLabel={t('contractsM4:common.all')}
        />
        <div className="grid gap-4 lg:grid-cols-2">
          <div className="flex flex-col gap-2">
            {list.isPending ? (
              <LoadingState rows={5} />
            ) : list.isError ? (
              <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
            ) : items.length === 0 ? (
              <p className="text-muted-foreground text-sm">{t('signature:list.empty')}</p>
            ) : (
              <ul className="flex flex-col gap-2">
                {items.map((r) => (
                  <li key={r.id}>
                    <button
                      type="button"
                      aria-pressed={selected === r.id}
                      className="hover:bg-muted flex w-full flex-wrap items-center gap-2 rounded-md border p-2 text-left text-sm"
                      onClick={() => setSelected(r.id)}
                    >
                      <Badge variant={signatureVariant(r.status)}>
                        {t(`docsEnums:signatureStatus.${r.status}`)}
                      </Badge>
                      <span>{t(`signature:subjects.${r.subject_type}`)}</span>
                      <span>{t(`docsEnums:signatureDriver.${r.driver}`)}</span>
                      <span className="text-muted-foreground">
                        {t('signature:list.signed', {
                          signed: r.signers.filter((s) => s.status === 'signed').length,
                          total: r.signers.length,
                        })}
                      </span>
                      <span className="text-muted-foreground ml-auto text-xs">
                        {formatDateTime(r.created_at, i18n.language)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
            <LoadMore query={list} />
          </div>
          <div>
            {current ? (
              <Section title={t(`signature:subjects.${current.subject_type}`)}>
                <SignatureDetail request={current} />
              </Section>
            ) : (
              <p className="text-muted-foreground text-sm">{t('signature:list.select')}</p>
            )}
          </div>
        </div>
      </PageFrame>
    </RequirePermission>
  );
}

/** Signature provider settings (providers.configure): provider code, credentials and webhook secret, write-only. */
export function SignatureSettingsPage() {
  const { can, marinaId } = useCurrentMarina();
  const settings = useSignatureSettings(marinaId);
  return (
    <RequirePermission allowed={can('providers.configure')}>
      <PageFrame>
        {settings.isPending ? (
          <LoadingState rows={4} />
        ) : settings.isError ? (
          <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />
        ) : (
          <SignatureSettingsForm key={settings.data.version} settings={settings.data} />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function SignatureSettingsForm({ settings }: { settings: SignatureSettings }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const save = useUpdateSignatureSettings(marinaId);
  const [provider, setProvider] = useState(settings.provider_code ?? '');
  const [credentials, setCredentials] = useState('');
  const [secret, setSecret] = useState('');
  const [jsonError, setJsonError] = useState(false);
  return (
    <>
      <PageHeader
        title={t('signature:settings.title')}
        description={t('signature:settings.description')}
        actions={
          <Button
            disabled={save.isPending}
            onClick={() => {
              let creds: Record<string, string> | undefined;
              if (credentials.trim()) {
                try {
                  const parsed: unknown = JSON.parse(credentials);
                  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
                    throw new Error('object');
                  if (Object.values(parsed).some((v) => typeof v !== 'string'))
                    throw new Error('strings');
                  creds = parsed as Record<string, string>;
                } catch {
                  setJsonError(true);
                  return;
                }
              }
              setJsonError(false);
              save.mutate(
                {
                  version: settings.version,
                  body: {
                    provider_code: provider || null,
                    ...(creds ? { credentials: creds } : {}),
                    ...(secret ? { webhook_secret: secret } : {}),
                  },
                },
                {
                  onSuccess: () => {
                    setCredentials('');
                    setSecret('');
                    toast.success(t('signature:settings.saved'));
                  },
                },
              );
            }}
          >
            {t('common:actions.save')}
          </Button>
        }
      />
      <M4ProblemAlert error={save.error} />
      <Section
        title={t('signature:settings.provider')}
        description={t('signature:settings.providerHint')}
      >
        {settings.available_providers.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('signature:settings.noProviders')}</p>
        ) : (
          <EnumSelect
            id="sig-provider"
            label={t('signature:settings.provider')}
            values={settings.available_providers}
            labelOf={(p) => p}
            value={provider}
            onChange={setProvider}
            allLabel={t('signature:settings.none')}
          />
        )}
        <div className="flex flex-wrap gap-2 text-sm">
          <Badge variant={settings.has_credentials ? 'default' : 'outline'}>
            {settings.has_credentials
              ? t('signature:settings.hasCredentials')
              : t('signature:settings.noCredentials')}
          </Badge>
          <Badge variant={settings.has_webhook_secret ? 'default' : 'outline'}>
            {settings.has_webhook_secret
              ? t('signature:settings.hasSecret')
              : t('signature:settings.noSecret')}
          </Badge>
        </div>
        <Field data-invalid={jsonError || undefined}>
          <FieldLabel htmlFor="sig-credentials">{t('signature:settings.credentials')}</FieldLabel>
          <Textarea
            id="sig-credentials"
            className="font-mono text-sm"
            autoComplete="off"
            value={credentials}
            onChange={(e) => setCredentials(e.target.value)}
          />
          <FieldDescription>
            {jsonError
              ? t('signature:settings.jsonError')
              : t('signature:settings.credentialsHint')}
          </FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="sig-secret">{t('signature:settings.secret')}</FieldLabel>
          <Input
            id="sig-secret"
            type="password"
            autoComplete="off"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
          />
        </Field>
        {settings.webhook_url ? (
          <Field>
            <FieldLabel htmlFor="sig-webhook">{t('signature:settings.webhookUrl')}</FieldLabel>
            <Input id="sig-webhook" readOnly value={settings.webhook_url} />
          </Field>
        ) : null}
      </Section>
    </>
  );
}
