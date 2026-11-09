import { useT } from '@porto/shared/i18n';
import { PageHeader } from '@porto/shared/ui/patterns';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { PageFrame } from '../../contracts-m4/components/kit';
import { AlertsInbox } from '../components/AlertsInbox';
import { GatewaysTab, SettingsTab, SourcesTab } from '../components/ConfigTabs';
import { DevicesTab } from '../components/DevicesTab';

/**
 * Sensori (m12-plan §16.1): the alert inbox (map.view, actions by permission), the devices (read with map.view,
 * configured with harbor.configure), gateways and sources (harbor.configure) and the settings.
 */
export function SensorsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const canConfigure = can('harbor.configure');
  return (
    <RequirePermission allowed={can('map.view')}>
      <PageFrame>
        <PageHeader title={t('sensors:title')} description={t('sensors:description')} />
        <Tabs defaultValue="alerts" className="flex flex-col gap-4">
          <TabsList className="flex-wrap">
            <TabsTrigger value="alerts" data-testid="sensors-tab-alerts">
              {t('sensors:tabs.alerts')}
            </TabsTrigger>
            <TabsTrigger value="devices" data-testid="sensors-tab-devices">
              {t('sensors:tabs.devices')}
            </TabsTrigger>
            {canConfigure ? (
              <>
                <TabsTrigger value="gateways" data-testid="sensors-tab-gateways">
                  {t('sensors:tabs.gateways')}
                </TabsTrigger>
                <TabsTrigger value="sources" data-testid="sensors-tab-sources">
                  {t('sensors:tabs.sources')}
                </TabsTrigger>
              </>
            ) : null}
            <TabsTrigger value="settings" data-testid="sensors-tab-settings">
              {t('sensors:tabs.settings')}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="alerts">
            <AlertsInbox marinaId={marinaId} />
          </TabsContent>
          <TabsContent value="devices">
            <DevicesTab marinaId={marinaId} canConfigure={canConfigure} />
          </TabsContent>
          {canConfigure ? (
            <>
              <TabsContent value="gateways">
                <GatewaysTab marinaId={marinaId} />
              </TabsContent>
              <TabsContent value="sources">
                <SourcesTab marinaId={marinaId} />
              </TabsContent>
            </>
          ) : null}
          <TabsContent value="settings">
            <SettingsTab marinaId={marinaId} canConfigure={canConfigure} />
          </TabsContent>
        </Tabs>
      </PageFrame>
    </RequirePermission>
  );
}
