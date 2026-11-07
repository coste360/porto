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

/** Lens tabs above the map, the less used ones under "Altri" (spec 9.2). */
export function LensTabs({ lens, onChange }: { lens: LensId; onChange: (lens: LensId) => void }) {
  const { t } = useT();
  const other = OTHER_LENSES.includes(lens);
  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* A toggle group, not tabs: the lens recolours the same map, there is no tab panel per lens. */}
      <ToggleGroup
        type="single"
        variant="outline"
        value={other ? '' : lens}
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
          <Button variant={other ? 'secondary' : 'ghost'} size="sm" data-testid="ops-lens-other">
            {other ? t(`occupancyEnums:lens.${lens}`) : t('opsMap:lens.other')}
            <ChevronDownIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuRadioGroup value={lens} onValueChange={(v) => onChange(v as LensId)}>
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
