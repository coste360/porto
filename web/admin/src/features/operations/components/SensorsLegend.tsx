import { useT } from '@porto/shared/i18n';
import {
  SENSORS_LEGEND,
  SENSORS_TOKEN,
  TOKEN_CLASSES,
  type SensorsLegendKey,
} from '@porto/shared/map';
import { Checkbox } from '@porto/shared/ui/checkbox';

/** Legend of the Sensori lens (m12-plan §9.2): counts per state, checkboxes hide (dim) a state. */
export function SensorsLegend({
  counts,
  hidden,
  onHiddenChange,
}: {
  counts: Record<SensorsLegendKey, number>;
  hidden: ReadonlySet<string>;
  onHiddenChange: (hidden: Set<string>) => void;
}) {
  const { t } = useT();
  return (
    <section
      aria-label={t('sensors:lens.toggle')}
      className="flex flex-col gap-1"
      data-testid="sensors-legend"
    >
      <ul className="flex flex-col gap-1">
        {SENSORS_LEGEND.map((key) => (
          <li key={key} className="flex items-center gap-2" data-testid={`sensors-legend-${key}`}>
            <Checkbox
              id={`sensors-legend-${key}`}
              checked={!hidden.has(key)}
              onCheckedChange={(v) => {
                const next = new Set(hidden);
                if (v === true) next.delete(key);
                else next.add(key);
                onHiddenChange(next);
              }}
            />
            <span
              className={`size-3 shrink-0 rounded-sm ${TOKEN_CLASSES[SENSORS_TOKEN[key]].bg}`}
              aria-hidden
            />
            <label htmlFor={`sensors-legend-${key}`} className="flex-1 text-sm">
              {t(`sensors:lens.${key}`)}
            </label>
            <span className="text-sm tabular-nums" data-testid={`sensors-legend-count-${key}`}>
              {counts[key]}
            </span>
          </li>
        ))}
      </ul>
      <p className="text-muted-foreground text-xs">{t('sensors:lens.hint')}</p>
    </section>
  );
}
