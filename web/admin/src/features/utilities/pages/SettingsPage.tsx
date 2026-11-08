import { useState } from 'react';

import {
  useItems,
  useUpdateUtilitySettings,
  useUtilitySettings,
  type UtilitySettings,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatQtyMilli, parseQtyToMilli } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { EnumSelect, M4ProblemAlert, PageFrame, Section } from '../../contracts-m4/components/kit';
import { UtilitiesNav } from '../components/kit';

const MODES: readonly UtilitySettings['mode'][] = ['with_installment', 'monthly'];

/** Utility settings (m9-plan §9.4): tariff items, charging mode and daily thresholds. */
export function SettingsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const settings = useUtilitySettings(marinaId);
  return (
    <RequirePermission allowed={can('marina.configure') || can('charges.manage')}>
      <PageFrame>
        <PageHeader
          title={t('utilities:settings.title')}
          description={t('utilities:settings.description')}
        />
        <UtilitiesNav marinaId={marinaId} current="settings" />
        {settings.isPending ? (
          <LoadingState rows={3} />
        ) : settings.isError ? (
          <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />
        ) : (
          <SettingsForm
            key={settings.data.version}
            marinaId={marinaId}
            settings={settings.data}
            editable={can('marina.configure')}
          />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function SettingsForm({
  marinaId,
  settings,
  editable,
}: {
  marinaId: string;
  settings: UtilitySettings;
  editable: boolean;
}) {
  const { t, i18n } = useT();
  const items = useItems(marinaId, { active: true });
  const save = useUpdateUtilitySettings(marinaId);
  const list = items.data?.pages.flatMap((p) => p.items) ?? [];
  const [mode, setMode] = useState<UtilitySettings['mode'] | ''>(settings.mode);
  const [power, setPower] = useState(settings.power_item_id ?? '');
  const [water, setWater] = useState(settings.water_item_id ?? '');
  const fmt = (v: number | undefined) => (v ? formatQtyMilli(v, i18n.language) : '');
  const [powerMax, setPowerMax] = useState(fmt(settings.power_daily_threshold_milli));
  const [waterMax, setWaterMax] = useState(fmt(settings.water_daily_threshold_milli));
  const parse = (s: string) => (s.trim() ? parseQtyToMilli(s, i18n.language) : null);
  const powerMilli = parse(powerMax);
  const waterMilli = parse(waterMax);
  const invalid =
    (powerMax.trim() !== '' && !powerMilli) || (waterMax.trim() !== '' && !waterMilli);
  return (
    <Section title={t('utilities:settings.charging')}>
      <div className="grid gap-4 sm:grid-cols-2">
        <EnumSelect
          id="utl-mode"
          label={t('utilities:fields.mode')}
          values={MODES}
          value={mode}
          onChange={setMode}
          labelOf={(m) => t(`utilities:enums.mode.${m}`)}
        />
        <span />
        <EnumSelect
          id="utl-power-item"
          label={t('utilities:fields.powerItem')}
          values={list.map((i) => i.id)}
          value={power}
          onChange={setPower}
          labelOf={(id) => list.find((i) => i.id === id)?.code ?? id}
          allLabel={t('utilities:none')}
        />
        <EnumSelect
          id="utl-water-item"
          label={t('utilities:fields.waterItem')}
          values={list.map((i) => i.id)}
          value={water}
          onChange={setWater}
          labelOf={(id) => list.find((i) => i.id === id)?.code ?? id}
          allLabel={t('utilities:none')}
        />
        <Field>
          <FieldLabel htmlFor="utl-power-max">{t('utilities:fields.powerThreshold')}</FieldLabel>
          <Input
            id="utl-power-max"
            inputMode="decimal"
            value={powerMax}
            onChange={(e) => setPowerMax(e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="utl-water-max">{t('utilities:fields.waterThreshold')}</FieldLabel>
          <Input
            id="utl-water-max"
            inputMode="decimal"
            value={waterMax}
            onChange={(e) => setWaterMax(e.target.value)}
          />
        </Field>
      </div>
      <div className="mt-4 flex flex-col gap-3">
        <M4ProblemAlert error={save.error} />
        {editable ? (
          <div>
            <Button
              disabled={invalid || save.isPending}
              onClick={() =>
                save.mutate({
                  version: settings.version,
                  body: {
                    mode: mode || 'with_installment',
                    ...(power ? { power_item_id: power } : {}),
                    ...(water ? { water_item_id: water } : {}),
                    ...(powerMilli ? { power_daily_threshold_milli: powerMilli } : {}),
                    ...(waterMilli ? { water_daily_threshold_milli: waterMilli } : {}),
                  },
                })
              }
            >
              {t('utilities:settings.save')}
            </Button>
          </div>
        ) : null}
      </div>
    </Section>
  );
}
