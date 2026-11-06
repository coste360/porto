import { LayersIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { LAYERS, toggleLayer, type LayerVisibility } from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { Label } from '@porto/shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '@porto/shared/ui/popover';
import { Switch } from '@porto/shared/ui/switch';

export function LayerSwitches({
  layers,
  onChange,
}: {
  layers: LayerVisibility;
  onChange: (layers: LayerVisibility) => void;
}) {
  const { t } = useT();
  return (
    <ul className="flex flex-col gap-2" aria-label={t('maps:layers.title')}>
      {LAYERS.map((id) => (
        <li key={id} className="flex items-center justify-between gap-4">
          <Label htmlFor={`layer-${id}`}>{t(`maps:layers.${id}`)}</Label>
          <Switch
            id={`layer-${id}`}
            checked={layers[id]}
            onCheckedChange={(on) => onChange(toggleLayer(layers, id, on))}
          />
        </li>
      ))}
    </ul>
  );
}

/** Layer switches in a popover (spec 8.1). */
export function LayerToggles(props: {
  layers: LayerVisibility;
  onChange: (layers: LayerVisibility) => void;
}) {
  const { t } = useT();
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm">
          <LayersIcon />
          {t('maps:layers.title')}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-64">
        <LayerSwitches {...props} />
      </PopoverContent>
    </Popover>
  );
}
