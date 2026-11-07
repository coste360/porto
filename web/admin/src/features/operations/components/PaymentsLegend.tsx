import { useT } from '@porto/shared/i18n';
import { PAYMENTS_LEGEND, TOKEN_CLASSES, type PaymentsLegendKey } from '@porto/shared/map';
import { Checkbox } from '@porto/shared/ui/checkbox';

const TOKEN: Record<PaymentsLegendKey, keyof typeof TOKEN_CLASSES> = {
  ok: 'ok',
  due_soon: 'warn',
  overdue: 'bad',
  overdue_60: 'bad',
  disputed: 'override',
  suspended: 'blocked',
  none: 'muted',
};

/** Legend of the Pagamenti lens (m5-plan §9.1): counts per state, checkboxes hide (dim) a state. */
export function PaymentsLegend({
  counts,
  hidden,
  onHiddenChange,
}: {
  counts: Record<PaymentsLegendKey, number>;
  hidden: ReadonlySet<string>;
  onHiddenChange: (hidden: Set<string>) => void;
}) {
  const { t } = useT();
  return (
    <section
      aria-label={t('receivables:lens.title')}
      className="flex flex-col gap-1"
      data-testid="payments-legend"
    >
      <ul className="flex flex-col gap-1">
        {PAYMENTS_LEGEND.map((key) => (
          <li key={key} className="flex items-center gap-2" data-testid={`payments-legend-${key}`}>
            <Checkbox
              id={`payments-legend-${key}`}
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
            <label htmlFor={`payments-legend-${key}`} className="flex-1 text-sm">
              {key === 'none' ? t('receivables:lens.none') : t(`billingEnums:lensState.${key}`)}
            </label>
            <span className="text-sm tabular-nums">{counts[key]}</span>
          </li>
        ))}
      </ul>
      <p className="text-muted-foreground text-xs">{t('receivables:lens.hint')}</p>
    </section>
  );
}
