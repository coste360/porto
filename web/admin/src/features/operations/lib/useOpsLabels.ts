import { useCallback } from 'react';

import type { DayElementData } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import type { SceneItem } from '@porto/shared/map';

import { useElementLabel } from '../../maps/useElementLabel';

/**
 * Accessible name of an operational element: "{code}, {status}, {vessel}" (opsMap:element.aria, spec 9.1),
 * falling back to the M1 label for structures, pedestals and decorations.
 */
export function useOpsLabels(byResource: ReadonlyMap<string, DayElementData> | undefined) {
  const { t } = useT();
  const base = useElementLabel();
  return useCallback(
    (item: SceneItem): string => {
      const el =
        item.kind === 'berth' || item.kind === 'land' || item.kind === 'area'
          ? byResource?.get(item.ref)
          : undefined;
      if (!el) return base(item);
      return t('opsMap:element.aria', {
        code: el.code,
        status: t(`occupancyEnums:dayStatus.${el.status}`),
        vessel: el.vessel?.name ?? t('opsMap:element.noVessel'),
      });
    },
    [t, base, byResource],
  );
}
