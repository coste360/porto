import { PlusIcon, Trash2Icon } from 'lucide-react';

import { useAllItems } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { MoneyInput } from '@porto/shared/money';
import { Button } from '@porto/shared/ui/button';
import { Input } from '@porto/shared/ui/input';
import { ElementCombobox } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { parseQty, previewLine, previewTotals } from '../lib/lines';
import { Money, SimpleTable, VatRate } from './kit';
import { type LineDraft, emptyLine, itemName, parseSignedQty, previewInput } from '../lib/drafts';

/**
 * Item lines with live VAT preview (m5-plan §9.1). The unit price is optional: empty means "from the active
 * price list" and the line has no preview until the server prices it.
 */
export function LinesEditor({
  marinaId,
  lines,
  onChange,
  allowNegative = false,
  idPrefix = 'line',
}: {
  marinaId: string;
  lines: LineDraft[];
  onChange: (lines: LineDraft[]) => void;
  allowNegative?: boolean;
  idPrefix?: string;
}) {
  const { t, i18n } = useT();
  const items = useAllItems(marinaId);
  const list = (items.data ?? []).filter((i) => i.active);
  const byId = new Map(list.map((i) => [i.id, i]));
  const options = list.map((i) => ({
    value: i.id,
    label: `${i.code} · ${itemName(i, i18n.language)}`,
  }));
  const patch = (key: number, p: Partial<LineDraft>) =>
    onChange(lines.map((l) => (l.key === key ? { ...l, ...p } : l)));
  const totals = previewTotals(lines.map((l) => previewInput(l, byId, allowNegative)));
  return (
    <div className="flex flex-col gap-2">
      <SimpleTable
        head={[
          t('charges:lines.item'),
          t('charges:lines.qty'),
          t('charges:lines.unit'),
          t('charges:lines.discount'),
          t('charges:lines.vat'),
          t('charges:lines.total'),
          '',
        ]}
        empty={t('charges:lines.empty')}
        rows={lines.length}
      >
        {lines.map((l, i) => {
          const item = l.itemId ? byId.get(l.itemId) : undefined;
          const pv = previewInput(l, byId, allowNegative);
          return (
            <TableRow key={l.key} data-testid="line-row">
              <TableCell className="min-w-56">
                <ElementCombobox
                  id={`${idPrefix}-item-${i}`}
                  aria-label={t('charges:lines.itemOf', { n: i + 1 })}
                  options={options}
                  value={l.itemId}
                  onChange={(v) => patch(l.key, { itemId: v })}
                  placeholder={t('charges:lines.itemPlaceholder')}
                  searchPlaceholder={t('charges:lines.itemSearch')}
                  emptyText={t('charges:lines.noItems')}
                />
              </TableCell>
              <TableCell className="w-24">
                <Input
                  aria-label={t('charges:lines.qtyOf', { n: i + 1 })}
                  inputMode="decimal"
                  value={l.qty}
                  aria-invalid={
                    (allowNegative ? parseSignedQty(l.qty) : parseQty(l.qty)) === null
                      ? true
                      : undefined
                  }
                  onChange={(e) => patch(l.key, { qty: e.target.value })}
                />
              </TableCell>
              <TableCell className="w-32">
                <MoneyInput
                  aria-label={t('charges:lines.unitOf', { n: i + 1 })}
                  placeholder={t('charges:lines.fromList')}
                  locale={i18n.language}
                  value={l.unitCents}
                  onValueChange={(v) => patch(l.key, { unitCents: v })}
                />
              </TableCell>
              <TableCell className="w-28">
                <MoneyInput
                  aria-label={t('charges:lines.discountOf', { n: i + 1 })}
                  locale={i18n.language}
                  value={l.discountCents}
                  onValueChange={(v) => patch(l.key, { discountCents: v })}
                />
              </TableCell>
              <TableCell>
                {item ? <VatRate bp={item.vat_rate_bp} nature={item.vat_nature} /> : null}
              </TableCell>
              <TableCell>{pv ? <Money cents={previewLine(pv).gross} /> : null}</TableCell>
              <TableCell>
                <Button
                  type="button"
                  size="icon"
                  variant="ghost"
                  aria-label={t('charges:lines.remove', { n: i + 1 })}
                  onClick={() => onChange(lines.filter((x) => x.key !== l.key))}
                >
                  <Trash2Icon />
                </Button>
              </TableCell>
            </TableRow>
          );
        })}
      </SimpleTable>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={() => onChange([...lines, emptyLine()])}
        >
          <PlusIcon />
          {t('charges:lines.add')}
        </Button>
        <div className="flex flex-wrap gap-4 text-sm" data-testid="lines-preview">
          <span>
            {t('charges:vat.net')} <Money cents={totals.net} />
          </span>
          <span>
            {t('charges:vat.vat')} <Money cents={totals.vat} />
          </span>
          <span className="font-semibold">
            {t('charges:vat.gross')} <Money cents={totals.gross} />
          </span>
        </div>
      </div>
      <p className="text-muted-foreground text-xs">{t('charges:lines.previewHint')}</p>
    </div>
  );
}
