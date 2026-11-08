import { useT } from '@porto/shared/i18n';
import { PageHeader } from '@porto/shared/ui/patterns';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { PageFrame } from '../../contracts-m4/components/kit';
import { SettingsTab, TablesTab, VariablesTab } from '../components/FormulaTabs';
import { PackagesTab } from '../components/PackagesTab';
import { ItemsTab, QualificationsTab, WorkTypesTab } from '../components/TaxonomyTabs';

const TABS = [
  'workTypes',
  'qualifications',
  'items',
  'variables',
  'tables',
  'packages',
  'settings',
] as const;

/** Yard catalogue (m10-plan §14.1): work types, qualifications, item yard settings, formula variables, band tables,
 * work packages with their versions and the yard settings. Readable with yard.read, editable with yard.manage. */
export function YardCatalogPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const canManage = can('yard.manage');
  const props = { marinaId, canManage };
  return (
    <RequirePermission allowed={can('yard.read') || canManage}>
      <PageFrame>
        <div data-testid="yard-catalog-page" className="flex flex-col gap-4">
          <PageHeader title={t('yardCatalog:title')} description={t('yardCatalog:description')} />
          <Tabs defaultValue="packages">
            <TabsList className="flex-wrap">
              {TABS.map((tab) => (
                <TabsTrigger key={tab} value={tab}>
                  {t(`yardCatalog:tabs.${tab}`)}
                </TabsTrigger>
              ))}
            </TabsList>
            <TabsContent value="workTypes">
              <WorkTypesTab {...props} />
            </TabsContent>
            <TabsContent value="qualifications">
              <QualificationsTab {...props} />
            </TabsContent>
            <TabsContent value="items">
              <ItemsTab {...props} />
            </TabsContent>
            <TabsContent value="variables">
              <VariablesTab {...props} />
            </TabsContent>
            <TabsContent value="tables">
              <TablesTab {...props} />
            </TabsContent>
            <TabsContent value="packages">
              <PackagesTab {...props} />
            </TabsContent>
            <TabsContent value="settings">
              <SettingsTab {...props} />
            </TabsContent>
          </Tabs>
        </div>
      </PageFrame>
    </RequirePermission>
  );
}
