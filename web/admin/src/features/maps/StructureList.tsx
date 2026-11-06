import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import type { SceneItem } from '@porto/shared/map';
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@porto/shared/ui/accordion';
import { Button } from '@porto/shared/ui/button';

import { groupBerthsByStructure, groupTitle } from './lib';

export function StructureList({
  items,
  selected,
  onPick,
}: {
  items: readonly SceneItem[];
  selected?: ReadonlySet<string>;
  onPick?: (ref: string, additive: boolean) => void;
}) {
  const { t, i18n } = useT();
  const groups = groupBerthsByStructure(items);
  if (groups.length === 0)
    return <p className="text-muted-foreground p-3 text-sm">{t('maps:list.empty')}</p>;
  return (
    <Accordion type="multiple" aria-label={t('maps:list.title')}>
      {groups.map((g) => {
        const key = g.ref ?? g.kind;
        const { title, subtitle } = groupTitle(g, t);
        const empty = g.berths.length === 0;
        return (
          <AccordionItem key={key} value={key} data-empty={empty || undefined}>
            <AccordionTrigger className={empty ? 'text-muted-foreground' : undefined}>
              <span className="flex flex-1 items-baseline justify-between gap-2">
                <span className="flex flex-col">
                  <span>{title}</span>
                  {subtitle ? (
                    <span className="text-muted-foreground text-xs font-normal">{subtitle}</span>
                  ) : null}
                </span>
                <span className="text-muted-foreground text-xs font-normal tabular-nums">
                  {t('maps:counts.berths', { count: g.berths.length })}
                </span>
              </span>
            </AccordionTrigger>
            <AccordionContent>
              <ul className="flex flex-col gap-1">
                {g.ref && onPick ? (
                  <li>
                    <Button variant="ghost" size="sm" onClick={(e) => onPick(g.ref!, e.shiftKey)}>
                      {t('maps:list.selectStructure', { name: title })}
                    </Button>
                  </li>
                ) : null}
                {empty ? (
                  <li className="text-muted-foreground text-sm">{t('maps:list.noBerths')}</li>
                ) : null}
                {g.berths.map((b) => (
                  <li key={b.ref} className="flex items-center justify-between gap-2 text-sm">
                    {onPick ? (
                      <Button
                        variant={selected?.has(b.ref) ? 'secondary' : 'ghost'}
                        size="sm"
                        aria-pressed={selected?.has(b.ref) ?? false}
                        onClick={(e) => onPick(b.ref, e.shiftKey)}
                      >
                        {b.el.code}
                      </Button>
                    ) : (
                      <span className="font-medium">{b.el.code}</span>
                    )}
                    <span className="text-muted-foreground">
                      {formatCm(b.el.width_cm, i18n.language)} ×{' '}
                      {formatCm(b.el.length_cm, i18n.language)}
                      {b.el.price_class ? ` · ${b.el.price_class}` : ''}
                    </span>
                  </li>
                ))}
              </ul>
            </AccordionContent>
          </AccordionItem>
        );
      })}
    </Accordion>
  );
}
