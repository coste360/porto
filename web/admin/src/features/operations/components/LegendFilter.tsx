import { useT } from '@porto/shared/i18n';
import { LENS_LEGEND, TOKEN_CLASSES, type LensId, type LensStyle } from '@porto/shared/map';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';

/**
 * The legend is the filter (spec 9.2): a checkbox and the count of the elements in that state on the chosen
 * night; "Seleziona / deseleziona tutto"; clicking the count lists the elements.
 */
export function LegendFilter({
  lens,
  counts,
  tokens,
  hidden,
  onHiddenChange,
  onShowList,
}: {
  lens: LensId;
  counts: Record<string, number>;
  tokens: ReadonlyMap<string, LensStyle['colorToken']>;
  hidden: ReadonlySet<string>;
  onHiddenChange: (hidden: Set<string>) => void;
  onShowList: (legendKey: string) => void;
}) {
  const { t } = useT();
  const keys = LENS_LEGEND[lens];
  const toggle = (key: string, on: boolean) => {
    const next = new Set(hidden);
    if (on) next.delete(key);
    else next.add(key);
    onHiddenChange(next);
  };
  return (
    <section aria-label={t('opsMap:legend.label')} className="flex flex-col gap-1">
      <ul className="flex flex-col gap-1">
        {keys.map((key) => {
          const id = `ops-legend-${key}`;
          const token = tokens.get(key) ?? 'muted';
          return (
            <li
              key={key}
              className="flex items-center gap-2"
              data-testid={`ops-legend-item-${key}`}
            >
              <Checkbox
                id={id}
                checked={!hidden.has(key)}
                onCheckedChange={(v) => toggle(key, v === true)}
              />
              <span
                className={`size-3 shrink-0 rounded-sm ${TOKEN_CLASSES[token].bg}`}
                aria-hidden
              />
              <label htmlFor={id} className="flex-1 text-sm">
                {t(`occupancyEnums:lens.legend.${key}`)}
              </label>
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 tabular-nums"
                data-testid={`ops-legend-count-${key}`}
                aria-label={t('opsMap:legend.showList', {
                  label: t(`occupancyEnums:lens.legend.${key}`),
                  count: counts[key] ?? 0,
                })}
                onClick={() => onShowList(key)}
                disabled={(counts[key] ?? 0) === 0}
              >
                {counts[key] ?? 0}
              </Button>
            </li>
          );
        })}
      </ul>
      <div className="flex flex-wrap gap-1">
        <Button variant="link" size="sm" className="px-0" onClick={() => onHiddenChange(new Set())}>
          {t('opsMap:legend.selectAll')}
        </Button>
        <Button
          variant="link"
          size="sm"
          className="px-0"
          onClick={() => onHiddenChange(new Set(keys))}
        >
          {t('opsMap:legend.selectNone')}
        </Button>
      </div>
    </section>
  );
}
