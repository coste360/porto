import { useOnlineBookingSettings } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { PageHeader } from '@porto/shared/ui/patterns';
import { ScrollArea, ScrollBar } from '@porto/shared/ui/scroll-area';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { ChannelsTab } from '../components/ChannelsTab';
import { EmbedTab } from '../components/EmbedTab';
import { QuotasTab } from '../components/QuotasTab';
import { SettingsForm } from '../components/SettingsForm';
import { TextsTab } from '../components/TextsTab';

const FORM_TABS = ['rules', 'payment', 'approval', 'arrival'] as const;

/**
 * Online booking configuration (m8-plan §12.3): rules, payment and cancellation, approval, texts, arrival, quotas,
 * embed, channels. Quotas need harbor.configure and channels providers.configure; their tabs show only then.
 */
export function OnlineSettingsPage() {
  const { t } = useT();
  const { marinaId, marina, can } = useCurrentMarina();
  const settings = useOnlineBookingSettings(marinaId);
  const showQuotas = can('harbor.configure');
  const showChannels = can('providers.configure');
  return (
    <RequirePermission allowed={can('marina.configure')}>
      <PageHeader title={t('online:nav.settings')} description={t('online:settings.description')} />
      {settings.isPending ? (
        <LoadingState rows={6} />
      ) : settings.isError ? (
        <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />
      ) : (
        <div className="flex flex-col gap-4">
          {!settings.data.booking_enabled ? (
            <Alert>
              <AlertDescription>{t('online:settings.disabled')}</AlertDescription>
            </Alert>
          ) : null}
          {!settings.data.saved ? (
            <Alert data-testid="online-settings-defaults">
              <AlertDescription>{t('online:settings.notSaved')}</AlertDescription>
            </Alert>
          ) : null}
          <Tabs defaultValue="rules">
            <ScrollArea className="w-full">
              <TabsList>
                {FORM_TABS.map((k) => (
                  <TabsTrigger key={k} value={k}>
                    {t(`online:settings.tabs.${k}`)}
                  </TabsTrigger>
                ))}
                <TabsTrigger value="texts">{t('online:settings.tabs.texts')}</TabsTrigger>
                {showQuotas ? (
                  <TabsTrigger value="quotas">{t('online:settings.tabs.quotas')}</TabsTrigger>
                ) : null}
                <TabsTrigger value="embed">{t('online:settings.tabs.embed')}</TabsTrigger>
                {showChannels ? (
                  <TabsTrigger value="channels">{t('online:settings.tabs.channels')}</TabsTrigger>
                ) : null}
              </TabsList>
              <ScrollBar orientation="horizontal" />
            </ScrollArea>
            <SettingsForm marinaId={marinaId} settings={settings.data} />
            <TabsContent value="texts">
              <TextsTab marinaId={marinaId} />
            </TabsContent>
            {showQuotas ? (
              <TabsContent value="quotas">
                <QuotasTab marinaId={marinaId} />
              </TabsContent>
            ) : null}
            <TabsContent value="embed">
              <EmbedTab slug={marina?.slug ?? ''} origins={settings.data.embed_origins} />
            </TabsContent>
            {showChannels ? (
              <TabsContent value="channels">
                <ChannelsTab marinaId={marinaId} />
              </TabsContent>
            ) : null}
          </Tabs>
        </div>
      )}
    </RequirePermission>
  );
}
