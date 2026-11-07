import { useState } from 'react';
import { toast } from 'sonner';

import {
  usePaymentProviders,
  useProviderSettings,
  useTestProvider,
  useUpdateProviderSettings,
  type PaymentProviderInfo,
  type PaymentProviderSettings,
  type PaymentProviderSettingsInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { CopyButton } from '../components/PaymentRequestDialog';
import { BillingProblemAlert, EnumSelect, PageFrame, Section } from '../components/kit';
import { OnlineFeatureNotice } from '../../desktop/OnlineFeatureNotice';

type Method = NonNullable<PaymentProviderSettingsInput['methods']>[number];
const METHODS: readonly Method[] = ['card', 'wallet', 'sepa_debit'];

/**
 * Payment providers (m5-plan §5.4, §9.1): the manual driver is always on; an online provider is optional, disabled
 * by default, and its secrets are write-only (only "set" / "not set" ever comes back).
 */
export function PaymentSettingsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const providers = usePaymentProviders(marinaId);
  const online = (providers.data?.items ?? []).filter((p) => p.code !== 'manual');
  return (
    <RequirePermission allowed={can('providers.configure')}>
      <PageFrame>
        <PageHeader
          title={t('paymentProviders:title')}
          description={t('paymentProviders:description')}
        />
        <OnlineFeatureNotice feature="onlinePayments" />
        <Alert data-testid="offline-banner">
          <AlertTitle>{t('paymentProviders:offline.title')}</AlertTitle>
          <AlertDescription>{t('paymentProviders:offline.description')}</AlertDescription>
        </Alert>
        <Section title={t('paymentProviders:manual.title')}>
          <p className="text-sm">{t('paymentProviders:manual.description')}</p>
          <div>
            <Badge>{t('paymentProviders:manual.alwaysOn')}</Badge>
          </div>
        </Section>
        {providers.isPending ? (
          <LoadingState />
        ) : providers.isError ? (
          <QueryErrorState error={providers.error} onRetry={() => void providers.refetch()} />
        ) : (
          online.map((p) => <ProviderCard key={p.code} provider={p} />)
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function ProviderCard({ provider }: { provider: PaymentProviderInfo }) {
  const { marinaId } = useCurrentMarina();
  const settings = useProviderSettings(marinaId, provider.code);
  if (settings.isPending) return <LoadingState />;
  if (settings.isError) return <QueryErrorState error={settings.error} />;
  return <ProviderForm key={settings.data.version} provider={provider} settings={settings.data} />;
}

/** Placeholder of a write-only secret: "set" (masked) or empty. */
function secretPlaceholder(has: boolean, t: (k: string) => string) {
  return has ? t('paymentProviders:secret.set') : t('paymentProviders:secret.notSet');
}

function ProviderForm({
  provider,
  settings,
}: {
  provider: PaymentProviderInfo;
  settings: PaymentProviderSettings;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateProviderSettings(marinaId);
  const test = useTestProvider(marinaId);
  const [enabled, setEnabled] = useState(settings.enabled);
  const [mode, setMode] = useState<'test' | 'live'>(settings.mode);
  const [sepa, setSepa] = useState(settings.sepa_enabled);
  const [methods, setMethods] = useState<Method[]>(
    settings.methods.filter((m): m is Method => (METHODS as readonly string[]).includes(m)),
  );
  const [descriptor, setDescriptor] = useState(settings.statement_descriptor);
  const [secretKey, setSecretKey] = useState('');
  const [publishableKey, setPublishableKey] = useState('');
  const [webhookSecret, setWebhookSecret] = useState('');
  const name = t(`paymentProviders:names.${provider.code}`, provider.code);
  const save = () =>
    update.mutate(
      {
        code: provider.code,
        version: settings.version,
        body: {
          enabled,
          mode,
          sepa_enabled: sepa,
          methods,
          statement_descriptor: descriptor,
          ...(secretKey ? { secret_key: secretKey } : {}),
          ...(publishableKey ? { publishable_key: publishableKey } : {}),
          ...(webhookSecret ? { webhook_secret: webhookSecret } : {}),
        },
      },
      {
        onSuccess: () => {
          setSecretKey('');
          setPublishableKey('');
          setWebhookSecret('');
          toast.success(t('paymentProviders:saved'));
        },
      },
    );
  return (
    <Section
      title={name}
      description={t('paymentProviders:provider.description')}
      action={
        <Badge variant={settings.enabled ? 'default' : 'outline'} data-testid="provider-state">
          {settings.enabled ? t('paymentProviders:enabled') : t('paymentProviders:disabled')}
        </Badge>
      }
    >
      <div data-testid={`provider-${provider.code}`} className="flex flex-col gap-4">
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={enabled} onCheckedChange={setEnabled} />
          {t('paymentProviders:provider.enable')}
        </label>
        <EnumSelect
          id={`prov-mode-${provider.code}`}
          label={t('paymentProviders:provider.mode')}
          values={['test', 'live'] as const}
          labelOf={(m) => t(`billingEnums:providerMode.${m}`)}
          value={mode}
          onChange={(m) => m && setMode(m)}
        />
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor={`prov-sk-${provider.code}`}>
              {t('paymentProviders:provider.secretKey')}
            </FieldLabel>
            <Input
              id={`prov-sk-${provider.code}`}
              type="password"
              autoComplete="off"
              maxLength={256}
              placeholder={secretPlaceholder(settings.has_secret_key, t)}
              value={secretKey}
              onChange={(e) => setSecretKey(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`prov-ws-${provider.code}`}>
              {t('paymentProviders:provider.webhookSecret')}
            </FieldLabel>
            <Input
              id={`prov-ws-${provider.code}`}
              type="password"
              autoComplete="off"
              maxLength={256}
              placeholder={secretPlaceholder(settings.has_webhook_secret, t)}
              value={webhookSecret}
              onChange={(e) => setWebhookSecret(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`prov-pk-${provider.code}`}>
              {t('paymentProviders:provider.publishableKey')}
            </FieldLabel>
            <Input
              id={`prov-pk-${provider.code}`}
              autoComplete="off"
              maxLength={256}
              placeholder={
                settings.publishable_key_last4
                  ? `••••${settings.publishable_key_last4}`
                  : t('paymentProviders:secret.notSet')
              }
              value={publishableKey}
              onChange={(e) => setPublishableKey(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`prov-desc-${provider.code}`}>
              {t('paymentProviders:provider.descriptor')}
            </FieldLabel>
            <Input
              id={`prov-desc-${provider.code}`}
              maxLength={22}
              value={descriptor}
              onChange={(e) => setDescriptor(e.target.value)}
            />
          </Field>
        </div>
        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-medium">{t('paymentProviders:provider.methods')}</legend>
          <div className="flex flex-wrap gap-3">
            {METHODS.map((m) => (
              <label key={m} className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={methods.includes(m)}
                  onCheckedChange={(v) =>
                    setMethods(v === true ? [...methods, m] : methods.filter((x) => x !== m))
                  }
                />
                {t(`paymentProviders:methods.${m}`)}
              </label>
            ))}
          </div>
        </fieldset>
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={sepa} onCheckedChange={setSepa} />
          {t('paymentProviders:provider.sepa')}
        </label>
        <div className="flex flex-col gap-2">
          <Field>
            <FieldLabel htmlFor={`prov-wh-${provider.code}`}>
              {t('paymentProviders:provider.webhookUrl')}
            </FieldLabel>
            <Input id={`prov-wh-${provider.code}`} readOnly value={settings.webhook_url} />
          </Field>
          <div>
            <CopyButton text={settings.webhook_url} label={t('paymentProviders:provider.copy')} />
          </div>
        </div>
        <BillingProblemAlert error={update.error ?? test.error} />
        {test.data ? (
          <p role="status" className="text-sm" data-testid="provider-test">
            {test.data.ok
              ? t('paymentProviders:test.ok')
              : t('paymentProviders:test.failed', { code: test.data.code ?? '' })}
          </p>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <Button disabled={update.isPending} onClick={save}>
            {t('common:actions.save')}
          </Button>
          <Button
            variant="outline"
            disabled={test.isPending || !settings.has_secret_key}
            onClick={() => test.mutate(provider.code)}
          >
            {t('paymentProviders:test.run')}
          </Button>
        </div>
      </div>
    </Section>
  );
}
