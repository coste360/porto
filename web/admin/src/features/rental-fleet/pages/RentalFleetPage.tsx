import { useT } from '@porto/shared/i18n';
import { PageHeader } from '@porto/shared/ui/patterns';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { PageFrame } from '../../contracts-m4/components/kit';
import { ExtrasTab } from '../components/ExtrasTab';
import { FormulasTab } from '../components/FormulasTab';
import { SettingsTab } from '../components/SettingsTab';
import { SkippersTab } from '../components/SkippersTab';
import { UnitsTab } from '../components/UnitsTab';

const TABS = ['units', 'skippers', 'formulas', 'extras', 'settings'] as const;

/**
 * Rental fleet (m11-plan §14.1): units, skippers with their weekly hours, formulas (with the price coverage of each
 * formula × unit), extras and settings. Units and skippers need rental.manage; formulas, extras and settings are
 * configuration (marina.configure, D20) and read-only otherwise.
 */
export function RentalFleetPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const canConfigure = can('marina.configure');
  return (
    <RequirePermission allowed={can('rental.manage')}>
      <PageFrame>
        <PageHeader title={t('rentalFleet:title')} description={t('rentalFleet:description')} />
        <Tabs defaultValue="units" className="flex flex-col gap-4">
          <TabsList className="flex-wrap">
            {TABS.map((tab) => (
              <TabsTrigger key={tab} value={tab} data-testid={`fleet-tab-${tab}`}>
                {t(`rentalFleet:tabs.${tab}`)}
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value="units">
            <UnitsTab marinaId={marinaId} />
          </TabsContent>
          <TabsContent value="skippers">
            <SkippersTab marinaId={marinaId} />
          </TabsContent>
          <TabsContent value="formulas">
            <FormulasTab marinaId={marinaId} canConfigure={canConfigure} />
          </TabsContent>
          <TabsContent value="extras">
            <ExtrasTab marinaId={marinaId} canConfigure={canConfigure} />
          </TabsContent>
          <TabsContent value="settings">
            <SettingsTab marinaId={marinaId} canConfigure={canConfigure} />
          </TabsContent>
        </Tabs>
      </PageFrame>
    </RequirePermission>
  );
}
