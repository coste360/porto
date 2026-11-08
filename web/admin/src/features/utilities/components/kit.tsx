// Building blocks of the utilities pages (m9-plan §12.2): section tabs, quantities with their unit and status
// badges.
import { Link } from '@tanstack/react-router';

import type { MeterReading, UtilityConsumption, UtilityUnit } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatQtyMilli } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';

export type UtilitiesSection = 'meters' | 'readings' | 'consumptions' | 'settings';

export function UtilitiesNav({
  marinaId,
  current,
}: {
  marinaId: string;
  current: UtilitiesSection;
}) {
  const { t } = useT();
  const cls = (on: boolean) =>
    on
      ? 'border-primary text-foreground border-b-2 px-3 py-2 text-sm font-medium'
      : 'text-muted-foreground hover:text-foreground px-3 py-2 text-sm';
  return (
    <nav aria-label={t('utilities:nav.sections')} className="flex flex-wrap gap-1 border-b">
      <Link to="/m/$marinaId/utilities" params={{ marinaId }} className={cls(current === 'meters')}>
        {t('utilities:nav.meters')}
      </Link>
      <Link
        to="/m/$marinaId/utilities/readings"
        params={{ marinaId }}
        className={cls(current === 'readings')}
      >
        {t('utilities:nav.readings')}
      </Link>
      <Link
        to="/m/$marinaId/utilities/consumptions"
        params={{ marinaId }}
        className={cls(current === 'consumptions')}
      >
        {t('utilities:nav.consumptions')}
      </Link>
      <Link
        to="/m/$marinaId/utilities/settings"
        params={{ marinaId }}
        className={cls(current === 'settings')}
      >
        {t('utilities:nav.settings')}
      </Link>
    </nav>
  );
}

/** A quantity in milli with its unit ("12,345 kWh"). */
export function Qty({
  milli,
  unit,
}: {
  milli: number | null | undefined;
  unit?: UtilityUnit | undefined;
}) {
  const { t, i18n } = useT();
  if (milli === null || milli === undefined) return null;
  return (
    <span className="tabular-nums">
      {formatQtyMilli(milli, i18n.language)}
      {unit ? ` ${t(`utilities:enums.unit.${unit}`)}` : ''}
    </span>
  );
}

export function ReadingStatusBadge({ status }: { status: MeterReading['status'] }) {
  const { t } = useT();
  const variant =
    status === 'anomaly' ? 'destructive' : status === 'void' ? 'outline' : 'secondary';
  return <Badge variant={variant}>{t(`utilities:enums.readingStatus.${status}`)}</Badge>;
}

export function ConsumptionStatusBadge({ status }: { status: UtilityConsumption['status'] }) {
  const { t } = useT();
  const variant =
    status === 'anomaly'
      ? 'destructive'
      : status === 'charged'
        ? 'secondary'
        : status === 'pending'
          ? 'default'
          : 'outline';
  return <Badge variant={variant}>{t(`utilities:enums.consumptionStatus.${status}`)}</Badge>;
}
