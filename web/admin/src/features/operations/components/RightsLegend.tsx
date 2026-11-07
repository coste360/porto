import { useT } from '@porto/shared/i18n';
import { RIGHTS_LEGEND, TOKEN_CLASSES, type RightsLegendKey } from '@porto/shared/map';
import { Checkbox } from '@porto/shared/ui/checkbox';

const TOKEN: Record<RightsLegendKey, keyof typeof TOKEN_CLASSES> = {
  marina: 'free',
  holder: 'owner',
  mandate: 'transit',
  unpaid: 'bad',
  none: 'muted',
};

/** Legend of the Diritti d'uso lens (m4-plan §8.1): counts per key, checkboxes hide (dim) a key. */
export function RightsLegend({
  counts,
  hidden,
  onHiddenChange,
}: {
  counts: Record<RightsLegendKey, number>;
  hidden: ReadonlySet<string>;
  onHiddenChange: (hidden: Set<string>) => void;
}) {
  const { t } = useT();
  return (
    <section
      aria-label={t('rights:lens.title')}
      className="flex flex-col gap-1"
      data-testid="rights-legend"
    >
      <ul className="flex flex-col gap-1">
        {RIGHTS_LEGEND.map((key) => (
          <li key={key} className="flex items-center gap-2" data-testid={`rights-legend-${key}`}>
            <Checkbox
              id={`rights-legend-${key}`}
              checked={!hidden.has(key)}
              onCheckedChange={(v) => {
                const next = new Set(hidden);
                if (v === true) next.delete(key);
                else next.add(key);
                onHiddenChange(next);
              }}
            />
            <span
              className={`size-3 shrink-0 rounded-sm ${TOKEN_CLASSES[TOKEN[key]].bg}`}
              aria-hidden
            />
            <label htmlFor={`rights-legend-${key}`} className="flex-1 text-sm">
              {t(`rights:lens.legend.${key}`)}
            </label>
            <span className="text-sm tabular-nums">{counts[key]}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
