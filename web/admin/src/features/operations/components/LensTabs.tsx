import { ChevronDownIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { OTHER_LENSES, PRIMARY_LENSES, type LensId } from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

/** Lens tabs above the map, the less used ones under "Altri" (spec 9.2). While an overlay lens (Diritti d'uso,
 * Pagamenti, Sensori) colours the map, no tab looks selected: picking one switches the overlay off. */
export function LensTabs({
  lens,
  onChange,
  overlay = false,
}: {
  lens: LensId;
  onChange: (lens: LensId) => void;
  overlay?: boolean;
}) {
  const { t } = useT();
  const other = OTHER_LENSES.includes(lens);
  const shown = overlay ? '' : lens;
  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* A toggle group, not tabs: the lens recolours the same map, there is no tab panel per lens. */}
      <ToggleGroup
        type="single"
        variant="outline"
        value={other ? '' : shown}
        onValueChange={(v) => {
          if (v) onChange(v as LensId);
        }}
        aria-label={t('opsMap:lens.label')}
      >
        {PRIMARY_LENSES.map((l) => (
          <ToggleGroupItem key={l} value={l} data-testid={`ops-lens-tab-${l}`}>
            {t(`occupancyEnums:lens.${l}`)}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant={other && !overlay ? 'secondary' : 'ghost'}
            size="sm"
            data-testid="ops-lens-other"
          >
            {other && !overlay ? t(`occupancyEnums:lens.${lens}`) : t('opsMap:lens.other')}
            <ChevronDownIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuRadioGroup value={shown} onValueChange={(v) => onChange(v as LensId)}>
            {OTHER_LENSES.map((l) => (
              <DropdownMenuRadioItem key={l} value={l} data-testid={`ops-lens-tab-${l}`}>
                {t(`occupancyEnums:lens.${l}`)}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
