import { useCallback } from 'react';

import { useT } from '@porto/shared/i18n';
import type { SceneItem } from '@porto/shared/map';

/** Accessible name of a map element ("Posto A-12", "Pontile B"...). */
export function useElementLabel() {
  const { t } = useT();
  return useCallback(
    (item: SceneItem): string => {
      switch (item.kind) {
        case 'structure':
          return t(`maps:element.structure.${item.el.kind}`, { code: item.el.code });
        case 'berth':
          return item.el.mooring_type === 'buoy'
            ? t('maps:element.buoy', { code: item.el.code })
            : t('maps:element.berth', { code: item.el.code });
        case 'area':
          return t('maps:element.area', { code: item.el.code });
        case 'land':
          return t('maps:element.land', { code: item.el.code });
        case 'pedestal':
          return t('maps:element.pedestal', { code: item.el.code });
        case 'decoration':
          return item.el.kind === 'symbol' && item.el.asset
            ? t(`kit:assets.${item.el.asset}`)
            : t(`maps:element.decoration.${item.el.kind}`);
      }
    },
    [t],
  );
}
