import { useState } from 'react';
import { toast } from 'sonner';

import {
  isProblem,
  usePricingSettings,
  useUpdatePricingSettings,
  type PricingSettings,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@porto/shared/ui/field';
import { PageHeader, PercentInput } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';

/** Pricing settings (m2-plan §4.3): default VAT rate and VAT inclusion of new items. */
export function PricingSettingsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const settings = usePricingSettings(marinaId);
  return (
    <RequirePermission allowed={can('pricing.configure')}>
      <PageHeader
        title={t('pricing:settings.title')}
        description={t('pricing:settings.description')}
      />
      {settings.isPending ? (
        <LoadingState />
      ) : settings.isError ? (
        <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />
      ) : (
        <SettingsForm key={settings.data.version} settings={settings.data} />
      )}
    </RequirePermission>
  );
}

function SettingsForm({ settings }: { settings: PricingSettings }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdatePricingSettings(marinaId);
  const [rate, setRate] = useState<number | null>(settings.default_vat_rate_bp);
  const [includes, setIncludes] = useState(settings.default_price_includes_vat);
  const invalid = rate === null || rate < 0 || rate > 10_000;

  const submit = async () => {
    if (invalid) return;
    try {
      await update.mutateAsync({
        version: settings.version,
        body: { default_vat_rate_bp: rate, default_price_includes_vat: includes },
      });
      toast.success(t('common:status.saved'));
    } catch (err) {
      if (isProblem(err, 'version_conflict')) toast.error(t('pricing:settings.conflict'));
      else toastProblem(t, err);
    }
  };

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle>{t('pricing:settings.vatTitle')}</CardTitle>
        <CardDescription>{t('pricing:settings.vatHint')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <FieldGroup>
            <Field data-invalid={invalid || undefined}>
              <FieldLabel htmlFor="pricing-default-vat">
                {t('pricing:settings.defaultVat')}
              </FieldLabel>
              <PercentInput
                id="pricing-default-vat"
                value={rate}
                onChange={setRate}
                locale={i18n.language}
                aria-invalid={invalid}
              />
              {invalid ? <FieldError>{t('pricing:items.validation.vatRate')}</FieldError> : null}
            </Field>
            <Field orientation="horizontal">
              <Switch
                id="pricing-default-includes"
                checked={includes}
                onCheckedChange={setIncludes}
              />
              <FieldLabel htmlFor="pricing-default-includes">
                {t('pricing:settings.defaultIncludesVat')}
              </FieldLabel>
            </Field>
            <FieldDescription>{t('pricing:settings.includesHint')}</FieldDescription>
            <div>
              <Button type="submit" disabled={invalid || update.isPending}>
                {t('common:actions.save')}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  );
}
